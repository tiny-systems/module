package utils

import (
	"context"
	"fmt"
	"strings"

	"github.com/goccy/go-json"
	"github.com/tiny-systems/ajson"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/pkg/evaluator"
	"github.com/tiny-systems/module/pkg/jsonschemagenerator"
)

// SimulatePortData generates mock data for a port based on its schema and incoming edges.
// If runtimeData is provided, it will overlay actual trace data on top of the simulated data.
// runtimeData is a map of port full names to their JSON-encoded data from trace spans.
func SimulatePortData(ctx context.Context, nodesMap map[string]v1alpha1.TinyNode, inspectPortFullName string, runtimeData map[string][]byte) (interface{}, error) {
	_, _, edgeConfigMap, portSchemaMap, _, err := GetFlowMaps(nodesMap)
	if err != nil {
		return nil, err
	}

	return SimulatePortDataFromMaps(ctx, portSchemaMap, edgeConfigMap, inspectPortFullName, runtimeData)
}

// SimulatePortDataFromMaps generates mock data using pre-computed flow maps.
// This is useful when you already have the maps from GetFlowMaps and want to avoid recomputing them.
func SimulatePortDataFromMaps(
	ctx context.Context,
	portSchemaMap map[string]*ajson.Node,
	edgeConfigMap map[string][]Destination,
	inspectPortFullName string,
	runtimeData map[string][]byte,
) (interface{}, error) {
	var (
		visited = make(map[string]struct{})
		cache   = make(map[string]interface{})
	)

	var inspect func(ctx context.Context, portName string) (interface{}, error)

	// generator callback - resolves port references in schema
	generatorCallback := func(inspectPort string) jsonschemagenerator.Callback {
		return func(n *ajson.Node) (result interface{}, replace bool, err error) {
			// Check if schema has a port property (for configurable definitions)
			portStr, err := jsonschemagenerator.GetStrKey("port", n)
			if err != nil || portStr == "" {
				return nil, false, nil
			}

			_, p := ParseFullPortName(portStr)
			if strings.HasPrefix(p, "_") {
				// system ports like _settings, _control
				return nil, false, nil
			}

			pathStr, _ := jsonschemagenerator.GetStrKey("path", n)

			var results []interface{}

			// Look for outgoing edge destinations
			if destinations, ok := edgeConfigMap[portStr]; ok {
				for _, dest := range destinations {
					if dest.Name == inspectPort {
						continue
					}
					if len(dest.Configuration) == 0 {
						// edge not configured, skip
						continue
					}

					// Simulate the data from the edge's source
					edgeData, err := inspect(ctx, dest.Name)
					if err != nil {
						return nil, false, err
					}

					edgeDataB, err := json.Marshal(edgeData)
					if err != nil {
						return nil, false, err
					}

					edgeDataNode, err := ajson.Unmarshal(edgeDataB)
					if err != nil {
						return nil, false, err
					}

					// Evaluate the edge configuration
					e := evaluator.NewEvaluator(func(expression string) (interface{}, error) {
						if expression == "" {
							return nil, fmt.Errorf("expression is empty")
						}
						if expression == "$" {
							// root path - return the whole data
							return edgeDataNode.Unpack()
						}
						jsonPathResult, err := ajson.Eval(edgeDataNode, expression)
						if err != nil {
							return nil, err
						}
						return jsonPathResult.Unpack()
					})

					data, err := e.Eval(dest.Configuration)
					if err != nil {
						return nil, true, err
					}
					// When the edge author provided an explicit schema for
					// the configurable target field (configure_edge's
					// `schema` param), use it to fill typed defaults for
					// any field that the upstream sim couldn't resolve
					// concretely. Without this, chains that pass user
					// data through configurable fields (e.g. context
					// through a router) produce nulls downstream and
					// trip the strict validator on real-but-unprovable
					// flows.
					if len(dest.Schema) > 0 {
						data = fillTypedDefaults(data, dest.Schema)
					}
					results = append(results, data)
				}
			}

			if len(results) == 0 {
				return nil, false, nil
			}
			result = results[0]

			if pathStr == "$" || pathStr == "" {
				return result, true, nil
			}

			// Evaluate JSON path for nested results
			finalJson, err := json.Marshal(result)
			if err != nil {
				return nil, false, err
			}
			finalNode, err := ajson.Unmarshal(finalJson)
			if err != nil {
				return nil, true, err
			}
			finalNode, err = ajson.Eval(finalNode, pathStr)
			if err != nil {
				return nil, true, err
			}
			result, err = finalNode.Unpack()
			if err != nil {
				return nil, true, err
			}

			// When a shared definition is used as a single item (e.g. OutMessage.Item)
			// but the path points to an array field (e.g. $.array from InMessage.Array),
			// unwrap the first element. The definition type is "object" but the path
			// returned an array — take [0] to get a representative item.
			if arr, ok := result.([]interface{}); ok && len(arr) > 0 {
				defType, _ := jsonschemagenerator.GetStrKey("type", n)
				if defType != "array" {
					result = arr[0]
				}
			}

			return result, true, nil
		}
	}

	inspect = func(ctx context.Context, portName string) (result interface{}, err error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if _, ok := visited[portName]; ok {
			if cached, ok := cache[portName]; ok {
				if cached == nil {
					return nil, fmt.Errorf("cached value for %s is nil", portName)
				}
				return cached, nil
			}
			return nil, fmt.Errorf("no cache value for visited port %s", portName)
		}

		defer func() {
			cache[portName] = result
		}()

		visited[portName] = struct{}{}

		schema, ok := portSchemaMap[portName]
		if !ok {
			schema = ajson.NullNode("")
		}

		// Generate mock data based on schema
		result, err = jsonschemagenerator.NewSchemaBasedDataGenerator().Generate(schema, generatorCallback(portName))
		if err != nil {
			return nil, err
		}

		// If runtime data is available for this port, layer it on top
		// of the simulated data. Trace data (from a real run) is the
		// source of truth and replaces wholesale; scenario data (user-
		// authored sample shapes) merges field-by-field so author-
		// supplied keys win without wiping chain-propagated keys.
		// The caller distinguishes via isScenarioPort — for now we
		// always merge object-shaped overlays and replace primitives.
		if runtimeData != nil {
			if entry, ok := runtimeData[portName]; ok {
				if len(entry) > 0 {
					var r interface{}
					if err := json.Unmarshal(entry, &r); err == nil && r != nil {
						result = mergeOverlay(result, r)
					}
				} else {
					result = nil
				}
			}
		}

		return result, nil
	}

	return inspect(ctx, inspectPortFullName)
}

