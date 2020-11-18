package cmd

// FIXME add --purge-binary or --purge=all to purge chezmoi binary after init
// FIXME combine above into --ninja option to set up dotfiles and remove all traces that chezmoi was ever there
// FIXME should ninja be an undocumented command?

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	"github.com/go-git/go-git/v5"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/twpayne/go-vfs"

	"github.com/twpayne/chezmoi/next/internal/chezmoi"
)

type initCmdConfig struct {
	apply         bool
	depth         int
	purge         bool
	useBuiltinGit bool
}

var dotfilesRepoGuesses = []struct {
	rx     *regexp.Regexp
	format string
}{
	{
		rx:     regexp.MustCompile(`\A[-0-9A-Za-z]+\z`),
		format: "https://github.com/%s/dotfiles.git",
	},
	{
		rx:     regexp.MustCompile(`\A[-0-9A-Za-z]+/[-0-9A-Za-z]+\.git\z`),
		format: "https://github.com/%s",
	},
	{
		rx:     regexp.MustCompile(`\A[-0-9A-Za-z]+/[-0-9A-Za-z]+\z`),
		format: "https://github.com/%s.git",
	},
	{
		rx:     regexp.MustCompile(`\A[-.0-9A-Za-z]+/[-0-9A-Za-z]+\z`),
		format: "https://%s/dotfiles.git",
	},
	{
		rx:     regexp.MustCompile(`\A[-.0-9A-Za-z]+/[-0-9A-Za-z]+/[-0-9A-Za-z]+\z`),
		format: "https://%s.git",
	},
	{
		rx:     regexp.MustCompile(`\A[-.0-9A-Za-z]+/[-0-9A-Za-z]+/[-0-9A-Za-z]+\.git\z`),
		format: "https://%s",
	},
	{
		rx:     regexp.MustCompile(`\Asr\.ht/~[-0-9A-Za-z]+\z`),
		format: "https://git.%s/dotfiles",
	},
	{
		rx:     regexp.MustCompile(`\Asr\.ht/~[-0-9A-Za-z]+/[-0-9A-Za-z]+\z`),
		format: "https://git.%s",
	},
}

func (c *Config) newInitCmd() *cobra.Command {
	initCmd := &cobra.Command{
		Args:    cobra.MaximumNArgs(1),
		Use:     "init [repo]",
		Short:   "Setup the source directory and update the destination directory to match the target state",
		Long:    mustGetLongHelp("init"),
		Example: getExample("init"),
		RunE:    c.runInitCmd,
		Annotations: map[string]string{
			modifiesDestinationDirectory: "true",
			requiresSourceDirectory:      "true",
			runsCommands:                 "true",
		},
	}

	persistentFlags := initCmd.PersistentFlags()
	persistentFlags.BoolVarP(&c.init.apply, "apply", "a", c.init.apply, "update destination directory")
	persistentFlags.IntVar(&c.init.depth, "depth", c.init.depth, "create a shallow clone")
	persistentFlags.BoolVar(&c.init.useBuiltinGit, "use-builtin-git", c.init.useBuiltinGit, "use builtin git")
	persistentFlags.BoolVarP(&c.init.purge, "purge", "p", c.init.purge, "purge config and source directories")

	return initCmd
}

