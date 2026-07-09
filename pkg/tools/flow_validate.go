package tools

import "fmt"

// FlowIssues returns human-readable structural faults in a flow graph:
// broken edges (invalid expressions), fully-dangling nodes (no edges at
// all), and enabled-but-unconnected error ports (silent failure drops).
//
// It operates on the same []map[string]interface{} element form that
// read_project returns, so EVERY surface that uses this shared tool layer
// — the hosted platform, the local mcp-server, the desktop client — gets
// the same node-level validation regardless of which ProjectReader fed
// the graph. Empty slice means the graph is structurally sound.
//
// Node-level checks (dangling / error-port) are pure topology and need no
// port schema, so they work anywhere. Broken-edge entries are surfaced
// only when the ProjectReader already populated an edge's data.error (the
// hosted platform validates expressions; a bare local adapter may not) —
// so this never regresses a surface that lacks expression validation, it
// just reports whatever is present.
func FlowIssues(elements []map[string]interface{}) []string {
	connected := make(map[string]bool)    // node id touched by any edge (either end)
	errPortWired := make(map[string]bool) // node id whose "error" out-port has an edge

	var issues []string
	for _, el := range elements {
		if !elemIsEdge(el) {
			continue
		}
		src := mapStr(el, "source")
		tgt := mapStr(el, "target")
		if src != "" {
			connected[src] = true
		}
		if tgt != "" {
			connected[tgt] = true
		}
		if src != "" && mapStr(el, "sourceHandle") == "error" {
			errPortWired[src] = true
		}
		if e := elemDataStr(el, "error"); e != "" {
			issues = append(issues, fmt.Sprintf(
				"BROKEN EDGE %s: %s — fix the expression or delete the edge.",
				mapStr(el, "id"), e))
		}
	}

	for _, el := range elements {
		if elemIsEdge(el) {
			continue
		}
		id := mapStr(el, "id")
		if id == "" {
			continue
		}
		if !connected[id] {
			issues = append(issues, fmt.Sprintf(
				"DANGLING NODE %s (%s): no edges at all — wire it into the flow or delete it.",
				id, elemDataStr(el, "component")))
			continue // fully orphaned; don't also nag about its error port
		}
		for _, p := range elemOutPorts(el) {
			if p == "error" && !errPortWired[id] {
				issues = append(issues, fmt.Sprintf(
					"UNCONNECTED ERROR PORT on %s (%s): wire the error port to a recovery/debug node, or disable it (settings.enableErrorPort=false) — an unwired error port silently drops failures.",
					id, elemDataStr(el, "component")))
			}
		}
	}
	return issues
}

// elemIsEdge classifies an element map. Edges declare type "tinyEdge"
// (and carry source+target); nodes declare "tinyNode". When type is
// absent, fall back to topology.
func elemIsEdge(el map[string]interface{}) bool {
	switch mapStr(el, "type") {
	case "tinyEdge":
		return true
	case "tinyNode":
		return false
	}
	return mapStr(el, "source") != "" && mapStr(el, "target") != ""
}

func mapStr(m map[string]interface{}, key string) string {
	s, _ := m[key].(string)
	return s
}

// elemDataStr reads a string field off the element's nested "data" map.
func elemDataStr(el map[string]interface{}, key string) string {
	data, _ := el["data"].(map[string]interface{})
	if data == nil {
		return ""
	}
	s, _ := data[key].(string)
	return s
}

// elemOutPorts reads data.ports.out as a []string, tolerating the
// []interface{} form a JSON round-trip produces.
func elemOutPorts(el map[string]interface{}) []string {
	data, _ := el["data"].(map[string]interface{})
	if data == nil {
		return nil
	}
	ports, _ := data["ports"].(map[string]interface{})
	if ports == nil {
		return nil
	}
	switch v := ports["out"].(type) {
	case []string:
		return v
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, p := range v {
			if s, ok := p.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
