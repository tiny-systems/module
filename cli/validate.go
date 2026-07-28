package cli

import (
	"fmt"

	api "github.com/tiny-systems/platform-api"
)

// errorPortName is the conventional name for a component's error output port.
// A source port with this name is expected to carry the canonical
// module.ErrorMessage shape.
const errorPortName = "error"

// validateErrorPorts checks that every source port named "error" declares the
// canonical module.ErrorMessage fields ({context, error, retryable}). It walks
// the published JSON schema for property names rather than reflecting the Go
// type, so it validates exactly what a consumer (the retry component) sees.
// Returns human-readable warnings; empty when everything conforms.
func validateErrorPorts(components []api.PublishComponent) []string {
	var warnings []string
	for _, c := range components {
		if c.Ports == nil {
			continue
		}
		for _, p := range *c.Ports {
			if p.Name != errorPortName || !p.Source || p.Schema == nil {
				continue
			}
			props := schemaPropertyNames(*p.Schema)
			var missing []string
			for _, req := range []string{"context", "error", "retryable"} {
				if !props[req] {
					missing = append(missing, req)
				}
			}
			if len(missing) > 0 {
				warnings = append(warnings, fmt.Sprintf(
					"component %q error port is missing %v — emit module.NewError(ctx, err) (module.ErrorMessage) so the retry component and the platform can supervise it; wrap transient failures with module.Retryable",
					c.Name, missing,
				))
			}
		}
	}
	return warnings
}

// schemaPropertyNames collects every key that appears under any "properties"
// object anywhere in a JSON schema (walking $defs, $ref targets are already
// inlined as $defs entries by the reflector). Good enough to assert a field
// is declared somewhere in the port's shape.
func schemaPropertyNames(schema map[string]interface{}) map[string]bool {
	out := map[string]bool{}
	var walk func(v interface{})
	walk = func(v interface{}) {
		switch x := v.(type) {
		case map[string]interface{}:
			if props, ok := x["properties"].(map[string]interface{}); ok {
				for name := range props {
					out[name] = true
				}
			}
			for _, val := range x {
				walk(val)
			}
		case []interface{}:
			for _, val := range x {
				walk(val)
			}
		}
	}
	walk(schema)
	return out
}
