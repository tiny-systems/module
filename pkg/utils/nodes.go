package utils

import (
	"fmt"
	"github.com/goccy/go-json"
	"github.com/microcosm-cc/bluemonday"
	"github.com/rs/zerolog/log"
	"github.com/tiny-systems/ajson"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/pkg/schema"
	"sort"
	"strconv"
	"strings"
)

func NodesToGraph(elements map[string]v1alpha1.TinyNode, flowName *string) ([]interface{}, []interface{}, error) {
	return NodesToGraphWithOptions(elements, flowName, true)
}

func NodesToGraphWithOptions(elements map[string]v1alpha1.TinyNode, flowName *string, minimal bool) ([]interface{}, []interface{}, error) {
	// Build port config map to look up edge configurations
	portConfigMap := make(map[string][]v1alpha1.TinyNodePortConfig)
	for _, node := range elements {
		for _, pc := range node.Spec.Ports {
			if pc.From == "" {
				continue // Skip non-edge configs
			}
			portFullName := GetPortFullName(node.Name, pc.Port)
			portConfigMap[portFullName] = append(portConfigMap[portFullName], pc)
		}
	}

	var (
		edges = make([]interface{}, 0)
		nodes = make([]interface{}, 0)
	)

	for nodeName, node := range elements {
		var (
			sharedWithThisFlow bool
			notThisFlow        bool
			blocked            bool
		)

		if flowName != nil {

			// check flow
			if checkSliceStr(*flowName, strings.Split(node.Annotations[v1alpha1.SharedWithFlowsAnnotation], ",")) {
				sharedWithThisFlow = true
			}

			if node.Labels[v1alpha1.FlowNameLabel] != *flowName {
				notThisFlow = true
			}

			if notThisFlow {
				if !sharedWithThisFlow {
					continue
				}
				blocked = true
			}
		}

		m := map[string]interface{}{
			"comment": bluemonday.StrictPolicy().Sanitize(node.Annotations[v1alpha1.NodeCommentAnnotation]),
		}

		if blocked {
			m["blocked"] = true
		}

		nodes = append(nodes, ApiNodeToMap(node, m, minimal))

		for _, edge := range node.Spec.Edges {
			data := map[string]interface{}{
				"valid": true,
			}

			// Look up edge configuration from target node's port configs
			targetPortConfigs, _ := portConfigMap[edge.To]
			sourcePortFullName := GetPortFullName(nodeName, edge.Port)
			_, targetPort := ParseFullPortName(edge.To)

			for _, pc := range targetPortConfigs {
				if pc.From != sourcePortFullName || pc.Port != targetPort {
					continue
				}
				if len(pc.Configuration) > 0 {
					data["configuration"] = json.RawMessage(pc.Configuration)
				}
				if len(pc.Schema) > 0 {
					data["schema"] = json.RawMessage(pc.Schema)
				}
				break
			}

			edgeMap, err := ApiEdgeToProtoMap(&node, &edge, data)
			if err != nil {
				continue
			}
			edges = append(edges, edgeMap)
		}
	}

	return nodes, edges, nil
}

