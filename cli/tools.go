package cli

import (
	"github.com/spf13/cobra"
)

var (
	devKey       string
	serverApiURL string
)
var toolsCmd = &cobra.Command{
	Use:   "tools",
	Short: "Developer tools",
	Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}
