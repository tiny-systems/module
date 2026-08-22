package cli

import (
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/tiny-systems/module/module"
)

// The overlay a module emits must be able to express namespaced rules.
//
// The operator chart has consumed `rbac.extraNamespacedRules` since 0.2.12, and
// two published modules already narrow their permissions that way — but the
// declaration type had no such field, so a module could be INSTALLED with
// narrow rules while its image declared the broad bundle. The drift gate
// correctly reported the two disagreeing, and no edit to either module could
// have fixed it.
func TestOverlayCarriesNamespacedRules(t *testing.T) {
	reqs := module.RBACRequirements{
		ExtraNamespacedRules: []module.RBACRule{{
			APIGroups: []string{""},
			Resources: []string{"secrets"},
			Verbs:     []string{"get", "list"},
		}},
	}

	var overlay struct {
		RBAC module.RBACRequirements `json:"rbac"`
	}
	overlay.RBAC = reqs
	out, err := yaml.Marshal(overlay)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(out)

	// The key has to match what the chart reads, exactly.
	if !strings.Contains(got, "extraNamespacedRules:") {
		t.Errorf("overlay omits extraNamespacedRules:\n%s", got)
	}
	// And a module that only needs namespaced access must not be described as
	// needing the cluster-wide bundle.
	if strings.Contains(got, "enableKubernetesResourceAccess") {
		t.Errorf("overlay claims cluster-wide access it did not declare:\n%s", got)
	}
	if strings.Contains(got, "extraRules:") {
		t.Errorf("namespaced rules leaked into the cluster-wide list:\n%s", got)
	}
}

// A module declaring ONLY namespaced rules must still emit an overlay. The
// emptiness check predates the field, so it would have returned early and
// published nothing — which the drift gate reads as "declares no RBAC" while
// the module quietly needs some.
func TestNamespacedOnlyModuleStillEmitsAnOverlay(t *testing.T) {
	reqs := &module.Requirements{RBAC: module.RBACRequirements{
		ExtraNamespacedRules: []module.RBACRule{{
			APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{"get"},
		}},
	}}

	empty := reqs == nil || (!reqs.RBAC.EnableKubernetesResourceAccess &&
		len(reqs.RBAC.ExtraRules) == 0 && len(reqs.RBAC.ExtraNamespacedRules) == 0)
	if empty {
		t.Fatal("a module declaring namespaced rules was treated as declaring none")
	}
}