func ApiNodeToMap(node v1alpha1.TinyNode, data map[string]interface{}, minimal bool) map[string]interface{} {

	if data == nil {
		data = map[string]interface{}{}
	}

	spin := 0
	m := map[string]interface{}{
		"type": "tinyNode",
	}

	position := map[string]interface{}{
		"x": 600,
		"y": 200,
	}

	if node.Annotations[v1alpha1.ComponentPosXAnnotation] != "" {
		position["x"], _ = strconv.Atoi(node.Annotations[v1alpha1.ComponentPosXAnnotation])
	}

	if node.Annotations[v1alpha1.ComponentPosYAnnotation] != "" {
		position["y"], _ = strconv.Atoi(node.Annotations[v1alpha1.ComponentPosYAnnotation])
	}

	if node.Annotations[v1alpha1.ComponentPosSpinAnnotation] != "" {
		spin, _ = strconv.Atoi(node.Annotations[v1alpha1.ComponentPosSpinAnnotation])
	}

	label := node.Status.Component.Description
	if node.Annotations[v1alpha1.NodeLabelAnnotation] != "" {
		label = node.Annotations[v1alpha1.NodeLabelAnnotation]
	}

	m["position"] = position

	defs := GetConfigurableDefinitions(node, nil)
	// configurable definitions collected

	// ok now we need to parse schemas and collect all configurable schemas and update definitions in others
	handlesMap := make(map[string]interface{})

	if node.Annotations[v1alpha1.NodeHandlesAnnotation] != "" {
		_ = json.Unmarshal([]byte(node.Annotations[v1alpha1.NodeHandlesAnnotation]), &handlesMap)
	}
	if handlesMap == nil {
		handlesMap = make(map[string]interface{})
	}

	for _, v := range node.Status.Ports {
		var typ = "target"
		if v.Source {
			typ = "source"
		}

		ma := map[string]interface{}{
			"id":               v.Name,
			"type":             typ,
			"position":         int(v.Position),
			"rotated_position": (int(v.Position) + spin) % 4,
			"label":            v.Label,
		}

		handlesMap[v.Name] = ma
		if minimal {
			continue
		}

		// Try to update schema with configurable definitions
		// If updating fails (e.g., schema has no $defs), use the original schema
		s, err := schema.UpdateWithDefinitions(v.Schema, defs)
		if err != nil {
			log.Debug().Err(err).Str("port", v.Name).Msg("unable to update conf definitions, using original schema")
			s = v.Schema
		}

		ma["schema"] = json.RawMessage(s)
		ma["configuration"] = json.RawMessage(v.Configuration)

		// Override configuration (NOT schema) from port-level Spec.Ports configs,
		// but ONLY for non-source ports. Source ports (like _control) are component-controlled
		// and their configuration should always reflect the component's current state.
		// Schema always comes from Status.Ports to ensure component updates are reflected.
		// User's configurable definitions from Spec.Ports are already merged via
		// GetConfigurableDefinitions + UpdateWithDefinitions above.
		if !v.Source {
			for _, pc := range node.Spec.Ports {
				if pc.From != "" || pc.Port != v.Name {
					// Skip edge configs (From!="") and other ports
					continue
				}
				ma["configuration"] = json.RawMessage(pc.Configuration)
			}
		}
	}

	bm := bluemonday.StrictPolicy()

	data["component"] = bm.Sanitize(node.Spec.Component)
	cd := bm.Sanitize(node.Status.Component.Description)
	if len(cd) > 0 {
		data["component_description"] = cd
	}

	data["component_info"] = bm.Sanitize(node.Status.Component.Info)
	//
	if !minimal {
		data["error"] = node.Status.Error
		data["status"] = bm.Sanitize(node.Status.Status)
		if node.Status.LastUpdateTime != nil {
			data["last_status_update"] = node.Status.LastUpdateTime.Unix()
		}
	}
	data["module"] = bm.Sanitize(node.Spec.Module)

	var keys []string
	for k := range handlesMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var handles []interface{}
	for _, k := range keys {
		handles = append(handles, handlesMap[k])
	}

	data["handles"] = handles
	data["spin"] = spin

	sw := bm.Sanitize(node.Annotations[v1alpha1.SharedWithFlowsAnnotation])
	if len(sw) > 0 {
		data["shared_with_flows"] = sw
	}

	dl := bm.Sanitize(node.Labels[v1alpha1.DashboardLabel])

	if len(dl) > 0 {
		data["dashboard"] = dl
	}

	data["flow_id"] = bm.Sanitize(node.Labels[v1alpha1.FlowNameLabel])
	data["label"] = bm.Sanitize(label)

	m["data"] = data
	m["id"] = node.Name

	return m
}

func GetConfigurableDefinitions(node v1alpha1.TinyNode, from *string) map[string]*ajson.Node {
	defs := map[string]*ajson.Node{}

	for _, v := range node.Status.Ports {
		_ = schema.CollectDefinitions(v.Schema, defs, schema.FilterShared)
	}

	//settings ports
	for _, port := range node.Spec.Ports {
		// prefer "from" node
		if port.From != "" && from != nil && port.From != *from {
			continue
		}
		_ = schema.CollectDefinitions(port.Schema, defs, schema.FilterConfigurable)
	}

	return defs
}

// MergeConfigurableDefinitions merges source node definitions into target node definitions.
// Target definitions always take precedence — they define what the target port accepts.
// Source definitions only fill in gaps (defs that the target doesn't have at all).
func MergeConfigurableDefinitions(targetDefs, sourceDefs map[string]*ajson.Node) map[string]*ajson.Node {
	for k, v := range sourceDefs {
		if _, ok := targetDefs[k]; !ok {
			targetDefs[k] = v
		}
	}
	return targetDefs
}


