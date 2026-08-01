package module

import "testing"

func TestNameMatches(t *testing.T) {
	cases := []struct {
		name           string
		found, running string
		want           bool
	}{
		// The case that matters now: references are written bare, while
		// operators deployed before the change still answer prefixed.
		{"bare node, prefixed operator", "http-module-v0", "tinysystems-http-module-v0", true},
		// And the reverse: a node written before the change, on a module
		// installed without a publisher prefix (what tiny produces).
		{"prefixed node, bare operator", "tinysystems-http-module-v0", "http-module-v0", true},
		{"slash form", "tinysystems/http-module-v0", "tinysystems-http-module-v0", true},
		{"slash vs bare", "tinysystems/http-module-v0", "http-module-v0", true},
		{"identical", "http-module-v0", "http-module-v0", true},
		{"case insensitive", "HTTP-Module-v0", "http-module-v0", true},
		{"different publishers, same module", "acme-http-module-v0", "tinysystems-http-module-v0", false},

		// Majors are different modules — different operators, different
		// components. They must never satisfy each other.
		{"different major", "http-module-v1", "http-module-v0", false},
		{"different module", "http-module-v0", "js-module-v0", false},

		// Known ambiguity, accepted deliberately: with the prefix optional
		// nothing distinguishes a publisher prefix from part of a module name,
		// so a bare name matches any module ending in it. A module literally
		// called "module" would collide with "js-module". Ruling that out would
		// mean knowing every publisher at match time, inside a reconciler that
		// has only the node's name to go on — and the failure this replaces,
		// silently ignoring a node forever, is far worse than an improbable
		// collision.
		{"bare name matches anything ending in it", "module-v0", "js-module-v0", true},
		{"unrelated", "grpc-module-v0", "http-module-v0", false},

		{"empty found", "", "http-module-v0", false},
		{"empty running", "http-module-v0", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := NameMatches(c.found, c.running); got != c.want {
				t.Errorf("NameMatches(%q, %q) = %v, want %v", c.found, c.running, got, c.want)
			}
		})
	}
}
