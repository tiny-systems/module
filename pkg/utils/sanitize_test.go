package utils

import (
	"regexp"
	"testing"
)

// rfc1123Subdomain is the same validation the Kubernetes API server
// applies to metadata.name / generateName. A sanitized resource name
// must satisfy it or the CRD create is rejected.
var rfc1123Subdomain = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func TestSanitizeResourceName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			// The playground regression: a long display name whose
			// 48-char suffix begins mid-separator. Before the fix this
			// returned "-webhook-is-hit-post-the-body-to-a-slack-channel"
			// (leading hyphen) and the API server rejected it.
			name: "long name truncating onto a separator",
			in:   "Playground a84afb40: When a webhook is hit, post the body to a Slack channel",
			want: "webhook-is-hit-post-the-body-to-a-slack-channel",
		},
		{
			name: "short clean name",
			in:   "What is the capital of France?",
			want: "what-is-the-capital-of-france",
		},
		{
			name: "leading and trailing punctuation",
			in:   "  -- Hello, World! --  ",
			want: "hello-world",
		},
		{
			name: "collapses runs of separators",
			in:   "a___b...c   d",
			want: "a-b-c-d",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeResourceName(tc.in)
			if got != tc.want {
				t.Fatalf("SanitizeResourceName(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if len([]rune(got)) > 48 {
				t.Fatalf("result %q exceeds 48 runes", got)
			}
			if !rfc1123Subdomain.MatchString(got) {
				t.Fatalf("result %q is not a valid RFC 1123 subdomain", got)
			}
		})
	}
}