func checkSliceStr(str string, sl []string) bool {
	for _, s := range sl {
		if str == s {
			return true
		}
	}
	return false
}

func ApiEdgeToProtoMap(node *v1alpha1.TinyNode, edge *v1alpha1.TinyNodeEdge, data map[string]interface{}) (map[string]interface{}, error) {

	toParts := strings.Split(edge.To, ":")
	if len(toParts) != 2 {
		return nil, fmt.Errorf("edge's to property: `%s` is invalid", edge.To)
	}

	bm := bluemonday.StrictPolicy()

	toNode, toPort := toParts[0], toParts[1]
	return map[string]interface{}{
		"id":           edge.ID,
		"source":       bm.Sanitize(node.Name),
		"sourceHandle": edge.Port,
		"target":       toNode,
		"targetHandle": toPort,
		"type":         "tinyEdge",
		"label":        "x",
		"data":         data,
	}, nil
}

type Destination struct {
	Name string
	// if destination is a source port
	Configuration []byte
}

func GetFlowMaps(nodesMap map[string]v1alpha1.TinyNode) (map[string][]byte, map[string][]v1alpha1.TinyNodePortConfig, map[string][]Destination, map[string]*ajson.Node, map[string]struct{}, error) {

	var (
		// how one port connected with others, key it's a full port name
		// all configs (edges) we have to reach the destination
		destinationsMap = make(map[string][]Destination)

		// port schemas parsed by ajson, key it's a full port name
		portSchemaMap = make(map[string]*ajson.Node)

		//array of port it configs, key it's a full port name
		portConfigMap = map[string][]v1alpha1.TinyNodePortConfig{}

		//
		portStatusSchemaMap = map[string][]byte{}
	)

	// Sort node names for deterministic processing order.
	// Go map iteration is randomized — without sorting, destinationsMap
	// slice ordering varies between calls, causing SimulatePortDataFromMaps
	// to pick different results[0] for ports with multiple incoming edges.
	sortedNodeNames := make([]string, 0, len(nodesMap))
	for name := range nodesMap {
		sortedNodeNames = append(sortedNodeNames, name)
	}
	sort.Strings(sortedNodeNames)

	// ALL PORT CONFIGS
	for _, nodeName := range sortedNodeNames {
		node := nodesMap[nodeName]
		for _, pc := range node.Spec.Ports {
			port := GetPortFullName(node.Name, pc.Port)
			if portConfigMap[port] == nil {
				portConfigMap[port] = make([]v1alpha1.TinyNodePortConfig, 0)
			}
			portConfigMap[port] = append(portConfigMap[port], pc)
		}
	}

	// ALL TARGET PORTS OF THE FLOW
	targetPortsMap := make(map[string]struct{})
	for _, nodeName := range sortedNodeNames {
		node := nodesMap[nodeName]
		// each node
		for _, np := range node.Status.Ports {

			if np.Source {
				// definitions of source ports do not override anything
				continue
			}
			targetPortsMap[GetPortFullName(node.Name, np.Name)] = struct{}{}
		}
	}

	// MAIN LOOP
	for _, nodeName := range sortedNodeNames {
		node := nodesMap[nodeName]
		// each node
		// default port info
		for _, np := range node.Status.Ports {

			portIDName := GetPortFullName(node.Name, np.Name)
			portStatusSchemaMap[portIDName] = np.Schema

			s, err := ajson.Unmarshal(np.Schema)
			if err != nil {
				continue
			}
			// default values are come from statuses
			portSchemaMap[portIDName] = s
		}

		for _, edge := range node.Spec.Edges {
			// init slice
			if _, ok := destinationsMap[edge.To]; !ok {
				destinationsMap[edge.To] = make([]Destination, 0)
			}

			var configuration []byte
			// current node is edge's target
			targetID := GetPortFullName(node.Name, edge.Port)
			portConfigs, _ := portConfigMap[edge.To]

			for _, pc := range portConfigs {
				_, sourcePort := ParseFullPortName(edge.To)

				if pc.From == targetID && pc.Port == sourcePort {
					configuration = pc.Configuration
					break
				}
			}
			//
			destinationsMap[edge.To] = append(destinationsMap[edge.To], Destination{
				// in vueflow the source it is a destination
				Name:          targetID,
				Configuration: configuration,
			})
		}

		// all schema definitions from target ports (ports which are targets for edges)
		nodeSettingsDefinitions := map[string]*ajson.Node{}
		// all schema definitions from target ports (ports which are targets for edges)
		nodeTargetDefinitions := map[string]*ajson.Node{}

		// prepare port configs and collect definitions for source ports
		// apply settings port configs
		for _, pc := range node.Spec.Ports {
			if pc.From != "" {
				// skipping port setting which affect through the edges
				continue
			}
			if len(pc.Schema) == 0 {
				continue
			}
			s, err := ajson.Unmarshal(pc.Schema)
			if err != nil {
				continue
			}
			portID := GetPortFullName(node.Name, pc.Port)
			if refNode, _ := s.GetKey("$ref"); refNode == nil {
				continue
			}
			portSchemaMap[portID] = s
		}

		// collect node definitions from source ports with custom property `port` added
		for _, port := range node.Status.Ports {
			// default schema from status
			var schemaBytes = port.Schema

			// find schema from configs for given port — only use if complete ($ref present)
			for _, pc := range node.Spec.Ports {
				if pc.From == "" && pc.Port == port.Name && len(pc.Schema) > 0 {
					if specSchema, sErr := ajson.Unmarshal(pc.Schema); sErr == nil {
						if refNode, _ := specSchema.GetKey("$ref"); refNode != nil {
							schemaBytes = pc.Schema
						}
					}
					break
				}
			}
			if len(schemaBytes) == 0 {
				continue
			}
			// why we parse again?
			// @todo check this
			s, err := ajson.Unmarshal(schemaBytes)
			if err != nil {
				log.Error().Err(err).Msg("unable to unmarshal schema")
				continue
			}

			portID := GetPortFullName(node.Name, port.Name)
			defs, _ := s.GetKey("$defs")

			portSchemaMap[portID] = s

			if port.Source {
				// skip source ports
				continue
			}
			if !defs.IsObject() {
				// if port schema has no definitions at all
				continue
			}
			portDefinitions, err := defs.GetObject()
			if err != nil {
				return nil, nil, nil, nil, nil, err
			}

			for k, v := range portDefinitions {
				// Annotate configurable and shared definitions with port.
				// These definitions need edge evaluation via the generator
				// callback (trace back through edges to get data).
				// Non-tagged definitions (like root types Start, Settings,
				// or simple types String) should NOT be intercepted —
				// the generator iterates their properties normally.
				configurable, _ := schema.GetBool("configurable", v)
				shared, _ := schema.GetBool("shared", v)
				if configurable || shared {
					_ = v.AppendObject("port", ajson.StringNode("", portID))
				}
				nodeTargetDefinitions[k] = v
				if port.Name == v1alpha1.SettingsPort {
					nodeSettingsDefinitions[k] = v
				}
			}

			// Enrich definitions from Spec handle schemas (From == "", no $ref).
			// Import may provide richer definitions (e.g. Itemcontext with properties)
			// than the Status schema (which has bare interface{} types).
			// Use SetNode to modify in-place — replacePortDefs for target ports
			// uses nodeSettingsDefinitions, not nodeTargetDefinitions.
			for _, pc := range node.Spec.Ports {
				if pc.From != "" || pc.Port != port.Name || len(pc.Schema) == 0 {
					continue
				}
				specSchema, err := ajson.Unmarshal(pc.Schema)
				if err != nil {
					break
				}
				if refNode, _ := specSchema.GetKey("$ref"); refNode != nil {
					break // Complete schema already replaced Status in first pass
				}
				specDefs, _ := specSchema.GetKey("$defs")
				if specDefs == nil || !specDefs.IsObject() {
					break
				}
				specDefsMap, sErr := specDefs.GetObject()
				if sErr != nil {
					break
				}
				for sk, sv := range specDefsMap {
					existing, exists := nodeTargetDefinitions[sk]
					if !exists {
						continue
					}
					specProps, _ := sv.GetKey("properties")
					if specProps == nil || !specProps.IsObject() || len(specProps.Keys()) == 0 {
						continue
					}
					exProps, _ := existing.GetKey("properties")
					if exProps != nil && exProps.IsObject() && len(exProps.Keys()) > 0 {
						continue // Already has properties
					}
					// Carry port annotation to enriched definition
					if portNode, pErr := existing.GetKey("port"); pErr == nil && portNode != nil {
						if ps, pErr2 := portNode.Unpack(); pErr2 == nil {
							if portStr, ok := ps.(string); ok && portStr != "" {
								_ = sv.AppendObject("port", ajson.StringNode("", portStr))
							}
						}
					}
					// SetNode modifies in-place so portSchemaMap entry is also updated
					_ = existing.SetNode(sv)
					nodeTargetDefinitions[sk] = sv
				}
				break
			}

			// Also enrich definitions from edge schemas (Spec.Ports with From != "").
			// Edge schemas contain richer definitions from the user's edge configuration
			// (e.g., ItemContext with actual properties derived from upstream data mapping).
			// These override bare definitions from Status schema.
			for _, pc := range node.Spec.Ports {
				if pc.From == "" || pc.Port != port.Name || len(pc.Schema) == 0 {
					continue
				}
				edgeSchema, err := ajson.Unmarshal(pc.Schema)
				if err != nil {
					continue
				}
				edgeDefs, _ := edgeSchema.GetKey("$defs")
				if edgeDefs == nil || !edgeDefs.IsObject() {
					continue
				}
				edgeDefsMap, err := edgeDefs.GetObject()
				if err != nil {
					continue
				}
				for ek, ev := range edgeDefsMap {
					existing, exists := nodeTargetDefinitions[ek]
					if !exists {
						// New definition from edge — add port annotation if configurable/shared
						evConfigurable, _ := schema.GetBool("configurable", ev)
						evShared, _ := schema.GetBool("shared", ev)
						if evConfigurable || evShared {
							_ = ev.AppendObject("port", ajson.StringNode("", portID))
						}
						nodeTargetDefinitions[ek] = ev
						continue
					}
					// Merge properties from edge definitions into existing.
					// When multiple edges feed the same port, each may define
					// different Context properties (e.g., slack path has responseUrl,
					// k8s path has namespace). Merging ALL properties ensures
					// simulation is deterministic regardless of map iteration order.
					evProps, _ := ev.GetKey("properties")
					exProps, _ := existing.GetKey("properties")
					evHasProps := evProps != nil && evProps.IsObject() && len(evProps.Keys()) > 0
					exHasProps := exProps != nil && exProps.IsObject() && len(exProps.Keys()) > 0
					if evHasProps && !exHasProps {
						// First edge with properties: replace bare definition.
						// Carry over port annotation so simulation can trace backwards.
						if portNode, pErr := existing.GetKey("port"); pErr == nil && portNode != nil {
							if ps, pErr2 := portNode.Unpack(); pErr2 == nil {
								if portStr, ok := ps.(string); ok && portStr != "" {
									_ = ev.AppendObject("port", ajson.StringNode("", portStr))
								}
							}
						}
						nodeTargetDefinitions[ek] = ev
					} else if evHasProps && exHasProps {
						// Both have properties: merge missing ones from edge into existing.
						for _, propName := range evProps.Keys() {
							if _, pErr := exProps.GetKey(propName); pErr != nil {
								propNode, gErr := evProps.GetKey(propName)
								if gErr == nil && propNode != nil {
									_ = exProps.AppendObject(propName, propNode)
								}
							}
						}
					}
				}
			}
		}

		// Replace definitions in two passes:
		// 1. Target ports first — replace from nodeSettingsDefinitions
		// 2. Source ports second — replace from nodeTargetDefinitions
		//
		// This order matters because SetNode clones the value. Source ports
		// use nodeTargetDefinitions which points to target port nodes.
		// If a source port (e.g. query_result) is processed before its
		// corresponding target port (e.g. store) alphabetically, it would
		// get the unreplaced definition. Processing targets first ensures
		// nodeTargetDefinitions entries are updated before source ports read them.
		replacePortDefs := func(ports []v1alpha1.TinyNodePortStatus, replaceMap map[string]*ajson.Node) error {
			for _, port := range ports {
				portID := GetPortFullName(node.Name, port.Name)
				s, ok := portSchemaMap[portID]
				if !ok {
					continue
				}
				definitionsNode, err := s.GetKey("$defs")
				if err != nil {
					continue
				}
				if !definitionsNode.IsObject() {
					continue
				}
				defs, err := definitionsNode.GetObject()
				if err != nil {
					return err
				}
				for k, v := range defs {
					if replace, ok := replaceMap[k]; ok {
						if err = v.SetNode(replace); err != nil {
							return err
						}
					}
				}
				portSchemaMap[portID] = s
			}
			return nil
		}

		// Separate ports into target and source lists
		var targetPorts, sourcePorts []v1alpha1.TinyNodePortStatus
		for _, port := range node.Status.Ports {
			portID := GetPortFullName(node.Name, port.Name)
			if _, ok := targetPortsMap[portID]; ok {
				targetPorts = append(targetPorts, port)
			} else {
				sourcePorts = append(sourcePorts, port)
			}
		}

		// Pass 1: target ports — replace with settings definitions
		if err := replacePortDefs(targetPorts, nodeSettingsDefinitions); err != nil {
			return nil, nil, nil, nil, nil, err
		}
		// Pass 2: source ports — replace with target definitions (now updated by pass 1)
		if err := replacePortDefs(sourcePorts, nodeTargetDefinitions); err != nil {
			return nil, nil, nil, nil, nil, err
		}
	}
	return portStatusSchemaMap, portConfigMap, destinationsMap, portSchemaMap, targetPortsMap, nil
}

