package utils

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"

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
	return ValidateEdgeWithSchemaAndRuntimeData(ctx, nodesMap, sourcePortFullName, edgeConfiguration, edgeSchemaBytes, nil)
}

// ValidateEdgeWithSchemaAndRuntimeData validates an edge's configuration against a custom schema,
// using runtime data when available instead of simulated mock data.
func ValidateEdgeWithSchemaAndRuntimeData(ctx context.Context, nodesMap map[string]v1alpha1.TinyNode, sourcePortFullName string, edgeConfiguration []byte, edgeSchemaBytes []byte, runtimeData map[string][]byte) error {
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

	// Simulate port data for the source port (using runtime data if available)
	portData, err := SimulatePortDataFromMaps(ctx, portSchemaMap, destinationsMap, sourcePortFullName, runtimeData)
	if err != nil {
		return fmt.Errorf("cannot get port data: %v", err)
	}

	// Validate the edge using the custom schema
	return ValidateEdgeSchema(edgeSchema, portData, edgeConfiguration)
}

// ValidateEdgeWithPrecomputedMaps validates an edge using pre-computed flow maps.
// Use this when GetFlowMaps has already been called (e.g., in buildGraphEvents) to avoid
// redundant snapshot calls that cause inconsistent validation results.
func ValidateEdgeWithPrecomputedMaps(ctx context.Context, portSchemaMap map[string]*ajson.Node, edgeConfigMap map[string][]Destination, sourcePortFullName string, edgeConfiguration []byte, edgeSchemaBytes []byte, runtimeData map[string][]byte) error {
	if len(edgeSchemaBytes) == 0 {
		return nil
	}
	edgeSchema, err := ajson.Unmarshal(edgeSchemaBytes)
	if err != nil {
		return fmt.Errorf("invalid edge schema: %v", err)
	}

	portData, err := SimulatePortDataFromMaps(ctx, portSchemaMap, edgeConfigMap, sourcePortFullName, runtimeData)
	if err != nil {
		return fmt.Errorf("cannot get port data: %v", err)
	}

	err = ValidateEdgeSchema(edgeSchema, portData, edgeConfiguration)
	if err != nil {
		// Diagnostic: log source port schema and simulated data when validation fails
		if sourceSchema, ok := portSchemaMap[sourcePortFullName]; ok {
			schemaBytes, _ := ajson.Marshal(sourceSchema)
			hasRuntime := runtimeData != nil && len(runtimeData[sourcePortFullName]) > 0
			log.Info().Str("source_port", sourcePortFullName).
				RawJSON("source_schema", schemaBytes).
				Interface("simulated_data", portData).
				Bool("has_runtime_data", hasRuntime).
				Str("error", err.Error()).
				Msg("edge validation failed — diagnostic")
		}
	}
	return err
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

	// Try to extract schema definition name for better context
	if defName := extractDefName(leaf.KeywordLocation); defName != "" {
		return fmt.Errorf("%s: does not match linked %s schema: %s", path, defName, leaf.Message)
	}
	return fmt.Errorf("%s: %s", path, leaf.Message)
}

// extractDefName pulls the definition name from a JSON Schema keyword location
// e.g. "schema.json#/$defs/Context/required" -> "Context"
func extractDefName(keywordLocation string) string {
	const defsKey = "$defs/"
	idx := strings.LastIndex(keywordLocation, defsKey)
	if idx == -1 {
		return ""
	}
	rest := keywordLocation[idx+len(defsKey):]
	if slashIdx := strings.Index(rest, "/"); slashIdx != -1 {
		return rest[:slashIdx]
	}
	return rest
}

// PropertyMismatch describes a property key in an edge schema definition that doesn't match
// the corresponding property key in the target port schema definition.
type PropertyMismatch struct {
	DefName     string // e.g. "Condition"
	EdgeKey     string // e.g. "routeName" (what the edge has)
	ExpectedKey string // e.g. "route" (what the target port expects), empty if no close match
}

// ValidateEdgeSchemaKeys takes raw edge schema and target port schema bytes, cross-validates
// property names, and returns a user-facing error string if mismatches are found.
// Returns empty string if schemas match or can't be parsed.
// Both platform and desktop client should call this on every edge.
func ValidateEdgeSchemaKeys(edgeSchemaBytes, targetPortSchemaBytes []byte) string {
	if len(edgeSchemaBytes) == 0 || len(targetPortSchemaBytes) == 0 {
		return ""
	}
	edgeSchema, err := ajson.Unmarshal(edgeSchemaBytes)
	if err != nil {
		return ""
	}
	targetSchema, err := ajson.Unmarshal(targetPortSchemaBytes)
	if err != nil {
		return ""
	}
	mismatches := CrossValidateEdgeSchemaKeys(edgeSchema, targetSchema)
	if len(mismatches) == 0 {
		return ""
	}
	m := mismatches[0]
	if m.ExpectedKey != "" {
		return fmt.Sprintf("property %q in %s does not match target port (expected %q) — value will be lost at runtime", m.EdgeKey, m.DefName, m.ExpectedKey)
	}
	return fmt.Sprintf("property %q in %s does not exist in target port schema — value will be lost at runtime", m.EdgeKey, m.DefName)
}

// CrossValidateEdgeSchemaKeys compares property names between edge schema $defs and target
// port schema $defs. For each definition that exists in both schemas, it checks whether the
// edge has property keys that don't exist in the target. This catches bugs like routeName vs route
// where the edge config uses Go field names instead of json tags.
//
// Returns nil if no mismatches found.
func CrossValidateEdgeSchemaKeys(edgeSchema, targetPortSchema *ajson.Node) []PropertyMismatch {
	if edgeSchema == nil || targetPortSchema == nil {
		return nil
	}

	edgeDefs, err := edgeSchema.GetKey("$defs")
	if err != nil || edgeDefs == nil || !edgeDefs.IsObject() {
		return nil
	}

	targetDefs, err := targetPortSchema.GetKey("$defs")
	if err != nil || targetDefs == nil || !targetDefs.IsObject() {
		return nil
	}

	var mismatches []PropertyMismatch

	for _, defName := range edgeDefs.Keys() {
		edgeDef, err := edgeDefs.GetKey(defName)
		if err != nil || edgeDef == nil || !edgeDef.IsObject() {
			continue
		}
		targetDef, err := targetDefs.GetKey(defName)
		if err != nil || targetDef == nil || !targetDef.IsObject() {
			continue
		}

		edgeProps, err := edgeDef.GetKey("properties")
		if err != nil || edgeProps == nil || !edgeProps.IsObject() {
			continue
		}
		targetProps, err := targetDef.GetKey("properties")
		if err != nil || targetProps == nil || !targetProps.IsObject() {
			continue
		}

		// Build set of target property keys
		targetKeys := make(map[string]bool, len(targetProps.Keys()))
		for _, k := range targetProps.Keys() {
			targetKeys[k] = true
		}

		// Check each edge property key against target
		for _, edgeKey := range edgeProps.Keys() {
			if targetKeys[edgeKey] {
				continue
			}
			// Edge has a key the target doesn't — possible field name vs json tag mismatch
			mismatch := PropertyMismatch{
				DefName: defName,
				EdgeKey: edgeKey,
			}
			// Try to find a close match (case-insensitive) for better error messages
			edgeKeyLower := strings.ToLower(edgeKey)
			for targetKey := range targetKeys {
				if strings.ToLower(targetKey) == edgeKeyLower {
					mismatch.ExpectedKey = targetKey
					break
				}
			}
			mismatches = append(mismatches, mismatch)
		}
	}

	return mismatches
}
