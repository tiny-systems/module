package cli

import "testing"

func TestPermitsHonoursWildcards(t *testing.T) {
	declared := map[groupResource][]string{
		{"batch", "jobs"}: {"create", "get"},
		{"apps", "*"}:     {"update"},
		{"*", "*"}:        {"list"},
		{"", "pods"}:      {"*"},
	}
	cases := []struct {
		gr   groupResource
		verb string
		want bool
	}{
		{groupResource{"batch", "jobs"}, "create", true},
		{groupResource{"batch", "jobs"}, "delete", false}, // the real bug's shape
		{groupResource{"apps", "deployments"}, "update", true},
		{groupResource{"anything", "whatever"}, "list", true},
		{groupResource{"", "pods"}, "delete", true}, // verb wildcard
		{groupResource{"", "secrets"}, "get", false},
	}
	for _, c := range cases {
		if got := permits(declared, c.gr, c.verb); got != c.want {
			t.Errorf("permits(%v, %q) = %v, want %v", c.gr, c.verb, got, c.want)
		}
	}
}

func TestPluralize(t *testing.T) {
	cases := map[string]string{
		"Pod": "pods", "Job": "jobs", "Deployment": "deployments",
		"Ingress": "ingresses", "NetworkPolicy": "networkpolicies",
		"Service": "services", "ConfigMap": "configmaps", "Secret": "secrets",
	}
	for kind, want := range cases {
		if got := pluralize(kind); got != want {
			t.Errorf("pluralize(%q) = %q, want %q", kind, got, want)
		}
	}
}

// The base flag reads as broad access but grants no writes — the confusion that
// let the missing create/delete ship.
func TestBaseAccessGrantsNoWrites(t *testing.T) {
	declared := DeclaredRules(true, nil)
	for _, verb := range []string{"create", "delete"} {
		if permits(declared, groupResource{"", "pods"}, verb) {
			t.Errorf("base access unexpectedly grants pods %s", verb)
		}
	}
	if !permits(declared, groupResource{"", "pods"}, "get") {
		t.Error("base access should grant pods get")
	}
}
