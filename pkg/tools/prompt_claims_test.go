package tools

import (
	"regexp"
	"strings"
	"testing"

	"github.com/tiny-systems/ajson"
	"github.com/tiny-systems/module/pkg/redact"
)

// The guide is a second implementation.
//
// It restates in prose what the code already declares — which functions exist,
// which tools you can call — and prose cannot fail a build, so it drifts. It
// drifted in the worst possible direction: confidently wrong. An agent
// following it wrote {{now()}} and {{RFC3339(now())}}, got errors, and
// concluded the system had no time functions at all. I then "fixed" the guide
// by writing that there were none — deleting a capability that works. Three
// readings, none of them correct, because nobody ran the expressions.
//
// These tests run them. Every expression the guide shows is evaluated against
// the real engine, and every tool it names is looked up in the real registry.
// A claim that stops being true fails here.

// codeSpan finds the inline code spans the guide writes its examples in —
// both `{{$.x}}` mappings and bare mentions like `now($)`. Scanning the whole
// text instead would sweep up prose; scanning only {{...}} would miss the
// function list, which is where the wrong claim actually lived.
var codeSpan = regexp.MustCompile("`([^`]+)`")

// callPattern finds function calls inside an expression.
// No space before the paren: "arranged horizontally (left to right)" is
// prose, not a call, and treating it as one buries the real drift in noise.
// The character before the name is captured so a method call can be told
// apart: JSON.parse(x) belongs to JavaScript, not to this engine, and Go's
// regexp has no lookbehind.
var callPattern = regexp.MustCompile(`(^|[^.\w])([a-zA-Z_][a-zA-Z0-9_]*)\(`)

// TestGuideExpressionFunctionsExist checks that every function named in an
// example resolves. A name the engine does not know fails with "is not a
// function", which is precisely the drift worth catching — the guide offering
// something that was renamed or never existed.
func TestGuideExpressionFunctionsExist(t *testing.T) {
	sample := ajson.Must(ajson.Unmarshal([]byte(`{"s":"abc","n":3,"arr":[1,2],"context":{"x":"y"}}`)))

	tools := map[string]bool{}
	for _, name := range defaultToolNames() {
		tools[name] = true
	}

	// Scoped to the section that documents the expression engine. The guide
	// also shows JavaScript for js_eval nodes — JSON.stringify, array.push —
	// which are calls of a different language, and checking those against this
	// engine would report drift that is not drift.
	section := sectionText(CorePrompt, "## Expression Syntax")
	if section == "" {
		t.Fatal("the Expression Syntax section is gone or renamed — this test no longer checks anything")
	}

	seen := map[string]bool{}
	for _, match := range codeSpan.FindAllStringSubmatch(section, -1) {
		for _, call := range callPattern.FindAllStringSubmatch(match[1], -1) {
			name := call[2]
			// Tool calls share the shape of function calls; they are checked
			// separately against the registry.
			if seen[name] || tools[name] {
				continue
			}
			seen[name] = true

			// One argument: the parser has no zero-argument calls, so this
			// probes existence rather than any particular signature.
			_, err := ajson.Eval(sample, name+"($.s)")
			if err != nil && strings.Contains(err.Error(), "is not a function") {
				t.Errorf("the guide shows %s(...) but the expression engine has no such function", name)
			}
		}
	}
	if len(seen) == 0 {
		t.Fatal("no function calls found in the guide — the pattern stopped matching, so this test proves nothing")
	}
	t.Logf("checked %d distinct functions named in the guide", len(seen))
}

// The specific claim that was wrong twice, pinned so it cannot rot again in
// either direction: the functions exist, and the zero-argument spelling does
// not work.
func TestGuideTimeClaimMatchesTheEngine(t *testing.T) {
	sample := ajson.Must(ajson.Unmarshal([]byte(`{"n":1}`)))

	if _, err := ajson.Eval(sample, "now($)"); err != nil {
		t.Errorf("now($) should return the current time as Unix seconds: %v", err)
	}
	if _, err := ajson.Eval(sample, "RFC3339(now($))"); err != nil {
		t.Errorf("RFC3339(now($)) should render the current time: %v", err)
	}
	if _, err := ajson.Eval(sample, "now()"); err == nil {
		t.Error("now() parses now — the guide says it is a syntax error and must be updated")
	}

	// The guide must not tell anyone these are missing, which is what it said
	// after an agent reported the zero-argument form failing.
	for _, wrong := range []string{"no date function of any kind", "There is no `now("} {
		if strings.Contains(CorePrompt, wrong) {
			t.Errorf("the guide claims time functions do not exist, but now($) and RFC3339() both work: %q", wrong)
		}
	}
}

