package cmd

import (
	"errors"

	"github.com/go-git/go-git/v5"
	"github.com/spf13/cobra"

	"github.com/twpayne/chezmoi/next/internal/chezmoi"
)

type updateCmdConfig struct {
	apply         bool
	include       *chezmoi.IncludeSet
	recursive     bool
	useBuiltinGit bool
}

func (c *Config) newUpdateCmd() *cobra.Command {
	updateCmd := &cobra.Command{
		Use:     "update",
		Short:   "Pull and apply any changes",
		Long:    mustGetLongHelp("update"),
		Example: getExample("update"),
		Args:    cobra.NoArgs,
		RunE:    c.runUpdateCmd,
		Annotations: map[string]string{
			modifiesDestinationDirectory: "true",
			requiresSourceDirectory:      "true",
			runsCommands:                 "true",
		},
	}

	persistentFlags := updateCmd.PersistentFlags()
	persistentFlags.BoolVarP(&c.update.apply, "apply", "a", c.update.apply, "apply after pulling")
	persistentFlags.VarP(c.update.include, "include", "i", "include entry types")
	persistentFlags.BoolVarP(&c.update.recursive, "recursive", "r", c.update.recursive, "recursive")
	persistentFlags.BoolVar(&c.update.useBuiltinGit, "use-builtin-git", c.update.useBuiltinGit, "use builtin git")

	return updateCmd
}

func (c *Config) runUpdateCmd(cmd *cobra.Command, args []string) error {
	if c.update.useBuiltinGit {
		rawSourceDir, err := c.baseSystem.RawPath(c.absSourceDir)
		if err != nil {
			return err
		}
		repo, err := git.PlainOpen(rawSourceDir)
		if err != nil {
			return err
		}
		wt, err := repo.Worktree()
		if err != nil {
			return err
		}
		if err := wt.Pull(&git.PullOptions{
			RecurseSubmodules: git.DefaultSubmoduleRecursionDepth,
		}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
			return err
		}
	} else {
		args := []string{
			"pull",
			"--rebase",
			"--recurse-submodules",
		}
		if err := c.run(c.absSourceDir, c.Git.Command, args); err != nil {
			return err
		}
	}

	if !c.update.apply {
		return nil
	}

	return c.applyArgs(c.destSystem, c.absDestDir, args, c.update.include, c.update.recursive, c.Umask.FileMode())
}