// ExportNodes exports flow nodes in a minimal format, omitting redundant data
// that can be derived at runtime:
// - Port schemas from Status.Ports (derived from component at runtime)
// - Edge schemas that equal target port default (re-derived at import)
// - Component description/info/version (lookup from component registry)
// - Handle labels/positions (from component definition)
// - Runtime error/status/last_update (runtime state)
func ExportNodes(nodesMap map[string]v1alpha1.TinyNode) ([]interface{}, error) {
	portStatusSchemaMap, portConfigMap, _, _, _, err := GetFlowMaps(nodesMap)
	if err != nil {
		return nil, err
	}

	var (
		edges = make([]interface{}, 0)
		nodes = make([]interface{}, 0)
	)

	for nodeName, node := range nodesMap {
		m := map[string]interface{}{}
		comment := bluemonday.StrictPolicy().Sanitize(node.Annotations[v1alpha1.NodeCommentAnnotation])

		if len(comment) > 0 {
			m["comment"] = comment
		}

		nodes = append(nodes, ApiNodeToMapMinimal(node, m))

		for _, edge := range node.Spec.Edges {
			data := map[string]interface{}{}

			var (
				edgeConfiguration []byte
				edgeSchema        []byte
				includeSchema     bool
			)

			sourcePortConfigs, _ := portConfigMap[edge.To]
			sourceNodeName, sourcePort := ParseFullPortName(edge.To)

			sourceNode, ok := nodesMap[sourceNodeName]
			if !ok {
				continue
			}

			from := GetPortFullName(nodeName, edge.Port)
			defs := GetConfigurableDefinitions(sourceNode, &from)

			for _, pc := range sourcePortConfigs {
				if pc.From != from || pc.Port != sourcePort {
					continue
				}

				edgeConfiguration = pc.Configuration

				// Only include schema if it differs from the default
				if len(pc.Schema) > 0 {
					defaultSchema := portStatusSchemaMap[edge.To]
					// Check if schema has custom configurable definitions
					if schema.HasCustomDefinitions(pc.Schema) {
						includeSchema = true
						edgeSchema = pc.Schema
					} else if !schema.SchemasEqual(pc.Schema, defaultSchema) {
						// Schema differs from default
						includeSchema = true
						edgeSchema = pc.Schema
					}
					// If schema equals default, omit it (will be re-derived at import)
				}

				if includeSchema && len(edgeSchema) > 0 {
					edgeSchema, _ = schema.UpdateWithDefinitions(edgeSchema, defs)
				}

				break
			}

			if len(edgeConfiguration) > 0 {
				data["configuration"] = json.RawMessage(edgeConfiguration)
			}
			if includeSchema && len(edgeSchema) > 0 {
				data["schema"] = json.RawMessage(edgeSchema)
			}

			edgeMap, err := ApiEdgeToProtoMap(&node, &edge, data)
			if err != nil {
				continue
			}
			edges = append(edges, edgeMap)
		}
	}

	return append(edges, nodes...), nil
}

