package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/registry"
	"sigs.k8s.io/yaml"
)

// rbacValuesCmd prints the operator-chart values overlay (the `rbac:` block)
// derived from the module's registry.SetRequirements. It is the single source
// of truth for the self-hosted install's ClusterRole: a module's values.yaml in
// the tiny-systems/modules index should be GENERATED with this command, not
// hand-written, so the granted RBAC always matches what the module's code
// actually declares. Drift between the compiled-in requirements and a
// hand-maintained overlay is what let pod_logs_get ship without `pods/log`
// access — the components silently 403 at runtime.
//
// Usage (from the module's release pipeline or a repo generator):
//
//	<module-binary> tools rbac-values > modules/<name>/values.yaml
//
// The output is exactly the overlay the operator chart's manager-rbac.yaml
// consumes via .Values.rbac.{enableKubernetesResourceAccess,extraRules}.
var rbacValuesCmd = &cobra.Command{
	Use:   "rbac-values",
	Short: "Print the operator-chart rbac values overlay derived from SetRequirements",
	Long: `Emit the module's declared RBAC as the operator chart's values overlay:

    rbac:
      enableKubernetesResourceAccess: <bool>
      extraRules:
      - apiGroups: [...]
        resources: [...]
        verbs: [...]

Generate the module's index values.yaml with this so the self-hosted install
grants exactly the RBAC the module declares via registry.SetRequirements,
instead of a hand-copied overlay that drifts.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Mirror the operator chart's values shape: a top-level `rbac` key
		// holding the module's RBACRequirements (json tags already match the
		// chart's .Values.rbac.* keys).
		var overlay struct {
			RBAC module.RBACRequirements `json:"rbac"`
		}
		if reqs := registry.GetRequirements(); reqs != nil {
			overlay.RBAC = reqs.RBAC
		}
		out, err := yaml.Marshal(overlay)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "marshal rbac values: %v\n", err)
			os.Exit(1)
		}
		_, _ = os.Stdout.Write(out)
	},
}
