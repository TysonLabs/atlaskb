package cli

import "github.com/spf13/cobra"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print AtlasKB version information",
	RunE: func(cmd *cobra.Command, args []string) error {
		return writeVersionInfo()
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