// ApiNodeToMapMinimal converts a TinyNode to a map for minimal export format.
// It omits redundant data that can be derived at runtime:
// - schema and configuration from handles (use defaults from component)
// - component_info, component_description, module_version
// - error, status, last_status_update
func ApiNodeToMapMinimal(node v1alpha1.TinyNode, data map[string]interface{}) map[string]interface{} {
	if data == nil {
		data = map[string]interface{}{}
	}

	spin := 0
	m := map[string]interface{}{
		"type": "tinyNode",
	}

	position := map[string]interface{}{
		"x": 600,
		"y": 200,
	}

	if node.Annotations[v1alpha1.ComponentPosXAnnotation] != "" {
		position["x"], _ = strconv.Atoi(node.Annotations[v1alpha1.ComponentPosXAnnotation])
	}

	if node.Annotations[v1alpha1.ComponentPosYAnnotation] != "" {
		position["y"], _ = strconv.Atoi(node.Annotations[v1alpha1.ComponentPosYAnnotation])
	}

	if node.Annotations[v1alpha1.ComponentPosSpinAnnotation] != "" {
		spin, _ = strconv.Atoi(node.Annotations[v1alpha1.ComponentPosSpinAnnotation])
	}

	label := node.Status.Component.Description
	if node.Annotations[v1alpha1.NodeLabelAnnotation] != "" {
		label = node.Annotations[v1alpha1.NodeLabelAnnotation]
	}

	m["position"] = position

	defs := GetConfigurableDefinitions(node, nil)

	// Build handles map with minimal data - only settings port with custom config
	handlesMap := make(map[string]interface{})

	// Get stored handle positions from annotations
	var storedHandles map[string]interface{}
	if node.Annotations[v1alpha1.NodeHandlesAnnotation] != "" {
		_ = json.Unmarshal([]byte(node.Annotations[v1alpha1.NodeHandlesAnnotation]), &storedHandles)
	}

	// Build default handles from status (minimal - just structure)
	for _, v := range node.Status.Ports {
		var typ = "target"
		if v.Source {
			typ = "source"
		}

		ma := map[string]interface{}{
			"id":               v.Name,
			"type":             typ,
			"position":         int(v.Position),
			"rotated_position": (int(v.Position) + spin) % 4,
			"label":            v.Label,
		}

		handlesMap[v.Name] = ma
	}

	// Process settings port configurations from Spec.Ports
	for _, pc := range node.Spec.Ports {
		if pc.From != "" {
			// Skip edge configurations - handled separately
			continue
		}
		if pc.Port != v1alpha1.SettingsPort {
			// Only settings port should be saved in handles for minimal export
			continue
		}

		if ma, ok := handlesMap[pc.Port].(map[string]interface{}); ok {
			if len(pc.Configuration) > 0 {
				ma["configuration"] = json.RawMessage(pc.Configuration)
			}
			if len(pc.Schema) > 0 {
				updatedSchema, err := schema.UpdateWithDefinitions(pc.Schema, defs)
				if err == nil {
					ma["schema"] = json.RawMessage(updatedSchema)
				} else {
					ma["schema"] = json.RawMessage(pc.Schema)
				}
			}
		}
	}

	bm := bluemonday.StrictPolicy()

	data["component"] = bm.Sanitize(node.Spec.Component)
	data["module"] = bm.Sanitize(node.Spec.Module)
	data["component_description"] = bm.Sanitize(node.Status.Component.Description)
	data["component_info"] = bm.Sanitize(node.Status.Component.Info)

	// Omit runtime state: error, status, last_status_update
	// These are transient and not needed for import

	var keys []string
	for k := range handlesMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var handles []interface{}
	for _, k := range keys {
		handles = append(handles, handlesMap[k])
	}

	data["handles"] = handles
	data["spin"] = spin

	sw := bm.Sanitize(node.Annotations[v1alpha1.SharedWithFlowsAnnotation])
	if len(sw) > 0 {
		data["shared_with_flows"] = sw
	}

	dl := bm.Sanitize(node.Labels[v1alpha1.DashboardLabel])
	if len(dl) > 0 {
		data["dashboard"] = dl
	}

	data["flow_id"] = bm.Sanitize(node.Labels[v1alpha1.FlowNameLabel])
	data["label"] = bm.Sanitize(label)

	m["data"] = data
	m["id"] = node.Name

	return m
}
