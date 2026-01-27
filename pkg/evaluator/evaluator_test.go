package evaluator

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tiny-systems/ajson"
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
			name: "object with expression returns actual type",
			data: `{"field": "{{$.data.value}}"}`,
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
			name: "expression returning number",
			data: `{"count": "{{$.data.count}}"}`,
			callback: func(expression string) (interface{}, error) {
				return 42.0, nil
			},
			want: map[string]interface{}{
				"count": 42.0,
			},
			wantErr: false,
		},
		{
			name: "expression returning boolean",
			data: `{"active": "{{$.data.active}}"}`,
			callback: func(expression string) (interface{}, error) {
				return true, nil
			},
			want: map[string]interface{}{
				"active": true,
			},
			wantErr: false,
		},
		{
			name: "multiple fields with expressions",
			data: `{
				"name": "{{$.user.name}}",
				"age": "{{$.user.age}}",
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
		{
			name: "dollar sign in literal string is preserved",
			data: `{"price": "$18.99", "discount": "Save $5"}`,
			callback: func(expression string) (interface{}, error) {
				return nil, fmt.Errorf("should not be called for literals")
			},
			want: map[string]interface{}{
				"price":    "$18.99",
				"discount": "Save $5",
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

func TestEvaluator_Eval_StringInterpolation(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		callback Callback
		want     interface{}
		wantErr  bool
	}{
		{
			name: "interpolation in middle of string",
			data: `{"greeting": "Hello {{$.name}}!"}`,
			callback: func(expression string) (interface{}, error) {
				if expression == "$.name" {
					return "World", nil
				}
				return nil, fmt.Errorf("unexpected: %s", expression)
			},
			want: map[string]interface{}{
				"greeting": "Hello World!",
			},
			wantErr: false,
		},
		{
			name: "multiple interpolations in one string",
			data: `{"message": "{{$.greeting}}, {{$.name}}! You have {{$.count}} messages."}`,
			callback: func(expression string) (interface{}, error) {
				switch expression {
				case "$.greeting":
					return "Hello", nil
				case "$.name":
					return "Alice", nil
				case "$.count":
					return 5, nil
				default:
					return nil, fmt.Errorf("unexpected: %s", expression)
				}
			},
			want: map[string]interface{}{
				"message": "Hello, Alice! You have 5 messages.",
			},
			wantErr: false,
		},
		{
			name: "pure expression returns actual type not string",
			data: `{"data": "{{$.object}}"}`,
			callback: func(expression string) (interface{}, error) {
				return map[string]interface{}{"nested": "value"}, nil
			},
			want: map[string]interface{}{
				"data": map[string]interface{}{"nested": "value"},
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
			"name": "{{$.data.name}}",
			"profile": {
				"age": 25,
				"city": "{{$.data.city}}"
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
			data: `{"values": ["{{$.a}}", "{{$.b}}", 3]}`,
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
					{"name": "{{$.user1}}"},
					{"name": "{{$.user2}}"},
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
		// Note: callback errors are now handled gracefully (return nil, not error)
		// This allows edge inspection to continue even when source data is unavailable
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

func TestEvaluator_Eval_NoExpression(t *testing.T) {
	data := `{"field": "just a regular string"}`

	callback := func(expression string) (interface{}, error) {
		t.Error("Callback should not be called for plain strings")
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

	if result["field"] != "just a regular string" {
		t.Errorf("field = %v, want 'just a regular string'", result["field"])
	}
}

func TestEvaluator_Eval_CallbackErrorGraceful(t *testing.T) {
	// When callback fails (e.g., source data unavailable), should return nil gracefully
	data := `{"field": "{{$.unavailable}}"}`

	callback := func(expression string) (interface{}, error) {
		return nil, fmt.Errorf("data not available")
	}

	eval := NewEvaluator(callback)
	got, err := eval.Eval([]byte(data))

	if err != nil {
		t.Fatalf("Eval() should not return error, got: %v", err)
	}

	result, ok := got.(map[string]interface{})
	if !ok {
		t.Fatal("result is not a map")
	}

	if result["field"] != nil {
		t.Errorf("field = %v, want nil", result["field"])
	}
}

func TestEvaluator_Eval_InterpolationWithCallbackError(t *testing.T) {
	// When callback fails in interpolation, should leave expression unevaluated
	data := `{"greeting": "Hello {{$.unavailable}}!"}`

	callback := func(expression string) (interface{}, error) {
		return nil, fmt.Errorf("data not available")
	}

	eval := NewEvaluator(callback)
	got, err := eval.Eval([]byte(data))

	if err != nil {
		t.Fatalf("Eval() should not return error, got: %v", err)
	}

	result, ok := got.(map[string]interface{})
	if !ok {
		t.Fatal("result is not a map")
	}

	// When interpolation callback fails, the expression is left unevaluated
	if result["greeting"] != "Hello {{$.unavailable}}!" {
		t.Errorf("greeting = %v, want 'Hello {{$.unavailable}}!'", result["greeting"])
	}
}

func TestEvaluator_Eval_RealWorldEdgeConfig(t *testing.T) {
	// Simulates real edge configuration from TinySystems with new format
	data := `{
		"targetField": "{{$.sourceData.items[0].value}}",
		"userId": "{{$.user.id}}",
		"timestamp": "{{now()}}",
		"staticValue": "constant",
		"nested": {
			"computed": "{{$.data.computed}}",
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
			return 1234567890.0, nil
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
					"value": "{{$.deep.value}}"
				}
			},
			"array": [
				{
					"item": "{{$.item1}}"
				},
				{
					"item": "{{$.item2}}"
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

func TestEvaluator_Eval_FunctionExpressions(t *testing.T) {
	// Test expressions that use functions, not just JSONPath
	tests := []struct {
		name     string
		data     string
		callback Callback
		want     interface{}
	}{
		{
			name: "function call expression",
			data: `{"converted": "{{string($.number)}}"}`,
			callback: func(expression string) (interface{}, error) {
				if expression == "string($.number)" {
					return "42", nil
				}
				return nil, fmt.Errorf("unexpected: %s", expression)
			},
			want: map[string]interface{}{
				"converted": "42",
			},
		},
		{
			name: "concatenation expression",
			data: `{"greeting": "{{\"Hello \" + $.name}}"}`,
			callback: func(expression string) (interface{}, error) {
				if expression == "\"Hello \" + $.name" {
					return "Hello World", nil
				}
				return nil, fmt.Errorf("unexpected: %s", expression)
			},
			want: map[string]interface{}{
				"greeting": "Hello World",
			},
		},
		{
			name: "comparison expression returning boolean",
			data: `{"isAdmin": "{{$.role == 'admin'}}"}`,
			callback: func(expression string) (interface{}, error) {
				if expression == "$.role == 'admin'" {
					return true, nil
				}
				return nil, fmt.Errorf("unexpected: %s", expression)
			},
			want: map[string]interface{}{
				"isAdmin": true,
			},
		},
		{
			name: "arithmetic expression",
			data: `{"total": "{{$.price * $.quantity}}"}`,
			callback: func(expression string) (interface{}, error) {
				if expression == "$.price * $.quantity" {
					return 99.99, nil
				}
				return nil, fmt.Errorf("unexpected: %s", expression)
			},
			want: map[string]interface{}{
				"total": 99.99,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eval := NewEvaluator(tt.callback)
			got, err := eval.Eval([]byte(tt.data))

			if err != nil {
				t.Fatalf("Eval() unexpected error: %v", err)
			}

			if !compareInterface(got, tt.want) {
				t.Errorf("Eval() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvaluator_Eval_PassEntireMessage(t *testing.T) {
	data := `{"context": "{{$}}"}`

	callback := func(expression string) (interface{}, error) {
		if expression == "$" {
			return map[string]interface{}{
				"method":  "GET",
				"path":    "/api/users",
				"headers": map[string]interface{}{"Content-Type": "application/json"},
			}, nil
		}
		return nil, fmt.Errorf("unexpected: %s", expression)
	}

	eval := NewEvaluator(callback)
	got, err := eval.Eval([]byte(data))

	if err != nil {
		t.Fatalf("Eval() unexpected error: %v", err)
	}

	result := got.(map[string]interface{})
	context := result["context"].(map[string]interface{})

	if context["method"] != "GET" {
		t.Errorf("context.method = %v, want 'GET'", context["method"])
	}
	if context["path"] != "/api/users" {
		t.Errorf("context.path = %v, want '/api/users'", context["path"])
	}
}

func TestEvaluator_WithErrorCallback(t *testing.T) {
	var capturedErrors []struct {
		expr string
		err  string
	}

	callback := func(expression string) (interface{}, error) {
		if expression == "$.valid" {
			return "valid value", nil
		}
		return nil, fmt.Errorf("unknown path: %s", expression)
	}

	onError := func(expr string, err error) {
		capturedErrors = append(capturedErrors, struct {
			expr string
			err  string
		}{expr: expr, err: err.Error()})
	}

	eval := NewEvaluator(callback).WithErrorCallback(onError)

	t.Run("pure expression error captured", func(t *testing.T) {
		capturedErrors = nil
		data := `{"field": "{{$.missing}}"}`
		_, err := eval.Eval([]byte(data))
		if err != nil {
			t.Fatalf("Eval() should not return error, got: %v", err)
		}
		if len(capturedErrors) != 1 {
			t.Fatalf("expected 1 error captured, got %d", len(capturedErrors))
		}
		if capturedErrors[0].expr != "$.missing" {
			t.Errorf("captured expr = %q, want %q", capturedErrors[0].expr, "$.missing")
		}
	})

	t.Run("interpolation error captured", func(t *testing.T) {
		capturedErrors = nil
		data := `{"greeting": "Hello {{$.missing}}!"}`
		got, err := eval.Eval([]byte(data))
		if err != nil {
			t.Fatalf("Eval() should not return error, got: %v", err)
		}
		if len(capturedErrors) != 1 {
			t.Fatalf("expected 1 error captured, got %d", len(capturedErrors))
		}
		// Check expression was left unevaluated
		result := got.(map[string]interface{})
		if result["greeting"] != "Hello {{$.missing}}!" {
			t.Errorf("greeting = %v, want 'Hello {{$.missing}}!'", result["greeting"])
		}
	})

	t.Run("multiple errors captured", func(t *testing.T) {
		capturedErrors = nil
		data := `{"a": "{{$.missing1}}", "b": "{{$.missing2}}", "c": "{{$.valid}}"}`
		_, err := eval.Eval([]byte(data))
		if err != nil {
			t.Fatalf("Eval() should not return error, got: %v", err)
		}
		if len(capturedErrors) != 2 {
			t.Fatalf("expected 2 errors captured, got %d", len(capturedErrors))
		}
	})

	t.Run("no callback no panic", func(t *testing.T) {
		evalNoCallback := NewEvaluator(callback)
		data := `{"field": "{{$.missing}}"}`
		_, err := evalNoCallback.Eval([]byte(data))
		if err != nil {
			t.Fatalf("Eval() should not return error, got: %v", err)
		}
		// Should not panic, just silently ignore
	})
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