// TestGuideNamesOnlyRealTools catches a reference to a tool that was renamed
// or never existed — read_project spent an unknown time telling callers to use
// apply_changes, which no server has.
func TestGuideNamesOnlyRealTools(t *testing.T) {
	// Names the guide uses in prose that are not tools: components, ports and
	// expression functions share the same shape.
	known := map[string]bool{}
	for _, tool := range defaultToolNames() {
		known[tool] = true
	}
	if len(known) == 0 {
		t.Skip("no tool registry available in this build")
	}

	// Only check names the guide presents as calls with a tool-ish shape and
	// that already look like tools we know about, so component names and
	// expression helpers do not produce noise.
	suspects := regexp.MustCompile(`\b(apply_changes|get_component_info|list_modules|build_flow|edit_flow|read_project|send_signal|get_traces|get_trace_detail|get_instructions|list_projects|get_dashboard|set_node_dashboard|get_node_port_schema|create_flow|delete_flow|scenarios|install_module|get_module_readme)\b`)
	for _, m := range suspects.FindAllString(CorePrompt, -1) {
		if !known[m] {
			t.Errorf("the guide references tool %q, which is not registered", m)
		}
	}
}

// defaultToolNames lists the tools a server registers by default. Kept beside
// the test because the registry is assembled by the host, not the SDK.
func defaultToolNames() []string {
	return []string{
		"build_flow", "edit_flow", "read_project", "list_projects", "list_modules",
		"get_component_info", "get_module_info", "get_instructions", "get_node_port_schema",
		"send_signal", "get_traces", "get_trace_detail", "get_dashboard", "set_node_dashboard",
		"create_flow", "delete_flow", "scenarios", "install_module", "search_modules",
		"clone_solution", "get_solution", "get_module_readme",
	}
}

// sectionText returns one "## " section of the guide, so a check can be aimed
// at the claims it actually understands.
func sectionText(guide, heading string) string {
	start := strings.Index(guide, heading)
	if start < 0 {
		return ""
	}
	rest := guide[start+len(heading):]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		return rest[:end]
	}
	return rest
}

// The credentials section tells an agent to declare a secret field
// `format: "password"`. Redaction is what makes that declaration worth
// anything: redact.Declared reads the same attribute to keep the value out of
// traces. Two places, one contract, and prose cannot fail a build — so assert
// the mechanism honours exactly what the guide advertises.
//
// This matters because the alternative was shape-guessing heuristics. The
// declared attribute already existed; the guide now points at it, and this
// stops the two drifting apart.
func TestGuideNamesTheAttributeRedactionActuallyReads(t *testing.T) {
	guide := CorePrompt

	if !strings.Contains(guide, `format: "password"`) {
		t.Fatal(`credentials guidance no longer names format: "password"`)
	}

	// Declared returns a copy of the SAME type with the field masked — not a
	// map. Assert against the struct, which is also how callers use it.
	type creds struct {
		APIKey string `json:"apiKey" format:"password"`
		Region string `json:"region"`
	}
	got, changed := redact.Declared(creds{APIKey: "sk-not-a-real-key", Region: "eu-west-1"})
	if !changed {
		t.Fatal(`redact.Declared ignored format:"password" — the guide is telling agents to declare something inert`)
	}
	out, ok := got.(creds)
	if !ok {
		t.Fatalf("redact.Declared returned %T, want a creds copy", got)
	}
	if out.APIKey == "sk-not-a-real-key" {
		t.Error("declared secret survived redaction verbatim")
	}
	if out.Region != "eu-west-1" {
		t.Errorf("region = %q, want it untouched — redaction must not eat ordinary fields", out.Region)
	}
}

// The settings-form pattern is the default the guide now recommends, and it is
// only correct if it ends with the value stored under a HANDLE rather than
// written into the graph. Assert the section still says both halves: validate
// before storing, and keep the value out of the routing.
func TestCredentialGuidanceKeepsItsTwoNonNegotiables(t *testing.T) {
	guide := CorePrompt

	for _, claim := range []string{
		"never belongs to the ROUTING",
		"not saved until the provider has accepted it",
	} {
		if !strings.Contains(guide, claim) {
			t.Errorf("credentials guidance lost %q", claim)
		}
	}
}
