package utils

import (
	"fmt"

	"github.com/goccy/go-json"
	"github.com/tiny-systems/module/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NodesFromGraphElements is the inverse of NodesToGraphWithOptions: it
// rebuilds full TinyNodes from flat export/graph elements. This is THE
// canonical reconstruction, shared by the platform's solution import and
// tiny's solution clone — one implementation so the two paths cannot
// drift.
//
// Faithfulness rules (mirrored from what the runtime actually reads):
//   - node handles carry both the port catalogue (→ Status.Ports, which the
//     editor renders) and the author's port configuration + schema
//     (→ Spec.Ports, target ports only — source port configs are
//     runtime-generated);
//   - an edge lands twice: on the source node's Spec.Edges (topology) and,
//     when it carries configuration or schema, as a From-qualified port
//     config on the TARGET node's Spec.Ports — where the public API and the
//     simulator read edge configs from;
//   - dashboard labels, positions, comments and shared-with-flows
//     annotations ride along.
//
// Returns the nodes keyed by their original element id, the ids in
// first-seen order, and a list of per-element errors. Callers decide
// whether errors are fatal (import: all-or-nothing) or reportable.
func NodesFromGraphElements(elements []map[string]interface{}) (map[string]*v1alpha1.TinyNode, []string, []string) {
	nodes := make(map[string]*v1alpha1.TinyNode)
	var order []string
	var errs []string

	// First pass — node elements
	for _, elem := range elements {
		data, ok := IsNode(elem)
		if !ok {
			continue
		}

		id, _ := elem["id"].(string)
		if id == "" {
			errs = append(errs, "node element has no id")
			continue
		}
		if _, exists := nodes[id]; exists {
			errs = append(errs, fmt.Sprintf("duplicate node id %q", id))
			continue
		}

		component := GetStr(data["component"])
		module := GetStr(data["module"])
		if component == "" || module == "" {
			errs = append(errs, fmt.Sprintf("node %q has no component/module", id))
			continue
		}

		flowName, _ := elem["flow"].(string)
		if flowName == "" {
			errs = append(errs, fmt.Sprintf("node %q has no flow", id))
			continue
		}

		node := &v1alpha1.TinyNode{
			ObjectMeta: metav1.ObjectMeta{
				Name:        id,
				Labels:      map[string]string{},
				Annotations: map[string]string{},
			},
			Spec: v1alpha1.TinyNodeSpec{
				Component: component,
				Module:    module,
			},
			Status: v1alpha1.TinyNodeStatus{
				Module: v1alpha1.TinyNodeModuleStatus{Name: module},
				Component: v1alpha1.TinyNodeComponentStatus{
					Description: GetStr(data["component_description"]),
					Info:        GetStr(data["component_info"]),
				},
			},
		}

		node.Labels[v1alpha1.FlowNameLabel] = flowName
		if d := GetStr(data["dashboard"]); d != "" {
			node.Labels[v1alpha1.DashboardLabel] = d
		}

		position, _ := elem["position"].(map[string]interface{})
		if position == nil {
			position = map[string]interface{}{"x": 0, "y": 0}
		}
		node.Annotations[v1alpha1.ComponentPosXAnnotation] = fmt.Sprintf("%d", GetInt(position["x"]))
		node.Annotations[v1alpha1.ComponentPosYAnnotation] = fmt.Sprintf("%d", GetInt(position["y"]))
		node.Annotations[v1alpha1.ComponentPosSpinAnnotation] = fmt.Sprintf("%d", GetInt(data["spin"]))

		if label := GetStr(data["label"]); label != "" {
			node.Annotations[v1alpha1.NodeLabelAnnotation] = label
		}
		if comment := GetStr(elem["comment"]); comment != "" {
			node.Annotations[v1alpha1.NodeCommentAnnotation] = comment
		}
		if shared := GetStr(data["shared_with_flows"]); shared != "" {
			node.Annotations[v1alpha1.SharedWithFlowsAnnotation] = shared
		}

		handles, _ := data["handles"].([]interface{})
		for _, h := range handles {
			handle, ok := h.(map[string]interface{})
			if !ok {
				continue
			}
			portID, _ := handle["id"].(string)
			if portID == "" {
				continue
			}
			isSource, _ := handle["type"].(string)

			var configBytes, schemaBytes []byte
			if config := handle["configuration"]; config != nil {
				if b, err := json.Marshal(config); err == nil {
					configBytes = b
				} else {
					errs = append(errs, fmt.Sprintf("node %q port %q: marshal config: %v", id, portID, err))
				}
			}
			if sch := handle["schema"]; sch != nil {
				if b, err := json.Marshal(sch); err == nil {
					schemaBytes = b
				} else {
					errs = append(errs, fmt.Sprintf("node %q port %q: marshal schema: %v", id, portID, err))
				}
			}

			node.Status.Ports = append(node.Status.Ports, v1alpha1.TinyNodePortStatus{
				Name:          portID,
				Label:         GetStr(handle["label"]),
				Position:      v1alpha1.Position(GetInt(handle["position"])),
				Source:        isSource == "source",
				Schema:        schemaBytes,
				Configuration: configBytes,
			})

			if isSource == "source" || (len(configBytes) == 0 && len(schemaBytes) == 0) {
				continue
			}
			node.Spec.Ports = append(node.Spec.Ports, v1alpha1.TinyNodePortConfig{
				Port:          portID,
				Configuration: configBytes,
				Schema:        schemaBytes,
			})
		}

		nodes[id] = node
		order = append(order, id)
	}

	// Second pass — edge elements.
	for _, elem := range elements {
		if !IsEdge(elem) {
			continue
		}

		source, _ := elem["source"].(string)
		sourceHandle, _ := elem["sourceHandle"].(string)
		target, _ := elem["target"].(string)
		targetHandle, _ := elem["targetHandle"].(string)
		flowName, _ := elem["flow"].(string)

		src, srcOK := nodes[source]
		tgt, tgtOK := nodes[target]
		if !srcOK || !tgtOK {
			errs = append(errs, fmt.Sprintf("edge %s:%s -> %s:%s references a node that is not in the payload", source, sourceHandle, target, targetHandle))
			continue
		}

		edgeID, _ := elem["id"].(string)
		if edgeID == "" {
			edgeID = fmt.Sprintf("%s_%s-%s_%s", source, sourceHandle, target, targetHandle)
		}

		src.Spec.Edges = append(src.Spec.Edges, v1alpha1.TinyNodeEdge{
			ID:     edgeID,
			Port:   sourceHandle,
			To:     target + ":" + targetHandle,
			FlowID: flowName,
		})

		edgeData, _ := elem["data"].(map[string]interface{})
		if edgeData == nil {
			continue
		}

		var configBytes, schemaBytes []byte
		if config := edgeData["configuration"]; config != nil {
			if b, err := json.Marshal(config); err == nil {
				configBytes = b
			} else {
				errs = append(errs, fmt.Sprintf("edge %s: marshal config: %v", edgeID, err))
			}
		}
		if sch := edgeData["schema"]; sch != nil {
			if b, err := json.Marshal(sch); err == nil {
				schemaBytes = b
			} else {
				errs = append(errs, fmt.Sprintf("edge %s: marshal schema: %v", edgeID, err))
			}
		}

		if len(configBytes) == 0 && len(schemaBytes) == 0 {
			continue
		}
		tgt.Spec.Ports = append(tgt.Spec.Ports, v1alpha1.TinyNodePortConfig{
			From:          source + ":" + sourceHandle,
			Port:          targetHandle,
			Configuration: configBytes,
			Schema:        schemaBytes,
			FlowID:        flowName,
		})
	}

	return nodes, order, errs
}
