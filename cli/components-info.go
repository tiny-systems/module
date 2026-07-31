package cli

import (
	"encoding/json"
	"os"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/tiny-systems/module/registry"
)

// componentsInfoJSON switches the command from a human summary to the machine
// shape a repo index carries.
var componentsInfoJSON bool

// discoveryComponent is what a module publishes about itself so it can be found
// before it is installed: enough to choose it, not enough to wire it up.
//
// Ports are deliberately absent. They are large, change with every release, and
// a running node answers for them authoritatively — while this ends up in a
// static index file that a crawler re-fetches on a loop, so it has to stay
// small. The platform's catalog reads this for uninstalled modules and merges
// live introspection over it once a module is actually running.
type discoveryComponent struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Info        string   `json:"info,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

var infoCmd = &cobra.Command{
	Use:   "components-info",
	Short: "Get components info",
	Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {
		components := registry.Get()

		// Emit the discovery shape for a module repo's components.yaml, which
		// `tiny repo index` folds into the index. Machine output goes to stdout
		// on its own so a release can redirect it straight into the file.
		if componentsInfoJSON {
			out := make([]discoveryComponent, 0, len(components))
			for _, c := range collectComponentsApi() {
				d := discoveryComponent{Name: c.Name, Description: c.Description}
				if c.Info != nil {
					d.Info = *c.Info
				}
				if c.Tags != nil {
					d.Tags = *c.Tags
				}
				out = append(out, d)
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(out); err != nil {
				log.Error().Err(err).Msg("encode components")
				os.Exit(1)
			}
			return
		}

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
