package utils

import (
	"fmt"
	"github.com/goccy/go-json"
	"github.com/microcosm-cc/bluemonday"
	"github.com/rs/zerolog/log"
	"github.com/spyzhov/ajson"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/pkg/schema"
	"sort"
	"strconv"
	"strings"
)

func NodesToGraph(elements map[string]v1alpha1.TinyNode, flowName *string) ([]interface{}, []interface{}, error) {
	var (
		edges = make([]interface{}, 0)
		nodes = make([]interface{}, 0)
	)

	for _, node := range elements {
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

		nodes = append(nodes, ApiNodeToMap(node, m, true))

		var (
			edgeConfiguration []byte
			edgeSchema        []byte
		)

		for _, edge := range node.Spec.Edges {

			data := map[string]interface{}{
				"valid": true,
			}
			if len(edgeConfiguration) > 0 {
				data["configuration"] = json.RawMessage(edgeConfiguration)
			}
			if len(edgeSchema) > 0 {
				data["schema"] = json.RawMessage(edgeSchema)
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

		s, err := schema.UpdateWithDefinitions(v.Schema, defs)
		if err != nil {
			log.Error().Err(err).Msg("unable to update conf definitions")
			continue
		}

		ma["schema"] = json.RawMessage(s)
		ma["configuration"] = json.RawMessage(v.Configuration)

		for _, pc := range node.Spec.Ports {
			if pc.From != "" || pc.Port != v.Name {
				// do not update schema and configuration with edge data
				continue
			}
			ma["configuration"] = json.RawMessage(pc.Configuration)
			if len(pc.Schema) == 0 {
				continue
			}
			updatedConfigSchema, err := schema.UpdateWithDefinitions(pc.Schema, defs)
			if err != nil {
				log.Error().Err(err).Msg("unable to update definitions")
				continue
			}
			ma["schema"] = json.RawMessage(updatedConfigSchema)
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
	data["module_version"] = bm.Sanitize(node.Status.Module.Version)

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

	// ALL PORT CONFIGS
	for _, node := range nodesMap {
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
	for _, node := range nodesMap {
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
	for _, node := range nodesMap {
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
			// useful when settings apply custom schema
			portSchemaMap[GetPortFullName(node.Name, pc.Port)] = s // override schema from statuses by configs (edges)
		}

		// collect node definitions from source ports with custom property `port` added
		for _, port := range node.Status.Ports {
			// default schema from status
			var schemaBytes = port.Schema

			// find schema from configs for given port
			for _, pc := range node.Spec.Ports {
				if pc.From == "" && pc.Port == port.Name {
					schemaBytes = pc.Schema
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
				_ = v.AppendObject("port", ajson.StringNode("", portID))
				nodeTargetDefinitions[k] = v
				if port.Name == v1alpha1.SettingsPort {
					nodeSettingsDefinitions[k] = v
				}
			}
		}

		// all nodeSourceDefinitions ready
		// replace source port definitions with node's target ones
		for _, port := range node.Status.Ports {
			portID := GetPortFullName(node.Name, port.Name)
			s, ok := portSchemaMap[portID]
			if !ok {
				continue
			}

			// target port replace its definitions from source ports
			definitionsNode, err := s.GetKey("$defs")
			if err != nil {
				continue
			}
			if !definitionsNode.IsObject() {
				continue
			}
			defs, err := definitionsNode.GetObject()
			if err != nil {
				return nil, nil, nil, nil, nil, err
			}

			for k, v := range defs {
				var replaceMap map[string]*ajson.Node
				if _, ok := targetPortsMap[portID]; ok {
					// source port could be replaced with settings one
					replaceMap = nodeSettingsDefinitions
				} else {
					// not source (target) port could be replaced with source ones
					replaceMap = nodeTargetDefinitions
				}
				if replace, ok := replaceMap[k]; ok {
					if err = v.SetNode(replace); err != nil {
						return nil, nil, nil, nil, nil, err
					}
				}
			}
			// update schema for target port
			portSchemaMap[portID] = s
		}
	}
	return portStatusSchemaMap, portConfigMap, destinationsMap, portSchemaMap, targetPortsMap, nil
}

func ExportNodes(nodesMap map[string]v1alpha1.TinyNode) ([]interface{}, error) {

	portStatusSchemaMap, portConfigMap, edgeConfigMap, portSchemaMap, targetPortMap, err := GetFlowMaps(nodesMap)
	if err != nil {
		return nil, err
	}

	_ = edgeConfigMap
	_ = targetPortMap
	_ = portSchemaMap

	var (
		edges = make([]interface{}, 0)
		nodes = make([]interface{}, 0)
	)

	// collect status port schemas

	// MAIN LOOP
	for nodeName, node := range nodesMap {

		m := map[string]interface{}{}
		comment := bluemonday.StrictPolicy().Sanitize(node.Annotations[v1alpha1.NodeCommentAnnotation])

		if len(comment) > 0 {
			m["comment"] = comment
		}

		nodes = append(nodes, ApiNodeToMap(node, m, false))

		for _, edge := range node.Spec.Edges {
			data := map[string]interface{}{}

			var (
				edgeConfiguration []byte
				edgeSchema        []byte
			)

			// other node port configs
			sourcePortConfigs, _ := portConfigMap[edge.To]
			sourceNodeName, sourcePort := ParseFullPortName(edge.To)

			sourceNode, ok := nodesMap[sourceNodeName]
			if !ok {
				continue
			}
			var (
				from = GetPortFullName(nodeName, edge.Port)
				defs = GetConfigurableDefinitions(sourceNode, &from)
			)

			for _, pc := range sourcePortConfigs {
				if pc.From == from && pc.Port == sourcePort {
					edgeConfiguration = pc.Configuration
					edgeSchema = pc.Schema

					if len(edgeSchema) == 0 {
						// edge has no own configured schema, use schema from target port's status
						edgeSchema = portStatusSchemaMap[edge.To]
					}
					edgeSchema, _ = schema.UpdateWithDefinitions(edgeSchema, defs)

					break
				}
			}

			if len(edgeConfiguration) > 0 {
				data["configuration"] = json.RawMessage(edgeConfiguration)
			}
			if len(edgeSchema) > 0 {
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
