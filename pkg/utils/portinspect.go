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

		// If runtime data is available for this port, use it instead of mock data
		if runtimeData != nil {
			if entry, ok := runtimeData[portName]; ok && len(entry) > 0 {
				var r interface{}
				if err := json.Unmarshal(entry, &r); err == nil && r != nil {
					result = r
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
