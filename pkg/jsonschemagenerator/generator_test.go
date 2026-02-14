package jsonschemagenerator

import (
	"testing"

	"github.com/tiny-systems/ajson"
)

// TestGenerate_CallbackMergesMissingProperties verifies that when a callback
// replaces an object, properties missing from the callback result are still
// filled in with schema-generated mock data.
//
// This simulates an edge evaluation callback that returns partial data
// (e.g. edge config maps some fields but not all). The generator should
// produce the callback result AND fill in unmapped fields from the schema.
func TestGenerate_CallbackMergesMissingProperties(t *testing.T) {
	// Schema: an object with 4 properties, one of which is an array of strings.
	// This mirrors the HTTP server Start port schema where "hostnames" is []string.
	schemaJSON := `{
		"$ref": "#/$defs/Start",
		"$defs": {
			"Start": {
				"type": "object",
				"port": "flow.node.start",
				"path": "$",
				"properties": {
					"autoHostName": {
						"type": "boolean",
						"title": "Auto Host Name",
						"propertyOrder": 1
					},
					"hostnames": {
						"type": "array",
						"items": {"$ref": "#/$defs/String"},
						"title": "Hostnames",
						"propertyOrder": 2
					},
					"readTimeout": {
						"type": "integer",
						"title": "Read Timeout",
						"propertyOrder": 3
					},
					"writeTimeout": {
						"type": "integer",
						"title": "Write Timeout",
						"propertyOrder": 4
					}
				}
			},
			"String": {
				"type": "string"
			}
		}
	}`

	node, err := ajson.Unmarshal([]byte(schemaJSON))
	if err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}

	// Callback simulates edge evaluation: returns partial data missing "hostnames"
	callback := func(n *ajson.Node) (interface{}, bool, error) {
		portStr, _ := GetStrKey("port", n)
		if portStr == "" {
			return nil, false, nil
		}
		// Simulate edge evaluation returning only some fields
		return map[string]interface{}{
			"autoHostName": false,
			"readTimeout":  60,
			"writeTimeout": 10,
		}, true, nil
	}

	gen := NewSchemaBasedDataGenerator()
	result, err := gen.Generate(node, callback)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}

	// Verify callback-provided fields are present
	if _, ok := m["autoHostName"]; !ok {
		t.Error("autoHostName missing from result")
	}
	if _, ok := m["readTimeout"]; !ok {
		t.Error("readTimeout missing from result")
	}
	if _, ok := m["writeTimeout"]; !ok {
		t.Error("writeTimeout missing from result")
	}

	// Verify the array field is filled in with mock data
	hostnames, ok := m["hostnames"]
	if !ok {
		t.Fatal("hostnames missing from result — generator should fill in unmapped array properties with mock data")
	}

	hostnamesArr, ok := hostnames.([]interface{})
	if !ok {
		t.Fatalf("hostnames should be an array, got %T", hostnames)
	}
	if len(hostnamesArr) == 0 {
		t.Fatal("hostnames should have at least one mock element")
	}

	// The mock element should be a string
	if _, ok := hostnamesArr[0].(string); !ok {
		t.Errorf("hostnames[0] should be a string, got %T", hostnamesArr[0])
	}
}

// TestGenerate_ArrayAlwaysHasElement verifies that arrays always get at least
// one mock element, even without a callback.
func TestGenerate_ArrayAlwaysHasElement(t *testing.T) {
	schemaJSON := `{
		"type": "object",
		"properties": {
			"items": {
				"type": "array",
				"items": {"type": "string"},
				"title": "Items"
			}
		}
	}`

	node, err := ajson.Unmarshal([]byte(schemaJSON))
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	gen := NewSchemaBasedDataGenerator()
	result, err := gen.Generate(node, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}

	items, ok := m["items"]
	if !ok {
		t.Fatal("items missing from result")
	}

	arr, ok := items.([]interface{})
	if !ok {
		t.Fatalf("items should be array, got %T", items)
	}
	if len(arr) == 0 {
		t.Fatal("array should have at least one mock element")
	}
	if _, ok := arr[0].(string); !ok {
		t.Errorf("items[0] should be string, got %T", arr[0])
	}
}
