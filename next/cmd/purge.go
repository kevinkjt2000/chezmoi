package cmd

import (
	"github.com/spf13/cobra"
)

type purgeCmdConfig struct {
	binary bool
}

func (c *Config) newPurgeCmd() *cobra.Command {
	purgeCmd := &cobra.Command{
		Use:     "purge",
		Short:   "Purge all of chezmoi's configuration and data",
		Long:    mustGetLongHelp("purge"),
		Example: getExample("purge"),
		Args:    cobra.NoArgs,
		RunE:    c.runPurgeCmd,
		Annotations: map[string]string{
			modifiesSourceDirectory: "true",
		},
	}

	persistentFlags := purgeCmd.PersistentFlags()
	persistentFlags.BoolVar(&c.purge.binary, "binary", c.purge.binary, "purge chezmoi executable")

	return purgeCmd
}

func (c *Config) runPurgeCmd(cmd *cobra.Command, args []string) error {
	return c.doPurge(&purgeOptions{
		binary: c.purge.binary,
	})
}
