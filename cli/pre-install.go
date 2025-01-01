package cli

import (
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var preInstallCmd = &cobra.Command{
	Use:   "pre-install",
	Short: "pre-install module",
	Run: func(cmd *cobra.Command, args []string) {
		log.Info().Msgf("pre-install module name: %s namespace: %s", name, namespace)
	},
}
