package utils

import (
	"bytes"
	"context"
	"fmt"

	"github.com/goccy/go-json"
	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/tiny-systems/ajson"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/pkg/evaluator"
)

// ValidateEdge validates an edge's configuration against the target port's schema.
// It simulates the source port data and evaluates the edge configuration,
// then validates the result against the target port schema.
func ValidateEdge(ctx context.Context, nodesMap map[string]v1alpha1.TinyNode, sourcePortFullName, targetPortFullName string, edgeConfiguration []byte) error {
	return ValidateEdgeWithRuntimeData(ctx, nodesMap, sourcePortFullName, targetPortFullName, edgeConfiguration, nil)
}

// ValidateEdgeWithSchema validates an edge's configuration against a custom schema.
// Use this when the edge has its own schema (different from the port's native schema).
func ValidateEdgeWithSchema(ctx context.Context, nodesMap map[string]v1alpha1.TinyNode, sourcePortFullName string, edgeConfiguration []byte, edgeSchemaBytes []byte) error {
	// Get flow maps for port schemas and destinations (needed for simulation)
	_, _, destinationsMap, portSchemaMap, _, err := GetFlowMaps(nodesMap)
	if err != nil {
		return fmt.Errorf("cannot get flow maps: %v", err)
	}

	// Parse the edge's custom schema
	if len(edgeSchemaBytes) == 0 {
		return nil // No schema to validate against
	}
	edgeSchema, err := ajson.Unmarshal(edgeSchemaBytes)
	if err != nil {
		return fmt.Errorf("invalid edge schema: %v", err)
	}

	// Simulate port data for the source port
	portData, err := SimulatePortDataFromMaps(ctx, portSchemaMap, destinationsMap, sourcePortFullName, nil)
	if err != nil {
		return fmt.Errorf("cannot get port data: %v", err)
	}

	// Validate the edge using the custom schema
	return ValidateEdgeSchema(edgeSchema, portData, edgeConfiguration)
}

// ValidateEdgeWithRuntimeData validates an edge's configuration against the target port's schema,
// using runtime data when available instead of simulated mock data.
func ValidateEdgeWithRuntimeData(ctx context.Context, nodesMap map[string]v1alpha1.TinyNode, sourcePortFullName, targetPortFullName string, edgeConfiguration []byte, runtimeData map[string][]byte) error {
	// Get flow maps for port schemas and destinations
	_, _, destinationsMap, portSchemaMap, _, err := GetFlowMaps(nodesMap)
	if err != nil {
		return fmt.Errorf("cannot get flow maps: %v", err)
	}

	// Get target port schema
	targetPortSchema := portSchemaMap[targetPortFullName]
	if targetPortSchema == nil {
		// No schema to validate against - consider it valid
		return nil
	}

	// Simulate port data for the source port (using runtime data if available)
	portData, err := SimulatePortDataFromMaps(ctx, portSchemaMap, destinationsMap, sourcePortFullName, runtimeData)
	if err != nil {
		return fmt.Errorf("cannot get port data: %v", err)
	}

	// Validate the edge
	return ValidateEdgeSchema(targetPortSchema, portData, edgeConfiguration)
}

// ValidateEdgeSchema validates the edge configuration against the target port schema.
// This is the lower-level validation function that can be used when you already have
// the port schema and simulated port data (e.g., in the platform's graph builder).
// Returns nil if valid, or an error (possibly *jsonschema.ValidationError) if invalid.
func ValidateEdgeSchema(portSchema *ajson.Node, incomingPortData interface{}, edgeConfiguration []byte) error {
	if portSchema == nil {
		// No schema means any data is valid
		return nil
	}

	// Prepare schema bytes
	schemaBytes, err := ajson.Marshal(portSchema)
	if err != nil {
		return err
	}

	if len(schemaBytes) == 0 {
		schemaBytes = []byte("{}")
	}

	// Compile the JSON schema
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft7
	err = compiler.AddResource("schema.json", bytes.NewReader(schemaBytes))
	if err != nil {
		return fmt.Errorf("port schema is invalid: %v", err)
	}

	sch, err := compiler.Compile("schema.json")
	if err != nil {
		return fmt.Errorf("schema compile error: %v", err)
	}

	// Marshal incoming port data
	incomingPortBytes, err := json.Marshal(incomingPortData)
	if err != nil {
		return fmt.Errorf("unable to marshal simulated port data: %v", err)
	}

	portDataNode, err := ajson.Unmarshal(incomingPortBytes)
	if err != nil {
		return fmt.Errorf("unable to create ajson node: %v", err)
	}

	// Collect expression evaluation errors
	var evalErrors []string

	// Create evaluator for JSONPath expressions with error callback
	e := evaluator.NewEvaluator(func(expression string) (interface{}, error) {
		if expression == "" {
			return nil, fmt.Errorf("expression is empty")
		}
		if expression == "$" {
			// Don't need to eval - return the whole data
			return portDataNode.Unpack()
		}
		jsonPathResult, err := ajson.Eval(portDataNode, expression)
		if err != nil {
			return nil, err
		}
		resultUnpack, err := jsonPathResult.Unpack()
		if err != nil {
			return nil, err
		}
		return resultUnpack, nil
	}).WithErrorCallback(func(expression string, err error) {
		evalErrors = append(evalErrors, fmt.Sprintf("{{%s}}: %v", expression, err))
	})

	// Use empty object if no configuration
	if len(edgeConfiguration) == 0 {
		edgeConfiguration = []byte(`{}`)
	}

	// Evaluate the edge configuration (resolves JSONPath expressions)
	portDataConfig, err := e.Eval(edgeConfiguration)
	if err != nil {
		return fmt.Errorf("eval error: %v", err)
	}

	// Check if any expression evaluation errors occurred
	if len(evalErrors) > 0 {
		return fmt.Errorf("expression error: %s", evalErrors[0])
	}

	// Validate against schema
	err = sch.Validate(portDataConfig)
	if err != nil {
		// Extract cleaner error message from validation error
		if validationErr, ok := err.(*jsonschema.ValidationError); ok {
			return formatValidationError(validationErr)
		}
		return err
	}
	return nil
}

// formatValidationError extracts a clean error message from jsonschema.ValidationError
func formatValidationError(err *jsonschema.ValidationError) error {
	// Find the leaf error (most specific)
	leaf := err
	for len(leaf.Causes) > 0 {
		leaf = leaf.Causes[0]
	}

	// Build clean error message: "path: message"
	path := leaf.InstanceLocation
	if path == "" {
		path = "/"
	}
	return fmt.Errorf("%s: %s", path, leaf.Message)
}
