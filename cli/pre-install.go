package cli

import (
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/registry"
)

var preInstallCmd = &cobra.Command{
	Use:   "pre-install",
	Short: "pre-install module",
	Run: func(cmd *cobra.Command, args []string) {
		log.Info().Msgf("check pre-install")

		for _, c := range registry.Get() {
			for _, p := range c.Ports() {
				if p.Name != module.PreInstall {
					continue
				}
				log.Info().Msgf("pre-install module using: %s component", c.GetInfo().Name)
				return
			}
		}
	},
}
