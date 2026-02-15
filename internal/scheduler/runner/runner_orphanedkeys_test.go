package runner

import (
	stdjson "encoding/json"
	"reflect"
	"testing"
)

// Test structs mirroring the router's types

type testRouteName struct {
	Value   string
	Options []string
}

func (r *testRouteName) UnmarshalJSON(data []byte) error {
	return stdjson.Unmarshal(data, &r.Value)
}

type testCondition struct {
	RouteName testRouteName `json:"route" title:"Route"`
	Condition bool          `json:"condition" title:"Condition"`
}

type testInMessage struct {
	Context    any             `json:"context" configurable:"true"`
	Conditions []testCondition `json:"conditions"`
}

type testSimple struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

type testNested struct {
	Inner testSimple `json:"inner"`
	Label string     `json:"label"`
}

func TestGetJSONTagNames(t *testing.T) {
	tags := getJSONTagNames(reflect.TypeOf(testSimple{}))
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}
	if _, ok := tags["name"]; !ok {
		t.Error("expected 'name' tag")
	}
	if _, ok := tags["value"]; !ok {
		t.Error("expected 'value' tag")
	}
}

func TestGetJSONTagNames_Pointer(t *testing.T) {
	tags := getJSONTagNames(reflect.TypeOf(&testSimple{}))
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}
}

func TestFindOrphanedKeys_ExactMatch(t *testing.T) {
	data := map[string]interface{}{
		"name":  "hello",
		"value": 42,
	}
	orphaned := findOrphanedKeys(data, reflect.TypeOf(testSimple{}))
	if len(orphaned) != 0 {
		t.Errorf("expected no orphaned keys, got %v", orphaned)
	}
}

func TestFindOrphanedKeys_TopLevelOrphan(t *testing.T) {
	data := map[string]interface{}{
		"Name":  "hello", // Go field name instead of json tag
		"value": 42,
	}
	orphaned := findOrphanedKeys(data, reflect.TypeOf(testSimple{}))
	if len(orphaned) != 1 || orphaned[0] != "Name" {
		t.Errorf("expected [Name], got %v", orphaned)
	}
}

func TestFindOrphanedKeys_NestedOrphanInStruct(t *testing.T) {
	data := map[string]interface{}{
		"inner": map[string]interface{}{
			"Name":  "wrong", // should be "name"
			"value": 1,
		},
		"label": "ok",
	}
	orphaned := findOrphanedKeys(data, reflect.TypeOf(testNested{}))
	if len(orphaned) != 1 || orphaned[0] != "inner.Name" {
		t.Errorf("expected [inner.Name], got %v", orphaned)
	}
}

func TestFindOrphanedKeys_NestedOrphanInArrayItems(t *testing.T) {
	// This is the exact routeName vs route scenario
	data := map[string]interface{}{
		"context": map[string]interface{}{"foo": "bar"},
		"conditions": []interface{}{
			map[string]interface{}{
				"routeName": "NS",  // WRONG — should be "route"
				"condition": true,
			},
			map[string]interface{}{
				"routeName": "PODS", // WRONG
				"condition": false,
			},
		},
	}
	orphaned := findOrphanedKeys(data, reflect.TypeOf(testInMessage{}))
	if len(orphaned) != 2 {
		t.Fatalf("expected 2 orphaned keys, got %v", orphaned)
	}
	expected := []string{"conditions[0].routeName", "conditions[1].routeName"}
	for i, exp := range expected {
		if orphaned[i] != exp {
			t.Errorf("orphaned[%d] = %q, want %q", i, orphaned[i], exp)
		}
	}
}

func TestFindOrphanedKeys_CustomUnmarshalerSkipped(t *testing.T) {
	// testRouteName implements json.Unmarshaler, so its internal keys should NOT be checked
	// Even though the map has "Value" and "Options" which aren't json tags,
	// the type handles its own deserialization
	data := map[string]interface{}{
		"route":     "NS",
		"condition": true,
	}
	orphaned := findOrphanedKeys(data, reflect.TypeOf(testCondition{}))
	if len(orphaned) != 0 {
		t.Errorf("expected no orphaned keys, got %v", orphaned)
	}
}

func TestFindOrphanedKeys_NilType(t *testing.T) {
	data := map[string]interface{}{"key": "val"}
	orphaned := findOrphanedKeys(data, nil)
	if orphaned != nil {
		t.Errorf("expected nil, got %v", orphaned)
	}
}

func TestFindOrphanedKeys_NonStructType(t *testing.T) {
	data := map[string]interface{}{"key": "val"}
	orphaned := findOrphanedKeys(data, reflect.TypeOf("string"))
	if orphaned != nil {
		t.Errorf("expected nil, got %v", orphaned)
	}
}

func TestFindOrphanedKeys_InterfaceField(t *testing.T) {
	// context is `any` (interface) — map value should not be recursed
	data := map[string]interface{}{
		"context":    map[string]interface{}{"anything": "goes"},
		"conditions": []interface{}{},
	}
	orphaned := findOrphanedKeys(data, reflect.TypeOf(testInMessage{}))
	if len(orphaned) != 0 {
		t.Errorf("expected no orphaned keys for interface field, got %v", orphaned)
	}
}
