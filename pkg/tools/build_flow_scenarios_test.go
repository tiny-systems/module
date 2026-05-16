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

func TestSetPathFirstWriterWins(t *testing.T) {
	dst := map[string]interface{}{}
	setPath(dst, "a.b", "first")
	setPath(dst, "a.b", "second")
	if got := dst["a"].(map[string]interface{})["b"]; got != "first" {
		t.Fatalf("first-writer-wins broken: got %v", got)
	}
}
