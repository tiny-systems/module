package tools

import (
	"reflect"
	"sort"
	"testing"
)

func TestExtractPathsFromConfig(t *testing.T) {
	cases := []struct {
		name string
		in   interface{}
		want []string
	}{
		{
			name: "single path",
			in:   map[string]interface{}{"name": "{{$.context.deploymentName}}"},
			want: []string{"context.deploymentName"},
		},
		{
			name: "concat expression",
			in:   map[string]interface{}{"image": "{{$.context.imageRepo + ':' + $.decoded.imageTag}}"},
			want: []string{"context.imageRepo", "decoded.imageTag"},
		},
		{
			name: "nested object + array",
			in: map[string]interface{}{
				"images": []interface{}{
					map[string]interface{}{
						"name":  "{{$.context.containerName}}",
						"image": "{{$.decoded.imageTag}}",
					},
				},
			},
			want: []string{"context.containerName", "decoded.imageTag"},
		},
		{
			name: "no expressions",
			in:   map[string]interface{}{"hardcoded": "value"},
			want: []string{},
		},
		{
			name: "literal in expression",
			in:   map[string]interface{}{"v": "{{'just-a-string'}}"},
			want: []string{},
		},
		{
			name: "array access",
			in:   map[string]interface{}{"name": "{{$.decoded.items[0].name}}"},
			want: []string{"decoded.items[0].name"},
		},
		{
			name: "wildcard array access",
			in:   map[string]interface{}{"tags": "{{$.tags[*]}}"},
			want: []string{"tags[*]"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractPathsFromConfig(tc.in)
			sort.Strings(got)
			sort.Strings(tc.want)
			if len(got) != len(tc.want) || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("paths mismatch: got %v want %v", got, tc.want)
			}
		})
	}
}

func TestSetPath(t *testing.T) {
	dst := map[string]interface{}{}
	setPath(dst, "context.deploymentName", "<deploymentName>")
	setPath(dst, "context.imageRepo", "<imageRepo>")
	setPath(dst, "decoded.imageTag", "<imageTag>")
	want := map[string]interface{}{
		"context": map[string]interface{}{
			"deploymentName": "<deploymentName>",
			"imageRepo":      "<imageRepo>",
		},
		"decoded": map[string]interface{}{
			"imageTag": "<imageTag>",
		},
	}
	if !reflect.DeepEqual(dst, want) {
		t.Fatalf("setPath result mismatch:\n got %v\nwant %v", dst, want)
	}
}

func TestSetPathArrayIntermediate(t *testing.T) {
	dst := map[string]interface{}{}
	setPath(dst, "decoded.items[0].name", "<name>")
	setPath(dst, "decoded.items[0].sku", "<sku>")
	setPath(dst, "decoded.status", "<status>")

	decoded, ok := dst["decoded"].(map[string]interface{})
	if !ok {
		t.Fatalf("decoded not an object: %T", dst["decoded"])
	}
	items, ok := decoded["items"].([]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("decoded.items not [single]: %v", decoded["items"])
	}
	elem, ok := items[0].(map[string]interface{})
	if !ok {
		t.Fatalf("decoded.items[0] not an object: %T", items[0])
	}
	if elem["name"] != "<name>" || elem["sku"] != "<sku>" {
		t.Fatalf("array element merge wrong: %v", elem)
	}
	if decoded["status"] != "<status>" {
		t.Fatalf("decoded.status wrong: %v", decoded["status"])
	}
}

func TestPathsUnderFields(t *testing.T) {
	got := pathsUnderFields(
		[]string{"context.who", "logs", "outputData.items[0].name", "context", "podName"},
		[]string{"context", "outputData"},
	)
	sort.Strings(got)
	want := []string{"context", "context.who", "outputData.items[0].name"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pathsUnderFields mismatch: got %v want %v", got, want)
	}
}

