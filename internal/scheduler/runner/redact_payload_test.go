package runner

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tiny-systems/module/api/v1alpha1"
)

func nodeWithSchema(schema string) v1alpha1.TinyNode {
	return v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Ports: []v1alpha1.TinyNodePortConfig{
				{Port: "_settings", Schema: []byte(schema)},
			},
		},
	}
}

func runnerWithNode(node v1alpha1.TinyNode) *Runner {
	r := NewRunner(nil)
	return r.SetNode(node)
}

// The leak this closes.
//
// A credential reaches a flow as port DATA, inside a field whose shape the user
// authored — so there is no Go struct tag for redact.Declared to read, and a
// corporate password is not shaped like a provider key for the shape pass to
// spot. Before this, the value reached the trace verbatim.
func TestPayloadRedactsAUserDeclaredSecretNoHeuristicWouldCatch(t *testing.T) {
	const secret = "hunter2-corporate-vpn-password"

	// A field name deliberately outside every heuristic: `z` matches no
	// credential-name regex, and the value matches no provider's key shape.
	r := runnerWithNode(nodeWithSchema(`{
	  "properties": {
	    "context": {
	      "properties": {"z": {"type": "string", "format": "password"}}
	    }
	  }
	}`))

	payload := []byte(`{"context":{"z":"` + secret + `","region":"eu-west-1"}}`)
	got := string(r.redactPayload(payload))

	if strings.Contains(got, secret) {
		t.Fatalf("declared credential reached the span verbatim: %s", got)
	}
	if !strings.Contains(got, "eu-west-1") {
		t.Errorf("ordinary data was eaten: %s", got)
	}
}

// Layer 3, the defence-in-depth half. redact.Secrets was previously applied only
// to port CONFIGURATION, so a payload carrying a conventionally-named credential
// reached traces intact even though the same name in routing was caught.
func TestPayloadRedactsConventionalNamesEvenWhenNothingIsDeclared(t *testing.T) {
	const secret = "totally-not-a-real-token"

	r := runnerWithNode(v1alpha1.TinyNode{}) // no schemas at all

	payload := []byte(`{"context":{"apiKey":"` + secret + `","note":"keep me"}}`)
	got := string(r.redactPayload(payload))

	if strings.Contains(got, secret) {
		t.Fatalf("conventionally-named credential reached the span: %s", got)
	}
	if !strings.Contains(got, "keep me") {
		t.Errorf("ordinary data was eaten: %s", got)
	}
}

// A declaration on ONE port has to protect the value on every hop. The
// credential is declared once — on the form, on the trigger — and then travels
// to nodes that declare nothing about it, which is exactly where it is most
// exposed.
func TestDeclarationsAreUnionedAcrossPorts(t *testing.T) {
	node := v1alpha1.TinyNode{Spec: v1alpha1.TinyNodeSpec{Ports: []v1alpha1.TinyNodePortConfig{
		{Port: "_settings", Schema: []byte(`{"properties":{"alpha":{"format":"password"}}}`)},
		{Port: "request", From: "x:out", Schema: []byte(`{"properties":{"beta":{"writeOnly":true}}}`)},
	}}}
	r := runnerWithNode(node)

	got := string(r.redactPayload([]byte(`{"alpha":"one","beta":"two","gamma":"three"}`)))
	for _, leaked := range []string{`"one"`, `"two"`} {
		if strings.Contains(got, leaked) {
			t.Errorf("declared secret %s survived: %s", leaked, got)
		}
	}
	if !strings.Contains(got, "three") {
		t.Errorf("undeclared field was redacted: %s", got)
	}
}

// An expression names a credential, it does not contain one — rewriting it would
// sever the wiring that depends on it.
func TestPayloadLeavesExpressionsIntact(t *testing.T) {
	r := runnerWithNode(nodeWithSchema(`{"properties":{"z":{"format":"password"}}}`))
	got := string(r.redactPayload([]byte(`{"z":"{{$.context.apiKey}}"}`)))
	if !strings.Contains(got, "{{$.context.apiKey}}") {
		t.Errorf("expression was rewritten: %s", got)
	}
}

// SetNode recomputes the cache. A node edited to declare a new secret must be
// protected on its very next hop, not after a restart.
func TestReDeclaringAfterSetNodeTakesEffect(t *testing.T) {
	r := runnerWithNode(v1alpha1.TinyNode{})
	if got := string(r.redactPayload([]byte(`{"z":"plain-value"}`))); !strings.Contains(got, "plain-value") {
		t.Fatalf("nothing declared yet, but the value was redacted: %s", got)
	}

	r.SetNode(nodeWithSchema(`{"properties":{"z":{"format":"password"}}}`))
	if got := string(r.redactPayload([]byte(`{"z":"plain-value"}`))); strings.Contains(got, "plain-value") {
		t.Errorf("new declaration did not take effect: %s", got)
	}
}

// Most payloads carry no credential and this runs on every hop, so the common
// path must not corrupt anything on its way through.
func TestPayloadWithoutSecretsRoundTrips(t *testing.T) {
	r := runnerWithNode(v1alpha1.TinyNode{})
	in := `{"context":{"pod":"web-1","count":3,"ok":true},"items":["a","b"]}`

	var want, got any
	if err := json.Unmarshal([]byte(in), &want); err != nil {
		t.Fatal(err)
	}
	out := r.redactPayload([]byte(in))
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v (%s)", err, out)
	}
	a, _ := json.Marshal(want)
	b, _ := json.Marshal(got)
	if string(a) != string(b) {
		t.Errorf("payload changed:\n got %s\nwant %s", b, a)
	}
}

func TestPayloadHandlesEmptyAndGarbage(t *testing.T) {
	r := runnerWithNode(v1alpha1.TinyNode{})
	if got := r.redactPayload(nil); got != nil {
		t.Errorf("nil in, %q out", got)
	}
	// Unparseable bytes cannot be inspected, and publishing an unexamined
	// payload is the failure this exists to prevent.
	if got := r.redactPayload([]byte("not json")); got != nil {
		t.Errorf("unparseable payload was published as %q", got)
	}
}
