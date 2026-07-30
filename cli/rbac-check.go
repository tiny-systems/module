package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tiny-systems/module/registry"
)

var rbacCheckStrict bool

var rbacCheckCmd = &cobra.Command{
	Use:   "rbac-check",
	Short: "Report Kubernetes calls the module's declared RBAC does not cover",
	Long: `Resolves every controller-runtime client call in the module's source and
compares it against the rules declared with registry.SetRequirements.

This is the gap the drift gate cannot see. That gate compares the published
overlay against the module's own declaration, so both can agree while both omit
a verb the code needs — which is how pod_create and pod_delete shipped for
months calling Create and Delete with neither verb declared, failing with a 403
on every self-hosted install.

Run from the module's root, where the source is. Reports nothing for calls it
cannot resolve (an interface it cannot follow, an unstructured object whose kind
is only known at runtime): a guess would be worse than silence.`,
	Run: func(cmd *cobra.Command, args []string) {
		cwd, err := os.Getwd()
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "unable to get current path: %v\n", err)
			os.Exit(1)
		}

		reqs := registry.GetRequirements()
		enableBase := false
		var extra [][3][]string
		if reqs != nil {
			enableBase = reqs.RBAC.EnableKubernetesResourceAccess
			for _, r := range reqs.RBAC.ExtraRules {
				extra = append(extra, [3][]string{r.APIGroups, r.Resources, r.Verbs})
			}
		}

		findings, err := CheckRBACCoverage(cwd, DeclaredRules(enableBase, extra))
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "rbac-check: %v\n", err)
			os.Exit(1)
		}

		if len(findings) == 0 {
			fmt.Println("rbac-check: every resolved Kubernetes call is covered by the declared rules")
			return
		}

		_, _ = fmt.Fprintf(os.Stderr, "rbac-check: %d call(s) not covered by the declared RBAC:\n", len(findings))
		for _, f := range findings {
			_, _ = fmt.Fprintf(os.Stderr, "  %-40s needs %-8s first seen at %s\n", f.Resource, f.Verb, f.Position)
		}
		_, _ = fmt.Fprintf(os.Stderr, "\nAdd the missing verbs to registry.SetRequirements, rebuild, then regenerate\nthe overlay from the image: docker run --rm --entrypoint /manager <image> tools rbac-values\n")

		// Exit non-zero only on request. A module mid-change should not have its
		// build broken by this, but CI that wants the guarantee can ask for it.
		if rbacCheckStrict {
			os.Exit(1)
		}
	},
}
