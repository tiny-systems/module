package utils

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/mitchellh/hashstructure/v2"
	"github.com/tiny-systems/module/api/v1alpha1"
)

// Flow element type constants
const (
	TinyNodeType = "tinyNode"
	TinyEdgeType = "tinyEdge"
)

// IsNode checks if an element map represents a node and returns its data.
func IsNode(n map[string]interface{}) (data map[string]interface{}, ok bool) {
	if n["data"] == nil || n["type"] != TinyNodeType {
		return
	}
	data, ok = n["data"].(map[string]interface{})
	return
}

// IsEdge checks if an element map represents an edge.
func IsEdge(n map[string]interface{}) bool {
	return n["type"] == TinyEdgeType &&
		n["source"] != nil &&
		n["target"] != nil &&
		n["targetHandle"] != nil &&
		n["sourceHandle"] != nil
}

// GetStr safely extracts a string from an interface{}.
func GetStr(i interface{}) string {
	if i == nil {
		return ""
	}
	if x, ok := i.(string); ok {
		return x
	}
	return ""
}

// GetInt safely extracts an int from an interface{}.
// Handles both float64 (from JSON unmarshal) and int types.
func GetInt(i interface{}) int {
	if i == nil {
		return 0
	}
	if x, ok := i.(float64); ok {
		return int(x)
	}
	if x, ok := i.(int); ok {
		return x
	}
	return 0
}

// GetFloat safely extracts a float64 from an interface{}.
func GetFloat(i interface{}) float64 {
	if i == nil {
		return 0
	}
	if x, ok := i.(float64); ok {
		return x
	}
	return 0
}

// GetBool safely extracts a bool from an interface{}.
func GetBool(i interface{}) bool {
	if i == nil {
		return false
	}
	if x, ok := i.(bool); ok {
		return x
	}
	return false
}

// ContainsStr checks if a string exists in a slice.
func ContainsStr(str string, sl []string) bool {
	for _, s := range sl {
		if str == s {
			return true
		}
	}
	return false
}

// GetFlowElementName generates a flow-scoped element name with hash prefix.
// If the element already has the hash prefix format, it extracts and re-prefixes.
func GetFlowElementName(projectName, flowName, element string) string {
	if element == "" {
		return ""
	}
	hashPrefix := GetFlowHashPrefix(projectName, flowName)

	// Check if element already has our hash prefix format (hash.suffix)
	// If so, extract the suffix; otherwise, use the element as-is as the suffix
	if len(element) > 9 && element[8] == '.' {
		// Element is already in format "XXXXXXXX.suffix", extract suffix
		return fmt.Sprintf("%s.%s", hashPrefix, element[9:])
	}

	// Element is a new ID (e.g., "new-node-00000000" or "router-000000")
	// Use the entire element as the suffix
	return fmt.Sprintf("%s.%s", hashPrefix, element)
}

// GenerateEdgeID creates a deterministic edge ID from source and target info.
// Format: {source}_{sourceHandle}-{target}_{targetHandle}
func GenerateEdgeID(source, sourceHandle, target, targetHandle string) string {
	return fmt.Sprintf("%s_%s-%s_%s", source, sourceHandle, target, targetHandle)
}

// IsEdgeID checks if an ID follows the edge ID format.
func IsEdgeID(id string) bool {
	if !strings.Contains(id, "_") || !strings.Contains(id, "-") {
		return false
	}
	_, _, _, _, err := ParseEdgeID(id)
	return err == nil
}

// ParseEdgeID parses an edge ID into its components.
// Format: {source}_{sourceHandle}-{target}_{targetHandle}
func ParseEdgeID(edgeID string) (source, sourceHandle, target, targetHandle string, err error) {
	// Split by hyphen first to get source_handle and target_handle parts
	parts := strings.SplitN(edgeID, "-", 2)
	if len(parts) != 2 {
		err = fmt.Errorf("invalid edge ID format: %s", edgeID)
		return
	}

	// Find the last underscore in the source part (source_sourceHandle)
	sourceLastUnderscore := strings.LastIndex(parts[0], "_")
	if sourceLastUnderscore == -1 {
		err = fmt.Errorf("invalid edge ID format: missing source handle separator in %s", edgeID)
		return
	}
	source = parts[0][:sourceLastUnderscore]
	sourceHandle = parts[0][sourceLastUnderscore+1:]

	// Find the last underscore in the target part (target_targetHandle)
	targetLastUnderscore := strings.LastIndex(parts[1], "_")
	if targetLastUnderscore == -1 {
		err = fmt.Errorf("invalid edge ID format: missing target handle separator in %s", edgeID)
		return
	}
	target = parts[1][:targetLastUnderscore]
	targetHandle = parts[1][targetLastUnderscore+1:]

	return
}

