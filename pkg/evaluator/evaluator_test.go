package evaluator

import (
	"fmt"
	"github.com/spyzhov/ajson"
	"strings"
	"testing"
	"time"
)

func TestNewEvaluator(t *testing.T) {
	tests := []struct {
		name     string
		callback Callback
		wantNil  bool
	}{
		{
			name: "with callback",
			callback: func(expression string) (interface{}, error) {
				return "result", nil
			},
			wantNil: false,
		},
		{
			name:     "nil callback uses default",
			callback: nil,
			wantNil:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eval := NewEvaluator(tt.callback)
			if eval == nil {
				t.Error("NewEvaluator() returned nil")
			}
			if eval.callback == nil {
				t.Error("NewEvaluator() callback is nil")
			}
		})
	}
}

func TestDefaultCallback(t *testing.T) {
	result, err := DefaultCallback("$.test")
	if err == nil {
		t.Error("DefaultCallback() expected error, got nil")
	}
	if result != nil {
		t.Errorf("DefaultCallback() result = %v, want nil", result)
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Errorf("DefaultCallback() error = %v, want 'not implemented'", err)
	}
}

func TestEvaluator_Eval_SimpleObjects(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		callback Callback
		want     interface{}
		wantErr  bool
	}{
		{
			name: "simple object without expressions",
			data: `{"name": "test", "value": 123}`,
			callback: func(expression string) (interface{}, error) {
				return nil, fmt.Errorf("should not be called")
			},
			want: map[string]interface{}{
				"name":  "test",
				"value": 123.0,
			},
			wantErr: false,
		},
		{
			name: "object with expression (requires value key too)",
			data: `{"field": {"expression": "$.data.value", "value": null}}`,
			callback: func(expression string) (interface{}, error) {
				if expression == "$.data.value" {
					return "evaluated result", nil
				}
				return nil, fmt.Errorf("unexpected expression: %s", expression)
			},
			want: map[string]interface{}{
				"field": "evaluated result",
			},
			wantErr: false,
		},
		{
			name: "object with expression and fallback value",
			data: `{"field": {"expression": "$.data", "value": "fallback"}}`,
			callback: func(expression string) (interface{}, error) {
				return "from expression", nil
			},
			want: map[string]interface{}{
				"field": "from expression",
			},
			wantErr: false,
		},
		{
			name: "multiple fields with expressions",
			data: `{
				"name": {"expression": "$.user.name", "value": null},
				"age": {"expression": "$.user.age", "value": null},
				"static": "value"
			}`,
			callback: func(expression string) (interface{}, error) {
				switch expression {
				case "$.user.name":
					return "John", nil
				case "$.user.age":
					return 30.0, nil
				default:
					return nil, fmt.Errorf("unknown expression")
				}
			},
			want: map[string]interface{}{
				"name":   "John",
				"age":    30.0,
				"static": "value",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eval := NewEvaluator(tt.callback)
			got, err := eval.Eval([]byte(tt.data))

			if (err != nil) != tt.wantErr {
				t.Errorf("Eval() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if !compareInterface(got, tt.want) {
					t.Errorf("Eval() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestEvaluator_Eval_NestedObjects(t *testing.T) {
	data := `{
		"user": {
			"name": {"expression": "$.data.name", "value": null},
			"profile": {
				"age": 25,
				"city": {"expression": "$.data.city", "value": null}
			}
		},
		"timestamp": 1234567890
	}`

	callback := func(expression string) (interface{}, error) {
		switch expression {
		case "$.data.name":
			return "Alice", nil
		case "$.data.city":
			return "New York", nil
		default:
			return nil, fmt.Errorf("unknown expression: %s", expression)
		}
	}

	eval := NewEvaluator(callback)
	got, err := eval.Eval([]byte(data))

	if err != nil {
		t.Fatalf("Eval() unexpected error: %v", err)
	}

	result, ok := got.(map[string]interface{})
	if !ok {
		t.Fatal("Eval() result is not a map")
	}

	user, ok := result["user"].(map[string]interface{})
	if !ok {
		t.Fatal("user is not a map")
	}

	if user["name"] != "Alice" {
		t.Errorf("user.name = %v, want Alice", user["name"])
	}

	profile, ok := user["profile"].(map[string]interface{})
	if !ok {
		t.Fatal("profile is not a map")
	}

	if profile["age"] != 25.0 {
		t.Errorf("profile.age = %v, want 25", profile["age"])
	}

	if profile["city"] != "New York" {
		t.Errorf("profile.city = %v, want New York", profile["city"])
	}

	if result["timestamp"] != 1234567890.0 {
		t.Errorf("timestamp = %v, want 1234567890", result["timestamp"])
	}
}

func TestEvaluator_Eval_Arrays(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		callback Callback
		want     interface{}
		wantErr  bool
	}{
		{
			name: "array of primitives",
			data: `{"items": [1, 2, 3]}`,
			callback: func(expression string) (interface{}, error) {
				return nil, fmt.Errorf("should not be called")
			},
			want: map[string]interface{}{
				"items": []interface{}{1.0, 2.0, 3.0},
			},
			wantErr: false,
		},
		{
			name: "array with expressions",
			data: `{"values": [{"expression": "$.a", "value": null}, {"expression": "$.b", "value": null}, 3]}`,
			callback: func(expression string) (interface{}, error) {
				switch expression {
				case "$.a":
					return 10.0, nil
				case "$.b":
					return 20.0, nil
				default:
					return nil, fmt.Errorf("unknown expression")
				}
			},
			want: map[string]interface{}{
				"values": []interface{}{10.0, 20.0, 3.0},
			},
			wantErr: false,
		},
		{
			name: "array of objects with expressions",
			data: `{
				"users": [
					{"name": {"expression": "$.user1", "value": null}},
					{"name": {"expression": "$.user2", "value": null}},
					{"name": "static"}
				]
			}`,
			callback: func(expression string) (interface{}, error) {
				switch expression {
				case "$.user1":
					return "Alice", nil
				case "$.user2":
					return "Bob", nil
				default:
					return nil, fmt.Errorf("unknown")
				}
			},
			want: map[string]interface{}{
				"users": []interface{}{
					map[string]interface{}{"name": "Alice"},
					map[string]interface{}{"name": "Bob"},
					map[string]interface{}{"name": "static"},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eval := NewEvaluator(tt.callback)
			got, err := eval.Eval([]byte(tt.data))

			if (err != nil) != tt.wantErr {
				t.Errorf("Eval() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if !compareInterface(got, tt.want) {
					t.Errorf("Eval() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestEvaluator_Eval_ErrorCases(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		callback Callback
		wantErr  bool
		errMsg   string
	}{
		{
			name:     "invalid JSON",
			data:     `{invalid json}`,
			callback: DefaultCallback,
			wantErr:  true,
			errMsg:   "",
		},
		{
			name:     "non-object root (array)",
			data:     `[1, 2, 3]`,
			callback: DefaultCallback,
			wantErr:  true,
			errMsg:   "node is not an object",
		},
		{
			name:     "non-object root (string)",
			data:     `"just a string"`,
			callback: DefaultCallback,
			wantErr:  true,
			errMsg:   "node is not an object",
		},
		{
			name:     "non-object root (number)",
			data:     `123`,
			callback: DefaultCallback,
			wantErr:  true,
			errMsg:   "node is not an object",
		},
		{
			name: "callback error",
			data: `{"field": {"expression": "$.bad", "value": null}}`,
			callback: func(expression string) (interface{}, error) {
				return nil, fmt.Errorf("callback error: %s", expression)
			},
			wantErr: true,
			errMsg:  "callback error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eval := NewEvaluator(tt.callback)
			_, err := eval.Eval([]byte(tt.data))

			if (err != nil) != tt.wantErr {
				t.Errorf("Eval() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Eval() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}

func TestEvaluator_Eval_EmptyExpression(t *testing.T) {
	data := `{"field": {"expression": "", "value": "fallback"}}`

	callback := func(expression string) (interface{}, error) {
		t.Error("Callback should not be called for empty expression")
		return nil, fmt.Errorf("should not be called")
	}

	eval := NewEvaluator(callback)
	got, err := eval.Eval([]byte(data))

	if err != nil {
		t.Fatalf("Eval() unexpected error: %v", err)
	}

	result, ok := got.(map[string]interface{})
	if !ok {
		t.Fatal("result is not a map")
	}

	if result["field"] != "fallback" {
		t.Errorf("field = %v, want 'fallback'", result["field"])
	}
}

func TestEvaluator_Eval_RealWorldEdgeConfig(t *testing.T) {
	// Simulates real edge configuration from TinySystems
	data := `{
		"targetField": {"expression": "$.sourceData.items[0].value", "value": null},
		"userId": {"expression": "$.user.id", "value": null},
		"timestamp": {"expression": "now()", "value": null},
		"staticValue": "constant",
		"nested": {
			"computed": {"expression": "$.data.computed", "value": null},
			"literal": 42
		}
	}`

	// Simulate JSONPath evaluation
	callback := func(expression string) (interface{}, error) {
		// Parse the source data
		sourceJSON := `{
			"sourceData": {"items": [{"value": "first-item"}]},
			"user": {"id": "user-123"},
			"data": {"computed": "computed-value"}
		}`
		root, _ := ajson.Unmarshal([]byte(sourceJSON))

		switch expression {
		case "$.sourceData.items[0].value":
			nodes, _ := ajson.Eval(root, expression)
			return nodes.Unpack()
		case "$.user.id":
			nodes, _ := ajson.Eval(root, expression)
			return nodes.Unpack()
		case "$.data.computed":
			nodes, _ := ajson.Eval(root, expression)
			return nodes.Unpack()
		case "now()":
			return float64(time.Now().Unix()), nil
		default:
			return nil, fmt.Errorf("unknown expression: %s", expression)
		}
	}

	eval := NewEvaluator(callback)
	got, err := eval.Eval([]byte(data))

	if err != nil {
		t.Fatalf("Eval() unexpected error: %v", err)
	}

	result, ok := got.(map[string]interface{})
	if !ok {
		t.Fatal("result is not a map")
	}

	if result["targetField"] != "first-item" {
		t.Errorf("targetField = %v, want 'first-item'", result["targetField"])
	}

	if result["userId"] != "user-123" {
		t.Errorf("userId = %v, want 'user-123'", result["userId"])
	}

	if result["staticValue"] != "constant" {
		t.Errorf("staticValue = %v, want 'constant'", result["staticValue"])
	}

	nested, ok := result["nested"].(map[string]interface{})
	if !ok {
		t.Fatal("nested is not a map")
	}

	if nested["computed"] != "computed-value" {
		t.Errorf("nested.computed = %v, want 'computed-value'", nested["computed"])
	}

	if nested["literal"] != 42.0 {
		t.Errorf("nested.literal = %v, want 42", nested["literal"])
	}
}

func TestEvaluator_Eval_ComplexNesting(t *testing.T) {
	data := `{
		"level1": {
			"level2": {
				"level3": {
					"value": {"expression": "$.deep.value", "value": null}
				}
			},
			"array": [
				{
					"item": {"expression": "$.item1", "value": null}
				},
				{
					"item": {"expression": "$.item2", "value": null}
				}
			]
		}
	}`

	callback := func(expression string) (interface{}, error) {
		switch expression {
		case "$.deep.value":
			return "deeply nested", nil
		case "$.item1":
			return "first", nil
		case "$.item2":
			return "second", nil
		default:
			return nil, fmt.Errorf("unknown: %s", expression)
		}
	}

	eval := NewEvaluator(callback)
	got, err := eval.Eval([]byte(data))

	if err != nil {
		t.Fatalf("Eval() unexpected error: %v", err)
	}

	result := got.(map[string]interface{})
	level1 := result["level1"].(map[string]interface{})
	level2 := level1["level2"].(map[string]interface{})
	level3 := level2["level3"].(map[string]interface{})

	if level3["value"] != "deeply nested" {
		t.Errorf("deep value = %v, want 'deeply nested'", level3["value"])
	}

	array := level1["array"].([]interface{})
	item0 := array[0].(map[string]interface{})
	item1 := array[1].(map[string]interface{})

	if item0["item"] != "first" {
		t.Errorf("array[0].item = %v, want 'first'", item0["item"])
	}

	if item1["item"] != "second" {
		t.Errorf("array[1].item = %v, want 'second'", item1["item"])
	}
}

// Helper function to compare interfaces (handles nested maps and slices)
func compareInterface(got, want interface{}) bool {
	switch wantVal := want.(type) {
	case map[string]interface{}:
		gotMap, ok := got.(map[string]interface{})
		if !ok {
			return false
		}
		if len(gotMap) != len(wantVal) {
			return false
		}
		for k, v := range wantVal {
			if !compareInterface(gotMap[k], v) {
				return false
			}
		}
		return true
	case []interface{}:
		gotSlice, ok := got.([]interface{})
		if !ok {
			return false
		}
		if len(gotSlice) != len(wantVal) {
			return false
		}
		for i, v := range wantVal {
			if !compareInterface(gotSlice[i], v) {
				return false
			}
		}
		return true
	default:
		return got == want
	}
}