// TestTargetSchemaPlaceholdersArray pins the failure that made scaffolded
// edges unfixable: llm_chat's `request` port declares `messages` as an
// array, the target node's settings example never mentions it, and the
// scaffold wrote the string "<messages>" — "/messages: expected array, but
// got string", red forever however many times scaffold re-ran.
func TestTargetSchemaPlaceholdersArray(t *testing.T) {
	config := map[string]interface{}{
		"messages":  "{{$.context.messages}}",
		"requestId": "{{$.context.requestId}}",
	}
	schema := []byte(`{
	 "$ref":"#/$defs/Request",
	 "$defs":{
	  "Message":{"type":"object","properties":{
	    "role":{"type":"string","enum":["user","assistant"]},
	    "content":{"type":"string"}}},
	  "Request":{"type":"object","properties":{
	    "messages":{"type":"array","minItems":1,"items":{"$ref":"#/$defs/Message"}},
	    "requestId":{"type":"string"}}}}}`)

	got := targetSchemaPlaceholders(config, schema)

	arr, ok := got["context.messages"].([]interface{})
	if !ok {
		t.Fatalf("array-typed target must not get a string: got %#v", got["context.messages"])
	}
	if len(arr) != 1 {
		t.Fatalf("array placeholder must hold one element (minItems), got %#v", arr)
	}
	elem, ok := arr[0].(map[string]interface{})
	if !ok {
		t.Fatalf("element must be built from the items schema, got %#v", arr[0])
	}
	if elem["role"] != "user" {
		t.Errorf("enum leaf: got %#v, want user", elem["role"])
	}
	if elem["content"] != "<content>" {
		t.Errorf("string leaf: got %#v, want <content>", elem["content"])
	}
	if got["context.requestId"] != "<requestId>" {
		t.Errorf("string field: got %#v, want <requestId>", got["context.requestId"])
	}
}

func TestTargetSchemaPlaceholdersScalars(t *testing.T) {
	got := targetSchemaPlaceholders(
		map[string]interface{}{
			"lines":  "{{$.context.lines}}",
			"follow": "{{$.context.follow}}",
			"name":   "{{$.context.name}}",
			"extra":  "{{$.context.extra}}",
			"joined": "p-{{$.context.joined}}",
		},
		[]byte(`{"properties":{
		  "lines":{"type":"integer"},
		  "follow":{"type":"boolean"},
		  "name":{"type":"string"}}}`))

	if got["context.lines"] != 0 {
		t.Errorf("integer field: got %#v, want 0", got["context.lines"])
	}
	if got["context.follow"] != false {
		t.Errorf("boolean field: got %#v, want false", got["context.follow"])
	}
	if got["context.name"] != "<name>" {
		t.Errorf("string field: got %#v, want <name>", got["context.name"])
	}
	if _, ok := got["context.extra"]; ok {
		t.Error("undeclared field must fall back, not be pinned")
	}
	if _, ok := got["context.joined"]; ok {
		t.Error("interpolated (non-whole-string) expression must not be pinned")
	}
}

func TestTargetSchemaPlaceholdersObject(t *testing.T) {
	got := targetSchemaPlaceholders(
		map[string]interface{}{"headers": "{{$.context.headers}}"},
		[]byte(`{"properties":{"headers":{"type":"object","properties":{
		  "traceId":{"type":"string"},
		  "retries":{"type":"integer"},
		  "nested":{"type":"object","properties":{"deep":{"type":"string"}}}}}}}`))

	want := map[string]interface{}{"traceId": "<traceId>", "retries": 0}
	if !reflect.DeepEqual(got["context.headers"], want) {
		t.Fatalf("object placeholder mismatch:\n got %#v\nwant %#v", got["context.headers"], want)
	}
}

// A shapeless `items` still owes the target one element — an empty array
// fails minItems just as loudly as a string fails `expected array`.
func TestTargetSchemaPlaceholdersShapelessItems(t *testing.T) {
	got := targetSchemaPlaceholders(
		map[string]interface{}{"items": "{{$.outputData.items}}"},
		[]byte(`{"properties":{"items":{"type":"array"}}}`))

	want := []interface{}{map[string]interface{}{}}
	if !reflect.DeepEqual(got["outputData.items"], want) {
		t.Fatalf("shapeless items mismatch:\n got %#v\nwant %#v", got["outputData.items"], want)
	}
}

// TestShapelessPlaceholderFallbackUnchanged: with nothing declared on the
// target port, the settings-example fallback behaves exactly as before.
func TestShapelessPlaceholderFallbackUnchanged(t *testing.T) {
	if got := shapelessPlaceholder("context.who", nil); got != "<who>" {
		t.Errorf("unknown target: got %#v, want <who>", got)
	}
	if got := shapelessPlaceholder("context.n", float64(3)); got != 0 {
		t.Errorf("number example: got %#v, want 0", got)
	}
	if got := shapelessPlaceholder("context.ok", true); got != false {
		t.Errorf("bool example: got %#v, want false", got)
	}
	if got, ok := shapelessPlaceholder("context.list", []interface{}{1}).([]interface{}); !ok || len(got) != 0 {
		t.Errorf("array example: got %#v, want empty array", got)
	}
}

func TestSetPathFirstWriterWins(t *testing.T) {
	dst := map[string]interface{}{}
	setPath(dst, "a.b", "first")
	setPath(dst, "a.b", "second")
	if got := dst["a"].(map[string]interface{})["b"]; got != "first" {
		t.Fatalf("first-writer-wins broken: got %v", got)
	}
}