// RequestElementsMap is a map of element ID to element data.
type RequestElementsMap map[string]map[string]interface{}

// BuildRequestElementsMap parses flow JSON and builds a map of elements.
// It handles node/edge renaming based on flow naming conventions.
func BuildRequestElementsMap(projectName, flowName string, data []byte) (RequestElementsMap, error) {
	flow := make(map[string][]interface{})
	if err := json.Unmarshal(data, &flow); err != nil {
		return nil, err
	}

	elements := flow["elements"]
	if elements == nil {
		return nil, fmt.Errorf("data is invalid: no elements")
	}

	// Convert []interface{} to []map[string]interface{}
	elementMaps := make([]map[string]interface{}, 0, len(elements))
	for _, v := range elements {
		if vv, ok := v.(map[string]interface{}); ok {
			elementMaps = append(elementMaps, vv)
		}
	}

	return BuildRequestElementsMapFromSlice(projectName, flowName, elementMaps)
}

// BuildRequestElementsMapFromSlice builds a request elements map from a slice of element maps.
// It handles node/edge renaming and prepares _ports for each node.
func BuildRequestElementsMapFromSlice(projectName, flowName string, elements []map[string]interface{}) (RequestElementsMap, error) {
	if elements == nil {
		return nil, fmt.Errorf("data is invalid: nil elements")
	}

	result := make(RequestElementsMap)
	sharedNodes := make(map[string]struct{})

	// First pass: prepare nodes
	for _, vv := range elements {
		if vv["id"] == nil {
			continue
		}
		nodeData, ok := IsNode(vv)
		if !ok {
			continue
		}

		vv["_ports"] = []map[string]interface{}{} // append related edges later here

		// Check if node is shared with other flows
		sharedWithFlows := GetStr(nodeData["shared_with_flows"])
		if !ContainsStr(flowName, strings.Split(sharedWithFlows, ",")) {
			// Rename nodes that are not shared
			vv["id"] = GetFlowElementName(projectName, flowName, GetStr(vv["id"]))
		} else {
			sharedNodes[GetStr(vv["id"])] = struct{}{}
		}

		result[GetStr(vv["id"])] = vv
	}

	// Second pass: prepare edges
	for _, vv := range elements {
		if vv["id"] == nil {
			continue
		}
		if !IsEdge(vv) {
			continue
		}

		// Rename source and target only if those nodes are not shared
		if _, ok := sharedNodes[GetStr(vv["target"])]; !ok {
			vv["target"] = GetFlowElementName(projectName, flowName, GetStr(vv["target"]))
		}
		if _, ok := sharedNodes[GetStr(vv["source"])]; !ok {
			vv["source"] = GetFlowElementName(projectName, flowName, GetStr(vv["source"]))
		}

		// Generate deterministic edge ID based on renamed source/target
		vv["id"] = GenerateEdgeID(
			GetStr(vv["source"]),
			GetStr(vv["sourceHandle"]),
			GetStr(vv["target"]),
			GetStr(vv["targetHandle"]),
		)

		result[GetStr(vv["id"])] = vv
	}

	// Third pass: prepare _ports - list of all edges connected FROM each node
	for _, vv := range elements {
		if !IsEdge(vv) {
			continue
		}

		sourceName := GetStr(vv["source"])
		if _, ok := sharedNodes[sourceName]; !ok {
			sourceName = GetFlowElementName(projectName, flowName, sourceName)
		}

		if source, ok := result[sourceName]; ok {
			if sourcePorts, ok := source["_ports"].([]map[string]interface{}); ok {
				sourcePorts = append(sourcePorts, vv)
				source["_ports"] = sourcePorts
			}
		}
	}

	return result, nil
}

// UpdateEdgesFromRequest updates a node's edges based on request data.
// It preserves edges from other flows and adds/updates edges from the current flow.
func UpdateEdgesFromRequest(edges []v1alpha1.TinyNodeEdge, flowID, nodeID string, requestMap RequestElementsMap) ([]v1alpha1.TinyNodeEdge, error) {
	sourceNode, ok := requestMap[nodeID]
	if !ok {
		return nil, fmt.Errorf("element %s not found in request", nodeID)
	}

	sourceNodePorts, ok := sourceNode["_ports"].([]map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("source element %s contains no _ports", nodeID)
	}

	defer delete(sourceNode, "_ports")
	duplicates := make(map[string]struct{})
	newEdges := make([]v1alpha1.TinyNodeEdge, 0)

	// Preserve edges from other flows
	for _, edge := range edges {
		if edge.FlowID == flowID || edge.FlowID == "" {
			continue
		}

		edgeID := edge.ID
		if _, ok := duplicates[edgeID]; ok {
			continue
		}
		duplicates[edgeID] = struct{}{}
		newEdges = append(newEdges, edge)
	}

	// Add edges from current flow request
	for _, v := range sourceNodePorts {
		edgeID := GetStr(v["id"])
		if edgeID == "" {
			continue
		}
		if _, ok := duplicates[edgeID]; ok {
			continue
		}
		duplicates[edgeID] = struct{}{}

		newEdges = append(newEdges, v1alpha1.TinyNodeEdge{
			Port:   GetStr(v["sourceHandle"]),
			To:     fmt.Sprintf("%s:%s", GetStr(v["target"]), GetStr(v["targetHandle"])),
			ID:     edgeID,
			FlowID: flowID,
		})
	}

	// Sort edges for consistent ordering
	sort.Slice(newEdges, func(i, j int) bool {
		return newEdges[i].ID < newEdges[j].ID
	})

	return newEdges, nil
}

