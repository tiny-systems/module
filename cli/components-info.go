package cli

import (
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/tiny-systems/module/registry"
)

var infoCmd = &cobra.Command{
	Use:   "components-info",
	Short: "Get components info",
	Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {
		components := registry.Get()
		log.Info().Int("components", len(components)).Msg("registered")
		for _, c := range components {
			log.Info().Msgf("%s - %s\n", c.GetInfo().Name, c.GetInfo().Description)
		}
	},
}
