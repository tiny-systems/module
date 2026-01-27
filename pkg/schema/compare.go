package schema

import (
	"reflect"

	"github.com/goccy/go-json"
	"github.com/tiny-systems/ajson"
)

// SchemasEqual compares two JSON schemas for semantic equality.
// It parses both into Go maps and compares them recursively to ignore
// whitespace and key ordering differences.
// Returns true if both schemas are nil/empty or if they are semantically equal.
func SchemasEqual(schema1, schema2 []byte) bool {
	// Both empty/nil
	if len(schema1) == 0 && len(schema2) == 0 {
		return true
	}

	// One is empty, other is not
	if len(schema1) == 0 || len(schema2) == 0 {
		return false
	}

	// Parse both schemas into generic interface{}
	var val1, val2 interface{}

	if err := json.Unmarshal(schema1, &val1); err != nil {
		return false
	}

	if err := json.Unmarshal(schema2, &val2); err != nil {
		return false
	}

	// Deep compare
	return reflect.DeepEqual(val1, val2)
}

// HasCustomDefinitions checks if a schema has any definitions marked with configurable:true.
// This indicates user-customized schema that must be preserved in exports.
func HasCustomDefinitions(schemaBytes []byte) bool {
	if len(schemaBytes) == 0 {
		return false
	}

	node, err := ajson.Unmarshal(schemaBytes)
	if err != nil {
		return false
	}

	defs, err := node.GetKey("$defs")
	if err != nil || !defs.IsObject() {
		return false
	}

	for _, defName := range defs.Keys() {
		def, err := defs.GetKey(defName)
		if err != nil || def == nil {
			continue
		}

		if configurable, _ := GetBool("configurable", def); configurable {
			return true
		}
	}

	return false
}

// SchemasEqualIgnoringDefinitions compares two schemas but ignores the $defs section.
// This is useful for comparing the "shape" of schemas without considering
// definition details that might have configurable variations.
func SchemasEqualIgnoringDefinitions(schema1, schema2 []byte) bool {
	if len(schema1) == 0 && len(schema2) == 0 {
		return true
	}

	if len(schema1) == 0 || len(schema2) == 0 {
		return false
	}

	// Parse both schemas into maps
	var val1, val2 map[string]interface{}

	if err := json.Unmarshal(schema1, &val1); err != nil {
		return false
	}

	if err := json.Unmarshal(schema2, &val2); err != nil {
		return false
	}

	// Remove $defs from both for comparison
	delete(val1, "$defs")
	delete(val2, "$defs")

	return reflect.DeepEqual(val1, val2)
}