func (c *Config) runInitCmd(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		if c.init.useBuiltinGit {
			rawSourceDir, err := c.baseSystem.RawPath(c.absSourceDir)
			if err != nil {
				return err
			}
			isBare := false
			_, err = git.PlainInit(rawSourceDir, isBare)
			return err
		}
		return c.run(c.absSourceDir, c.Git.Command, []string{"init"})
	}

	// Clone repo into source directory if it does not already exist.
	_, err := c.baseSystem.Stat(path.Join(c.absSourceDir, ".git"))
	switch {
	case err == nil:
		// Do nothing.
	case os.IsNotExist(err):
		rawSourceDir, err := c.baseSystem.RawPath(c.absSourceDir)
		if err != nil {
			return err
		}

		dotfilesRepoURL := guessDotfilesRepoURL(args[0])
		if c.init.useBuiltinGit {
			isBare := false
			if _, err := git.PlainClone(rawSourceDir, isBare, &git.CloneOptions{
				URL:               dotfilesRepoURL,
				Depth:             c.init.depth,
				RecurseSubmodules: git.DefaultSubmoduleRecursionDepth,
			}); err != nil {
				return err
			}
		} else {
			args := []string{
				"clone",
				"--recurse-submodules",
			}
			if c.init.depth != 0 {
				args = append(args,
					"--depth", strconv.Itoa(c.init.depth),
				)
			}
			args = append(args,
				dotfilesRepoURL,
				rawSourceDir,
			)
			if err := c.run("", c.Git.Command, args); err != nil {
				return err
			}
		}
	default:
		return err
	}

	// Find config template, execute it, and create config file.
	filename, ext, data, err := c.findConfigTemplate()
	if err != nil {
		return err
	}
	var configFileContents []byte
	if filename != "" {
		configFileContents, err = c.createConfigFile(filename, data)
		if err != nil {
			return err
		}
	}

	// Reload config if it was created.
	if filename != "" {
		viper.SetConfigType(ext)
		if err := viper.ReadConfig(bytes.NewBuffer(configFileContents)); err != nil {
			return err
		}
		if err := viper.Unmarshal(c); err != nil {
			return err
		}
	}

	// Apply.
	if c.init.apply {
		var args []string
		recursive := false
		if err := c.applyArgs(c.destSystem, c.absDestDir, args, chezmoi.NewIncludeSet(chezmoi.IncludeAll), recursive, c.Umask.FileMode()); err != nil {
			return err
		}
	}

	// Purge.
	if c.init.purge {
		if err := c.purge(); err != nil {
			return err
		}
	}

	return nil
}

// createConfigFile creates a config file using a template and returns its
// contents.
func (c *Config) createConfigFile(filename string, data []byte) ([]byte, error) {
	funcMap := make(template.FuncMap)
	for key, value := range c.templateFuncs {
		funcMap[key] = value
	}
	for name, f := range map[string]interface{}{
		"promptBool":   c.promptBool,
		"promptInt":    c.promptInt,
		"promptString": c.promptString,
	} {
		funcMap[name] = f
	}

	t, err := template.New(filename).Funcs(funcMap).Parse(string(data))
	if err != nil {
		return nil, err
	}

	templateData, err := c.getDefaultTemplateData()
	if err != nil {
		return nil, err
	}

	sb := strings.Builder{}
	if err = t.Execute(&sb, map[string]interface{}{
		"chezmoi": templateData,
	}); err != nil {
		return nil, err
	}
	contents := []byte(sb.String())

	configDir := filepath.Join(c.bds.ConfigHome, "chezmoi")
	if err := vfs.MkdirAll(c.baseSystem, configDir, 0o777); err != nil {
		return nil, err
	}

	configPath := filepath.Join(configDir, filename)
	if err := c.baseSystem.WriteFile(configPath, contents, 0o600); err != nil {
		return nil, err
	}

	return contents, nil
}

func (c *Config) findConfigTemplate() (string, string, []byte, error) {
	for _, ext := range viper.SupportedExts {
		filename := chezmoi.Prefix + "." + ext + chezmoi.TemplateSuffix
		contents, err := c.baseSystem.ReadFile(path.Join(c.absSourceDir, filename))
		switch {
		case os.IsNotExist(err):
			continue
		case err != nil:
			return "", "", nil, err
		}
		return "chezmoi." + ext, ext, contents, nil
	}
	return "", "", nil, nil
}

func (c *Config) promptBool(field string) bool {
	value, err := parseBool(c.promptString(field))
	if err != nil {
		panic(err)
	}
	return value
}

func (c *Config) promptInt(field string) int64 {
	value, err := strconv.ParseInt(c.promptString(field), 10, 64)
	if err != nil {
		panic(err)
	}
	return value
}

func (c *Config) promptString(field string) string {
	fmt.Fprintf(c.stdout, "%s? ", field)
	value, err := bufio.NewReader(c.stdin).ReadString('\n')
	if err != nil {
		panic(err)
	}
	return strings.TrimSpace(value)
}

// guessDotfilesRepoURL guesses the user's dotfile repo from arg.
func guessDotfilesRepoURL(arg string) string {
	for _, dotfileRepoGuess := range dotfilesRepoGuesses {
		if dotfileRepoGuess.rx.MatchString(arg) {
			return fmt.Sprintf(dotfileRepoGuess.format, arg)
		}
	}
	return arg
}