// SimulatePortDataSimple generates mock data for a port without runtime/trace data.
// This is a convenience wrapper around SimulatePortData.
func SimulatePortDataSimple(ctx context.Context, nodesMap map[string]v1alpha1.TinyNode, inspectPortFullName string) (interface{}, error) {
	return SimulatePortData(ctx, nodesMap, inspectPortFullName, nil)
}

// fillTypedDefaults walks `data` against `schemaBytes` and substitutes
// schema-derived fake values for any null fields, using the same
// JSONSchemaBasedDataGenerator that powers port simulation elsewhere.
// Used to honor user-declared edge schemas during chain simulation
// when upstream values can't be resolved (chain passes through
// configurable fields).
//
// The schema as stored on an edge is typically a per-field bag
// ({context: {...}, foo: {...}}) — not a full JSON Schema document.
// We unmarshal it as ajson and let the generator do per-field mocking
// based on declared type / properties.
func fillTypedDefaults(data interface{}, schemaBytes []byte) interface{} {
	if len(schemaBytes) == 0 {
		return data
	}
	root, err := ajson.Unmarshal(schemaBytes)
	if err != nil {
		return data
	}
	asMap, ok := data.(map[string]interface{})
	if !ok {
		// Non-object payloads: if entirely null and the schema is a
		// single field declaration, generate from it.
		if data != nil {
			return data
		}
		mock, err := jsonschemagenerator.NewSchemaBasedDataGenerator().Generate(root, nil)
		if err != nil {
			return data
		}
		return mock
	}

	for _, k := range root.Keys() {
		fieldSchema, err := root.GetKey(k)
		if err != nil || fieldSchema == nil {
			continue
		}
		current, present := asMap[k]
		if !present || current == nil {
			mock, mockErr := jsonschemagenerator.NewSchemaBasedDataGenerator().Generate(fieldSchema, nil)
			if mockErr == nil {
				asMap[k] = mock
			}
			continue
		}
		// The eval may have produced an object with null leaves (e.g.
		// {context: {apiKey: null, ...}} when upstream couldn't resolve
		// templated fields). Recurse so those leaves get filled from
		// the declared property schema.
		asMap[k] = mergeMocksIntoObject(current, fieldSchema)
	}
	return asMap
}

