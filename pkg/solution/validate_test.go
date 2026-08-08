package solution

import (
	"strings"
	"testing"

	"github.com/tiny-systems/module/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// perRunControl is the control-port schema a fire-once trigger publishes.
const perRunControl = `{"$defs":{"Control":{"type":"object","properties":{"send":{"type":"boolean"},"context":{"type":"object"}}}},"$ref":"#/$defs/Control"}`

// scheduledControl is what a ticker/cron publishes: it runs itself.
const scheduledControl = `{"$defs":{"Control":{"type":"object","properties":{"start":{"type":"boolean"},"stop":{"type":"boolean"}}}},"$ref":"#/$defs/Control"}`

func widgetNode(name string, settings string, incoming map[string]string) v1alpha1.TinyNode {
	n := withControl(name, settings, incoming, perRunControl)
	return n
}

func withControl(name string, settings string, incoming map[string]string, control string) v1alpha1.TinyNode {
	n := v1alpha1.TinyNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{v1alpha1.DashboardLabel: "true"},
		},
	}
	if settings != "" {
		n.Spec.Ports = append(n.Spec.Ports, v1alpha1.TinyNodePortConfig{
			Port:          v1alpha1.SettingsPort,
			Configuration: []byte(settings),
		})
	}
	for port, cfg := range incoming {
		n.Spec.Ports = append(n.Spec.Ports, v1alpha1.TinyNodePortConfig{
			From:          "upstream:out",
			Port:          port,
			Configuration: []byte(cfg),
		})
	}
	n.Status.Ports = append(n.Status.Ports, v1alpha1.TinyNodePortStatus{
		Name:   v1alpha1.ControlPort,
		Source: true,
		Schema: []byte(control),
	})
	return n
}

// A credential on the same form as per-run inputs means pasting a key on
// every run, and a secret in every message payload.
func TestRejectsCredentialMergedIntoRunForm(t *testing.T) {
	nodes := map[string]v1alpha1.TinyNode{
		"f.mod.signal-1": widgetNode("f.mod.signal-1",
			`{"context":{"apiKey":"","question":"why?","conversationId":"ops"}}`, nil),
	}
	err := CheckWidgetShape(nodes)
	if err == nil {
		t.Fatal("merged credential form was accepted")
	}
	if !strings.Contains(err.Error(), "inject.config") {
		t.Errorf("error should point at the fix, got: %v", err)
	}
}

// A widget whose only field IS the credential is the correct shape.
func TestAcceptsDedicatedCredentialWidget(t *testing.T) {
	nodes := map[string]v1alpha1.TinyNode{
		"f.mod.signal-cfg": widgetNode("f.mod.signal-cfg", `{"context":{"apiKey":""}}`, nil),
		"f.mod.signal-ask": widgetNode("f.mod.signal-ask",
			`{"context":{"question":"why?","conversationId":"ops"}}`, nil),
	}
	if err := CheckWidgetShape(nodes); err != nil {
		t.Errorf("correct two-widget shape was rejected: %v", err)
	}
}

// {{$}} into a widget renders the whole pipeline state as a form.
func TestRejectsWholeMessageIntoWidget(t *testing.T) {
	nodes := map[string]v1alpha1.TinyNode{
		"f.mod.debug-1": widgetNode("f.mod.debug-1", "", map[string]string{
			"in": `{"context":"{{$}}"}`,
		}),
	}
	err := CheckWidgetShape(nodes)
	if err == nil {
		t.Fatal("whole-message passthrough into a widget was accepted")
	}
	if !strings.Contains(err.Error(), "{{$}}") {
		t.Errorf("error should name the offending mapping, got: %v", err)
	}
}

// The same passthrough is correct on a non-widget hop and must not be flagged.
func TestAllowsWholeMessageOnPlumbing(t *testing.T) {
	plumbing := widgetNode("f.mod.js-1", "", map[string]string{"request": `{"context":"{{$}}"}`})
	delete(plumbing.Labels, v1alpha1.DashboardLabel)
	if err := CheckWidgetShape(map[string]v1alpha1.TinyNode{"f.mod.js-1": plumbing}); err != nil {
		t.Errorf("passthrough on plumbing was rejected: %v", err)
	}
}

// A widget mapping a specific field is what we want.
func TestAcceptsNamedAnswerField(t *testing.T) {
	nodes := map[string]v1alpha1.TinyNode{
		"f.mod.debug-1": widgetNode("f.mod.debug-1", "", map[string]string{
			"in": `{"answer":"{{$.outputData.messages[0].content}}"}`,
		}),
	}
	if err := CheckWidgetShape(nodes); err != nil {
		t.Errorf("named answer field was rejected: %v", err)
	}
}

// A scheduled trigger's widget IS the settings form — the user fills it once
// and the flow runs itself, so a credential belongs there. Seven live
// solutions were wrongly flagged before this distinction existed.
func TestAllowsCredentialOnScheduledTrigger(t *testing.T) {
	nodes := map[string]v1alpha1.TinyNode{
		"f.mod.ticker-1": withControl("f.mod.ticker-1",
			`{"context":{"slackToken":"","labelSelector":"app=web","slackChannelId":"C123"}}`,
			nil, scheduledControl),
	}
	if err := CheckWidgetShape(nodes); err != nil {
		t.Errorf("scheduled trigger's settings form was rejected: %v", err)
	}
}

// A widget whose output feeds a persisted `config` port is a settings form —
// submitted once — regardless of its control port offering `send`. This is
// the very shape the credential rule recommends, and an earlier version of
// the rule rejected it.
func TestAllowsCredentialOnConfigFeedingWidget(t *testing.T) {
	n := widgetNode("f.mod.signal-cfg",
		`{"context":{"apiKey":"","conversationId":"default"}}`, nil)
	n.Spec.Edges = []v1alpha1.TinyNodeEdge{{
		ID:   "e1",
		Port: "out",
		To:   "f.mod.inject-1:config",
	}}
	if err := CheckWidgetShape(map[string]v1alpha1.TinyNode{n.Name: n}); err != nil {
		t.Errorf("settings widget feeding inject.config was rejected: %v", err)
	}
}

// The same widget wired straight into the flow is a per-run form again.
func TestRejectsCredentialWhenWidgetFeedsFlow(t *testing.T) {
	n := widgetNode("f.mod.signal-ask",
		`{"context":{"apiKey":"","question":"why?"}}`, nil)
	n.Spec.Edges = []v1alpha1.TinyNodeEdge{{ID: "e1", Port: "out", To: "f.mod.js-1:request"}}
	if err := CheckWidgetShape(map[string]v1alpha1.TinyNode{n.Name: n}); err == nil {
		t.Error("credential on a form wired into the flow was accepted")
	}
}
