package tools

import (
	"strings"
	"testing"
)

func nodeWithUnknownSettings(id, component string, unknown []interface{}) map[string]interface{} {
	return map[string]interface{}{
		"id":   id,
		"type": "tinyNode",
		"data": map[string]interface{}{
			"component":             component,
			"settings_unknown_keys": unknown,
		},
	}
}

func edgeBetween(src, tgt string) map[string]interface{} {
	return map[string]interface{}{
		"id":     src + "-" + tgt,
		"source": src,
		"target": tgt,
	}
}

// The failure this exists for: a credential typed into settings that the
// component never declared. json.Unmarshal drops it silently, the runtime logs
// the complaint into a pod nobody reads, and the flow fails with "api key
// missing" while the key is visibly right there in the node.
func TestIgnoredSettingsAreReported(t *testing.T) {
	issues := FlowIssues([]map[string]interface{}{
		nodeWithUnknownSettings("n1", "llm_complete", []interface{}{"apiKey"}),
		nodeWithUnknownSettings("n2", "debug", nil),
		edgeBetween("n1", "n2"),
	})

	var found string
	for _, i := range issues {
		if strings.Contains(i, "IGNORED SETTINGS") {
			found = i
		}
	}
	if found == "" {
		t.Fatalf("no issue raised for a setting the component does not declare: %v", issues)
	}
	if !strings.Contains(found, "apiKey") || !strings.Contains(found, "llm_complete") {
		t.Errorf("the issue does not say which key on which component: %s", found)
	}
	if !strings.Contains(found, "input port") {
		t.Errorf("a credential in settings should be pointed at the port that consumes it: %s", found)
	}
}

func TestSeveralIgnoredKeysReadAsPlural(t *testing.T) {
	issues := FlowIssues([]map[string]interface{}{
		nodeWithUnknownSettings("n1", "llm_complete", []interface{}{"apiKey", "modelName"}),
		nodeWithUnknownSettings("n2", "debug", nil),
		edgeBetween("n1", "n2"),
	})
	for _, i := range issues {
		if strings.Contains(i, "IGNORED SETTINGS") {
			if !strings.Contains(i, "apiKey, modelName") || !strings.Contains(i, "they are") {
				t.Fatalf("issue reads badly for several keys: %s", i)
			}
			return
		}
	}
	t.Fatal("no issue raised")
}

// A node whose settings all match must not be nagged — an advisory that fires
// on healthy flows is one nobody reads.
func TestDeclaredSettingsRaiseNothing(t *testing.T) {
	issues := FlowIssues([]map[string]interface{}{
		nodeWithUnknownSettings("n1", "llm_complete", nil),
		nodeWithUnknownSettings("n2", "debug", nil),
		edgeBetween("n1", "n2"),
	})
	for _, i := range issues {
		if strings.Contains(i, "IGNORED SETTINGS") {
			t.Fatalf("healthy node reported: %s", i)
		}
	}
}