// mergeOverlay layers overlay onto base. When both are objects, the result
// has every key from either side: overlay wins on key collisions (recursing
// into nested objects), and base preserves keys the overlay omits. For any
// non-object overlay, it replaces base wholesale — primitives and arrays
// substitute, they don't merge. This is the scenario/runtime semantic we
// want: a user-supplied scenario at port X declares "here's the shape of
// the fields I care about" without erasing other chain-propagated fields
// the sim could resolve on its own.
func mergeOverlay(base, overlay interface{}) interface{} {
	overlayMap, overlayOK := overlay.(map[string]interface{})
	if !overlayOK {
		return overlay
	}
	baseMap, baseOK := base.(map[string]interface{})
	if !baseOK {
		return overlayMap
	}
	out := make(map[string]interface{}, len(baseMap)+len(overlayMap))
	for k, v := range baseMap {
		out[k] = v
	}
	for k, ov := range overlayMap {
		if bv, present := out[k]; present {
			out[k] = mergeOverlay(bv, ov)
			continue
		}
		out[k] = ov
	}
	return out
}

// mergeMocksIntoObject walks an evaluated object against a JSON Schema
// node and substitutes generator mocks for any null leaves. Non-null
// leaves are preserved verbatim — we only fill where the chain
// simulator couldn't resolve a value.
func mergeMocksIntoObject(value interface{}, schemaNode *ajson.Node) interface{} {
	asMap, ok := value.(map[string]interface{})
	if !ok {
		if value == nil {
			mock, err := jsonschemagenerator.NewSchemaBasedDataGenerator().Generate(schemaNode, nil)
			if err == nil {
				return mock
			}
		}
		return value
	}
	props, err := schemaNode.GetKey("properties")
	if err != nil || props == nil {
		return asMap
	}
	for _, k := range props.Keys() {
		propSchema, err := props.GetKey(k)
		if err != nil || propSchema == nil {
			continue
		}
		current, present := asMap[k]
		if !present || current == nil {
			mock, mockErr := jsonschemagenerator.NewSchemaBasedDataGenerator().Generate(propSchema, nil)
			if mockErr == nil {
				asMap[k] = mock
			}
			continue
		}
		asMap[k] = mergeMocksIntoObject(current, propSchema)
	}
	return asMap
}

// GetPortHandles returns all handles for a node, properly formatted for the frontend
func GetPortHandles(node v1alpha1.TinyNode, includeSystemPorts bool) []map[string]interface{} {
	nodeMap := ApiNodeToMap(node, nil, false)
	nodeData, ok := nodeMap["data"].(map[string]interface{})
	if !ok {
		return nil
	}

	handles, ok := nodeData["handles"].([]interface{})
	if !ok {
		return nil
	}

	result := make([]map[string]interface{}, 0, len(handles))
	for _, handle := range handles {
		handleMap, ok := handle.(map[string]interface{})
		if !ok {
			continue
		}

		id, _ := handleMap["id"].(string)
		if !includeSystemPorts && (id == v1alpha1.SettingsPort || id == v1alpha1.ControlPort) {
			continue
		}

		result = append(result, handleMap)
	}

	return result
}

// GetAllPortHandles returns all handles including system ports
func GetAllPortHandles(node v1alpha1.TinyNode) []map[string]interface{} {
	return GetPortHandles(node, true)
}
