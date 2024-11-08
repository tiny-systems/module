package cli

import (
	"github.com/spf13/cobra"
)

var (
	devKey string
)
var toolsCmd = &cobra.Command{
	Use:   "tools",
	Short: "Developer tools",
	Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}
