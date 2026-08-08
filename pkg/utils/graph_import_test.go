package utils

import (
	"encoding/json"
	"testing"

	"github.com/tiny-systems/module/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestGraphRoundTrip proves the property clone_solution depends on:
// NodesToGraph (non-minimal, the publish path) followed by
// NodesFromGraphElements reconstructs the parts of a TinyNode the runtime
// reads — spec ports with configuration AND schema, edges, the dashboard
// label — without loss.
func TestGraphRoundTrip(t *testing.T) {
	flow := "flow-a"
	src := v1alpha1.TinyNode{
		ObjectMeta: metav1.ObjectMeta{
			Name: "aaaa1111.mod-x.comp-a-abcde",
			Labels: map[string]string{
				v1alpha1.FlowNameLabel:  flow,
				v1alpha1.DashboardLabel: "true",
			},
			Annotations: map[string]string{
				v1alpha1.DashboardPageAnnotation: "Setup",
			},
		},
		Spec: v1alpha1.TinyNodeSpec{
			Component: "comp_a",
			Module:    "mod-x",
			Edges: []v1alpha1.TinyNodeEdge{{
				ID:     "aaaa1111.mod-x.comp-a-abcde_out-aaaa1111.mod-x.comp-b-fghij_in",
				Port:   "out",
				To:     "aaaa1111.mod-x.comp-b-fghij:in",
				FlowID: flow,
			}},
			Ports: []v1alpha1.TinyNodePortConfig{{
				Port:          "_settings",
				Configuration: []byte(`{"context":{"apiKey":""}}`),
				Schema:        []byte(`{"$defs":{"Context":{"configurable":true,"type":"object"}}}`),
			}},
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "_settings", Configuration: []byte(`{"context":{"apiKey":""}}`), Schema: []byte(`{"$defs":{"Context":{"configurable":true,"type":"object"}}}`)},
				{Name: "out", Source: true},
			},
		},
	}
	tgt := v1alpha1.TinyNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "aaaa1111.mod-x.comp-b-fghij",
			Labels:      map[string]string{v1alpha1.FlowNameLabel: flow},
			Annotations: map[string]string{},
		},
		Spec: v1alpha1.TinyNodeSpec{
			Component: "comp_b",
			Module:    "mod-x",
			Ports: []v1alpha1.TinyNodePortConfig{{
				From:          "aaaa1111.mod-x.comp-a-abcde:out",
				Port:          "in",
				Configuration: []byte(`{"context":"{{$.context}}","value":"{{$.x}}"}`),
				FlowID:        flow,
			}},
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{{Name: "in"}},
		},
	}

	nodeEls, edgeEls, err := NodesToGraphWithOptions(map[string]v1alpha1.TinyNode{
		src.Name: src,
		tgt.Name: tgt,
	}, &flow, false)
	if err != nil {
		t.Fatalf("NodesToGraph: %v", err)
	}

	// The export stores elements as generic maps stamped with the flow.
	var elements []map[string]interface{}
	for _, e := range append(nodeEls, edgeEls...) {
		b, _ := json.Marshal(e)
		var m map[string]interface{}
		_ = json.Unmarshal(b, &m)
		m["flow"] = flow
		elements = append(elements, m)
	}

	nodes, order, errs := NodesFromGraphElements(elements)
	if len(errs) > 0 {
		t.Fatalf("reconstruction errors: %v", errs)
	}
	if len(order) != 2 {
		t.Fatalf("want 2 nodes, got %d", len(order))
	}

	rsrc := nodes[src.Name]
	if rsrc == nil {
		t.Fatal("source node missing")
	}
	if rsrc.Labels[v1alpha1.DashboardLabel] != "true" {
		t.Error("dashboard label lost")
	}
	// The tab a widget sits on is part of the solution's design: losing it
	// collapses a deliberate setup/use split back into one page.
	if got := rsrc.Annotations[v1alpha1.DashboardPageAnnotation]; got != "Setup" {
		t.Errorf("dashboard page = %q, want Setup", got)
	}
	if len(rsrc.Spec.Edges) != 1 || rsrc.Spec.Edges[0].To != tgt.Name+":in" {
		t.Errorf("edge lost or wrong: %+v", rsrc.Spec.Edges)
	}
	var foundSettings bool
	for _, p := range rsrc.Spec.Ports {
		if p.Port == "_settings" {
			foundSettings = true
			if len(p.Schema) == 0 {
				t.Error("settings schema lost")
			}
			if len(p.Configuration) == 0 {
				t.Error("settings configuration lost")
			}
		}
	}
	if !foundSettings {
		t.Error("settings port config lost")
	}

	rtgt := nodes[tgt.Name]
	if rtgt == nil {
		t.Fatal("target node missing")
	}
	var foundEdgeCfg bool
	for _, p := range rtgt.Spec.Ports {
		if p.From == src.Name+":out" && p.Port == "in" {
			foundEdgeCfg = true
			if len(p.Configuration) == 0 {
				t.Error("edge configuration lost")
			}
		}
	}
	if !foundEdgeCfg {
		t.Error("From-qualified edge port config lost")
	}
}
