package cli

import (
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var preDeleteCmd = &cobra.Command{
	Use:   "pre-delete",
	Short: "pre-delete module",
	Run: func(cmd *cobra.Command, args []string) {

		log.Info().Msgf("pre-delete module name: %s namespace: %s", name, namespace)
	},
}
