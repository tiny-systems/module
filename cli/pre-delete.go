package cli

import (
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/registry"
)

var preDeleteCmd = &cobra.Command{
	Use:   "pre-delete",
	Short: "pre-delete module",
	Run: func(cmd *cobra.Command, args []string) {
		log.Info().Msgf("check pre-delete")

		for _, c := range registry.Get() {
			for _, p := range c.Ports() {
				if p.Name != module.PreDelete {
					continue
				}
				log.Info().Msgf("pre-delete module using: %s component", c.GetInfo().Name)
				return
			}
		}
	},
}
