package runner

import (
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/goccy/go-json"
	"github.com/tiny-systems/module/api/v1alpha1"
	m "github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/redact"
)

// s3Settings mirrors the shape that leaks today: a storage component embeds its
// connection block — access key and secret key included — in Settings, and
// returns the LIVE struct from Ports(). See modules/storage-module.
type s3Settings struct {
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region,omitempty"`
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
	UseSSL    bool   `json:"useSSL"`
	Bucket    string `json:"bucket,omitempty"`
	MaxBytes  int64  `json:"maxBytes"`
}

func readStatusFor(t *testing.T, ports []m.Port) v1alpha1.TinyNodeStatus {
	t.Helper()

	r := NewRunner(&mockComponent{ports: ports}).SetLogger(logr.Discard())

	var status v1alpha1.TinyNodeStatus
	if err := r.ReadStatus(&status); err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}
	return status
}

func portConfig(t *testing.T, status v1alpha1.TinyNodeStatus, name string) []byte {
	t.Helper()

	for _, p := range status.Ports {
		if p.Name == name {
			return p.Configuration
		}
	}
	t.Fatalf("port %q missing from status", name)
	return nil
}

// A component that returns its live settings from Ports() publishes whatever it
// currently holds into the node status, which anyone with get on the TinyNode
// can read. Credentials must not survive that trip in cleartext.
func TestReadStatus_RedactsCredentials(t *testing.T) {
	live := s3Settings{
		Endpoint:  "s3.amazonaws.com",
		Region:    "us-east-1",
		AccessKey: "AKIAIOSFODNN7EXAMPLE",
		SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		UseSSL:    true,
		Bucket:    "reports",
		MaxBytes:  1048576,
	}

	status := readStatusFor(t, []m.Port{
		{Name: v1alpha1.SettingsPort, Label: "Settings", Configuration: live},
	})

	conf := portConfig(t, status, v1alpha1.SettingsPort)
	raw := string(conf)

	for _, secret := range []string{live.AccessKey, live.SecretKey} {
		if strings.Contains(raw, secret) {
			t.Errorf("credential %q reached node status in cleartext:\n%s", secret, raw)
		}
	}

	var got map[string]interface{}
	if err := json.Unmarshal(conf, &got); err != nil {
		t.Fatalf("status configuration is not valid JSON: %v", err)
	}

	// Masked, not dropped. The editor renders a port's current values from this
	// status — a _control port reads them from nowhere else, and an unsaved
	// _settings port falls back to it — so removing the key would empty the
	// field in the form instead of showing that a credential is set.
	for _, key := range []string{"accessKey", "secretKey"} {
		v, ok := got[key]
		if !ok {
			t.Errorf("key %q was dropped from status; it must be masked so the settings form still renders the field", key)
			continue
		}
		if v != redact.Value {
			t.Errorf("%s = %v, want the mask %q", key, v, redact.Value)
		}
	}

	// Everything else must survive untouched.
	for key, want := range map[string]interface{}{
		"endpoint": "s3.amazonaws.com",
		"region":   "us-east-1",
		"useSSL":   true,
		"bucket":   "reports",
	} {
		if got[key] != want {
			t.Errorf("non-secret field %s = %v, want %v", key, got[key], want)
		}
	}

	// Numbers must not drift through a float64 round-trip — assert on the
	// literal in the emitted bytes, not on a re-decoded value.
	if !strings.Contains(raw, `"maxBytes":1048576`) {
		t.Errorf("maxBytes literal did not survive re-encoding:\n%s", raw)
	}
}

// Nested and array-held credentials leak the same way a top-level one does.
func TestReadStatus_RedactsNestedCredentials(t *testing.T) {
	type header struct {
		Name          string `json:"name"`
		Authorization string `json:"authorization"`
	}
	type auth struct {
		APIKey string `json:"apiKey"`
	}
	type nested struct {
		URL     string   `json:"url"`
		Auth    auth     `json:"auth"`
		Headers []header `json:"headers"`
	}

	live := nested{
		URL:     "https://api.example.com",
		Auth:    auth{APIKey: "sk-live-abcdef123456"},
		Headers: []header{{Name: "x-a", Authorization: "Bearer tok-987654"}},
	}

	status := readStatusFor(t, []m.Port{
		{Name: v1alpha1.SettingsPort, Label: "Settings", Configuration: live},
	})

	raw := string(portConfig(t, status, v1alpha1.SettingsPort))
	for _, secret := range []string{"sk-live-abcdef123456", "Bearer tok-987654"} {
		if strings.Contains(raw, secret) {
			t.Errorf("nested credential %q reached node status in cleartext:\n%s", secret, raw)
		}
	}
	if !strings.Contains(raw, "https://api.example.com") {
		t.Errorf("non-secret nested value was lost:\n%s", raw)
	}
}

// Redaction must be inert for the overwhelming majority of components, which
// hold no credentials at all — those bytes go out exactly as marshalled.
func TestRedactPortConfiguration_NonSecretRoundTripsByteIdentical(t *testing.T) {
	tests := []string{
		`{"delay":1000,"message":"hello","enabled":true}`,
		`{"b":2,"a":1,"z":{"nested":[1,2,3]},"big":9007199254740993}`,
		`{"routes":["a","b"],"count":0,"ratio":1.50,"nothing":null}`,
		`[]`,
		`{}`,
		`"bare string"`,
		`null`,
	}

	for _, in := range tests {
		t.Run(in, func(t *testing.T) {
			out := redactPortConfiguration([]byte(in))
			if string(out) != in {
				t.Errorf("payload was rewritten:\n got %s\nwant %s", out, in)
			}
		})
	}
}

// An expression names a secret, it does not contain one — rewriting it would
// sever the wiring that resolves it. Empty values have nothing to hide.
func TestRedactPortConfiguration_LeavesExpressionsAndEmptyAlone(t *testing.T) {
	in := `{"apiKey":"{{$.context.key}}","token":"","secret":"real-value"}`

	var got map[string]interface{}
	if err := json.Unmarshal(redactPortConfiguration([]byte(in)), &got); err != nil {
		t.Fatalf("invalid JSON out: %v", err)
	}

	if got["apiKey"] != "{{$.context.key}}" {
		t.Errorf("expression was rewritten: %v", got["apiKey"])
	}
	if got["token"] != "" {
		t.Errorf("empty value was replaced with a marker: %v", got["token"])
	}
	if got["secret"] != redact.Value {
		t.Errorf("actual credential was not masked: %v", got["secret"])
	}
}

// A port with no configuration must not gain one.
func TestRedactPortConfiguration_EmptyInput(t *testing.T) {
	if out := redactPortConfiguration(nil); out != nil {
		t.Errorf("nil input produced %q", out)
	}
	if out := redactPortConfiguration([]byte{}); len(out) != 0 {
		t.Errorf("empty input produced %q", out)
	}
}

// Unparseable bytes cannot be inspected, so they must not be published. This is
// unreachable in practice — json.Marshal produced the input — but the fallback
// has to fail closed rather than pass an unexamined payload through.
func TestRedactPortConfiguration_UnparseableFailsClosed(t *testing.T) {
	if out := redactPortConfiguration([]byte(`{"broken": `)); out != nil {
		t.Errorf("unparseable payload was published: %q", out)
	}
}
