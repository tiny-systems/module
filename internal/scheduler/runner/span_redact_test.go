package runner

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tiny-systems/module/pkg/redact"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// recordingSpan captures what was written to it. Only the event carrying the
// payload matters here.
type recordingSpan struct {
	// Embedding the interface keeps this to the two methods the test cares
	// about; the rest would panic if called, which is the honest outcome for a
	// stub asked to do something the test never claimed it does.
	trace.Span
	events []string
}

func (r *recordingSpan) AddEvent(_ string, opts ...trace.EventOption) {
	cfg := trace.NewEventConfig(opts...)
	for _, a := range cfg.Attributes() {
		if a.Key == attribute.Key("payload") {
			r.events = append(r.events, a.Value.AsString())
		}
	}
}

// Credentials belong on input ports rather than in settings — that is the
// design. It also means a key crosses a port as ordinary data, and this is
// where ordinary data gets written down: into a trace the canvas renders,
// get_trace_detail returns, and any agent with an MCP connection can read.
//
// Found by firing a real flow: 108 characters of Anthropic key, in two spans,
// with no redaction anywhere in the path.
func TestPortPayloadNeverCarriesACredentialIntoASpan(t *testing.T) {
	c := &Runner{}
	span := &recordingSpan{}

	c.addSpanPortData(span, `{"apiKey":"sk-ant-api03-`+strings.Repeat("A", 80)+`","alert":"disk full"}`)

	if len(span.events) != 1 {
		t.Fatalf("%d payload events", len(span.events))
	}
	got := span.events[0]
	if strings.Contains(got, "sk-ant-api03-AAAA") {
		t.Fatal("the key reached the span")
	}
	if !strings.Contains(got, "disk full") {
		t.Errorf("the rest of the payload was lost, which makes the trace useless: %s", got)
	}
}

// A key with no field name to give it away — inside prose, or under a field
// nobody thought to call secret — is still a key. Masking by shape is what
// makes that hold; masking by field name would not.
func TestACredentialIsMaskedWhereverItSits(t *testing.T) {
	c := &Runner{}
	span := &recordingSpan{}

	c.addSpanPortData(span, `{"userMessage":"use ghp_`+strings.Repeat("b", 30)+` to clone it"}`)

	if strings.Contains(span.events[0], "ghp_bbbb") {
		t.Fatalf("a credential in prose reached the span: %s", span.events[0])
	}
}

// An ordinary payload must survive untouched: a trace nobody can read is a
// trace nobody uses, and the next person will turn the masking off.
func TestOrdinaryPayloadsAreUnchanged(t *testing.T) {
	c := &Runner{}
	span := &recordingSpan{}

	payload := `{"rows":[{"name":"api-1","restarts":0}],"count":1}`
	c.addSpanPortData(span, payload)

	if span.events[0] != payload {
		t.Fatalf("payload was altered:\n want %s\n got  %s", payload, span.events[0])
	}
}

// The component declares which field holds a credential — format:"password",
// the same tag that makes the editor render it masked. Using that beats both
// heuristics: it catches a secret whose field name is innocent and whose value
// looks like nothing in particular.
func TestDeclaredSecretsNeverReachASpan(t *testing.T) {
	type req struct {
		Passphrase string `json:"passphrase" format:"password"`
		Question   string `json:"question"`
	}

	safe, changed := redact.Declared(req{Passphrase: "correct horse battery staple", Question: "which pods are down"})
	if !changed {
		t.Fatal("the declared field was not masked before marshalling")
	}
	encoded, err := json.Marshal(safe)
	if err != nil {
		t.Fatal(err)
	}

	c := &Runner{}
	span := &recordingSpan{}
	c.addSpanPortData(span, string(encoded))

	got := span.events[0]
	if strings.Contains(got, "correct horse") {
		t.Fatalf("a declared secret reached the span: %s", got)
	}
	if !strings.Contains(got, "which pods are down") {
		t.Errorf("the rest of the payload was lost: %s", got)
	}
}
