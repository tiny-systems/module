package tools

import (
	"encoding/json"
	"fmt"
	"sort"
)

// portSchemaBytes returns the raw JSON schema bytes for the named port
// on the given component, or nil if the catalog didn't carry it. Input
// ports and output ports live in different slices on ComponentInfo;
// pass isInput=true to look in InputPortDetails (which includes the
// `_settings` port), false for OutputPortDetails.
func portSchemaBytes(comp *ComponentInfo, portName string, isInput bool) []byte {
	if comp == nil {
		return nil
	}
	ports := comp.OutputPortDetails
	if isInput {
		ports = comp.InputPortDetails
	}
	for _, p := range ports {
		if p.Name == portName {
			return p.Schema
		}
	}
	return nil
}

// configurableFieldsIn scans a JSON Schema (as raw bytes) and returns the
// list of field names declared with `configurable: true`. Configurable
// fields are the variadic ones — their shape is supplied by the author
// alongside the data, not declared on the component's Go struct. We use
// this list to enforce that when an author supplies *data* for a
// configurable field, they MUST also supply a *schema* for it.
//
// Returns an empty slice (not nil) when no configurable fields are
// present, so callers can range over the result without nil checks.
func configurableFieldsIn(schemaBytes []byte) []string {
	if len(schemaBytes) == 0 {
		return []string{}
	}
	var root map[string]interface{}
	if err := json.Unmarshal(schemaBytes, &root); err != nil {
		return []string{}
	}
	defs, ok := root["$defs"].(map[string]interface{})
	if !ok {
		return []string{}
	}
	seen := map[string]struct{}{}
	for defName, raw := range defs {
		def, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if configurable, _ := def["configurable"].(bool); !configurable {
			continue
		}
		// Translate the $defs name back to a field name. The platform
		// reflector capitalises just the first character ("context" →
		// "Context", "outputdata" → "Outputdata"). Lowercase the first
		// letter to recover the JSON field name.
		name := defName
		if name == "" {
			continue
		}
		seen[lowerFirstByte(name)] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func lowerFirstByte(s string) string {
	if s == "" {
		return s
	}
	b := []byte(s)
	if b[0] >= 'A' && b[0] <= 'Z' {
		b[0] += 'a' - 'A'
	}
	return string(b)
}

// requireSchemaForData reports which configurable fields are filled in
// `data` without a matching entry in `userSchema`. The author must
// either provide a schema for those fields or omit the data — the
// platform no longer infers a schema from the value shape.
func requireSchemaForData(data, userSchema map[string]interface{}, configurable []string) []string {
	var missing []string
	for _, field := range configurable {
		if _, hasData := data[field]; !hasData {
			continue
		}
		if userSchema != nil {
			if _, hasSchema := userSchema[field]; hasSchema {
				continue
			}
		}
		missing = append(missing, field)
	}
	return missing
}

// schemaRequiredError builds a uniform error message listing the fields
// missing a schema declaration, with the configurable-field convention
// spelled out so the model can self-correct.
func schemaRequiredError(kind, owner string, fields []string) error {
	if len(fields) == 0 {
		return nil
	}
	return fmt.Errorf(
		"%s '%s' fills configurable field(s) %v without a schema. "+
			"Declare each field's shape in the schema parameter — the platform "+
			"no longer infers schemas from value shape.",
		kind, owner, fields,
	)
}
