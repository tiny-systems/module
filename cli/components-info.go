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

		// Conformance: an error port must emit the canonical
		// module.ErrorMessage shape ({context, error, retryable}) so the
		// retry component and the platform understand it. Warn — don't
		// fail — so a mid-migration module still runs, but the author
		// (first- or third-party) sees the gap when introspecting instead
		// of a silent runtime no-op once someone wires the port into retry.
		for _, w := range validateErrorPorts(collectComponentsApi()) {
			log.Warn().Msg(w)
		}
	},
}
