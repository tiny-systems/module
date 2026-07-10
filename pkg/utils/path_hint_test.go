package utils

import (
	"strings"
	"testing"

	"github.com/tiny-systems/ajson"
)

func TestPathHint(t *testing.T) {
	// realIP is nested under context; maxmind_account is at the root — the
	// exact Pattern-A/B mix-up from the geolite flow.
	root, err := ajson.Unmarshal([]byte(`{"context":{"realIP":"1.2.3.4"},"maxmind_account":"acc","maxmind_token":"tok"}`))
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		errStr  string
		wantSub string // "" = expect no suggestion
	}{
		{"missing .context", `{{'x/' + $.realIP}}: not parsed yet`, "$.context.realIP"},
		{"extra .context", `{{$.context.maxmind_account}}: not parsed yet`, "$.maxmind_account"},
		{"correct path → no hint", `{{$.context.realIP}}: not parsed yet`, ""},
		{"field nowhere → no hint", `{{$.nonexistent}}: not parsed yet`, ""},
	}
	for _, c := range cases {
		got := pathHint(root, c.errStr)
		if c.wantSub == "" {
			if got != "" {
				t.Errorf("%s: expected no hint, got %q", c.name, got)
			}
			continue
		}
		if !strings.Contains(got, c.wantSub) {
			t.Errorf("%s: expected hint containing %q, got %q", c.name, c.wantSub, got)
		}
	}
}

func TestFindFieldPath_AmbiguousReturnsEmpty(t *testing.T) {
	// same key at two paths → ambiguous → no suggestion (would be a guess)
	root, _ := ajson.Unmarshal([]byte(`{"a":{"id":"1"},"b":{"id":"2"}}`))
	if got := findFieldPath(root, "id"); got != "" {
		t.Errorf("ambiguous field should yield no path, got %q", got)
	}
}