// PortConfig is an intermediate struct for assembling port configurations.
type PortConfig struct {
	From          string
	Name          string
	Source        bool
	Schema        []byte
	Configuration []byte
}

// UpdatePortConfigsFromRequest updates a node's port configurations based on request data.
// It handles settings ports and edge configurations.
func UpdatePortConfigsFromRequest(ports []v1alpha1.TinyNodePortConfig, flowID, nodeID string, requestMap RequestElementsMap) []v1alpha1.TinyNodePortConfig {
	elementInRequest, ok := requestMap[nodeID]
	if !ok {
		return ports
	}

	elementDataInRequest, ok := elementInRequest["data"].(map[string]interface{})
	if !ok {
		return ports
	}

	var portConfigs []v1alpha1.TinyNodePortConfig

	// Preserve port configs from other flows
	for _, port := range ports {
		if port.FlowID == flowID || port.FlowID == "" || port.From == "" {
			// Don't preserve internal port configs
			continue
		}
		portConfigs = append(portConfigs, port)
	}

	var handlePorts []PortConfig

	// Process handles (settings port)
	if handles, ok := elementDataInRequest["handles"].([]interface{}); ok {
		for _, handle := range handles {
			handleMap, ok := handle.(map[string]interface{})
			if !ok {
				continue
			}

			portName := GetStr(handleMap["id"])
			if portName != v1alpha1.SettingsPort {
				// Only settings handle should be saved as port config
				continue
			}

			s := handleMap["schema"]
			conf := handleMap["configuration"]
			typ := GetStr(handleMap["type"])

			handlePort := PortConfig{
				Name: portName,
			}
			if typ == "source" {
				handlePort.Source = true
			}
			if conf == nil {
				continue
			}

			handlePort.Configuration, _ = json.Marshal(conf)
			handlePort.Schema, _ = json.Marshal(s)
			handlePorts = append(handlePorts, handlePort)
		}
	}

	// Process edges targeting this node
	for _, edge := range requestMap {
		if !IsEdge(edge) {
			continue
		}
		if GetStr(edge["source"]) == nodeID {
			continue
		}
		if GetStr(edge["target"]) != nodeID {
			continue
		}

		edgeData, ok := edge["data"].(map[string]interface{})
		if !ok {
			continue
		}

		fromNode := GetStr(edge["source"])
		fromPort := GetStr(edge["sourceHandle"])
		toHandle := GetStr(edge["targetHandle"])

		conf := edgeData["configuration"]
		sc := edgeData["schema"]

		handlePort := PortConfig{
			From: GetPortFullName(fromNode, fromPort),
			Name: toHandle,
		}

		if conf != nil {
			handlePort.Configuration, _ = json.Marshal(conf)
		}
		if sc != nil {
			handlePort.Schema, _ = json.Marshal(sc)
		}

		handlePorts = append(handlePorts, handlePort)
	}

	// Convert to TinyNodePortConfig
	for _, hp := range handlePorts {
		if len(hp.Configuration) == 0 && len(hp.Schema) == 0 {
			continue
		}
		if hp.From == "" && hp.Source {
			continue
		}

		pc := v1alpha1.TinyNodePortConfig{
			From:          hp.From,
			Port:          hp.Name,
			Configuration: hp.Configuration,
			Schema:        hp.Schema,
			FlowID:        flowID,
		}
		portConfigs = append(portConfigs, pc)
	}

	// Sort for consistent ordering
	sort.Slice(portConfigs, func(i, j int) bool {
		hash1, _ := hashstructure.Hash(portConfigs[i], hashstructure.FormatV2, nil)
		hash2, _ := hashstructure.Hash(portConfigs[j], hashstructure.FormatV2, nil)
		return hash1 < hash2
	})

	return portConfigs
}
