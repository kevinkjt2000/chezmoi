package cmd

// FIXME add --binary command to attempt to remove binary

import (
	"github.com/spf13/cobra"
)

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
	return purgeCmd
}

func (c *Config) runPurgeCmd(cmd *cobra.Command, args []string) error {
	return c.purge()
}
