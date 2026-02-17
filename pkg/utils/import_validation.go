package utils

import (
	"fmt"
	"sort"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// ValidateProjectImport performs strict validation of import data.
// Returns all errors (blocking) and warnings (informational).
// Import should be blocked if errors is non-empty.
func ValidateProjectImport(data *ProjectExport) (errors, warnings []string) {
	// Build lookup maps
	flowSet := make(map[string]bool)
	for _, flow := range data.TinyFlows {
		if flow.ResourceName == "" {
			errors = append(errors, "flow has empty resourceName")
			continue
		}
		flowSet[flow.ResourceName] = true
	}

	if len(data.TinyFlows) == 0 {
		errors = append(errors, "no flows defined in tinyFlows")
	}

	// First pass: collect node IDs and build node-to-component map
	nodeIDs := make(map[string]bool)
	nodeLabels := make(map[string]string) // nodeID -> label or component for readable messages
	for _, elem := range data.Elements {
		elemType, _ := elem["type"].(string)
		if elemType != TinyNodeType {
			continue
		}
		elemID, _ := elem["id"].(string)
		if elemID != "" {
			nodeIDs[elemID] = true
			if dataMap, ok := elem["data"].(map[string]interface{}); ok {
				label := GetStr(dataMap["label"])
				if label == "" {
					label = GetStr(dataMap["component"])
				}
				nodeLabels[elemID] = label
			}
		}
	}

	// Second pass: validate all elements
	for i, elem := range data.Elements {
		elemType, _ := elem["type"].(string)
		elemID, _ := elem["id"].(string)

		switch elemType {
		case TinyNodeType:
			e, w := validateImportNode(i, elemID, elem, flowSet)
			errors = append(errors, e...)
			warnings = append(warnings, w...)
		case TinyEdgeType, "edge":
			e, w := validateImportEdge(i, elem, flowSet, nodeIDs, nodeLabels)
			errors = append(errors, e...)
			warnings = append(warnings, w...)
		default:
			if elemType != "" {
				errors = append(errors, fmt.Sprintf("element[%d] %q: unknown type %q (expected %q or %q)", i, elemID, elemType, TinyNodeType, TinyEdgeType))
			}
		}
	}

	// Validate pages
	for i, page := range data.Pages {
		e, w := validateImportPage(i, &page, nodeIDs)
		errors = append(errors, e...)
		warnings = append(warnings, w...)
	}

	return errors, warnings
}

// validateImportNode validates a node element for import.
func validateImportNode(index int, id string, elem map[string]interface{}, flowSet map[string]bool) (errors, warnings []string) {
	prefix := fmt.Sprintf("node %q", id)
	if id == "" {
		prefix = fmt.Sprintf("node[%d]", index)
		errors = append(errors, prefix+": missing id")
	}

	flowName, _ := elem["flow"].(string)
	if flowName == "" {
		errors = append(errors, prefix+": missing flow")
	} else if !flowSet[flowName] {
		errors = append(errors, fmt.Sprintf("%s: flow %q not in tinyFlows", prefix, flowName))
	}

	dataMap, ok := elem["data"].(map[string]interface{})
	if !ok {
		errors = append(errors, prefix+": missing data object")
		return
	}

	if GetStr(dataMap["component"]) == "" {
		errors = append(errors, prefix+": missing data.component")
	}
	if GetStr(dataMap["module"]) == "" {
		errors = append(errors, prefix+": missing data.module")
	}

	// Validate handles
	handles, _ := dataMap["handles"].([]interface{})
	for j, h := range handles {
		handleMap, ok := h.(map[string]interface{})
		if !ok {
			continue
		}

		handleID := GetStr(handleMap["id"])
		handleType := GetStr(handleMap["type"])
		hPrefix := fmt.Sprintf("%s handle %q", prefix, handleID)

		// Source port handles are harmless (import code skips them) but unnecessary
		if handleType == "source" {
			warnings = append(warnings, fmt.Sprintf("%s: source port handle included — will be skipped (handle index %d)", hPrefix, j))
			continue
		}

		// Check schema completeness on target handles that have schema
		if schemaRaw, hasSchema := handleMap["schema"]; hasSchema && schemaRaw != nil {
			e, w := validateSchemaCompleteness(hPrefix, schemaRaw)
			errors = append(errors, e...)
			warnings = append(warnings, w...)
		}
	}

	return
}

// validateImportEdge validates an edge element for import.
func validateImportEdge(index int, elem map[string]interface{}, flowSet map[string]bool, nodeIDs map[string]bool, nodeLabels map[string]string) (errors, warnings []string) {
	source := GetStr(elem["source"])
	target := GetStr(elem["target"])
	sourceHandle := GetStr(elem["sourceHandle"])
	targetHandle := GetStr(elem["targetHandle"])

	// Build readable prefix
	srcLabel := nodeLabels[source]
	tgtLabel := nodeLabels[target]
	prefix := fmt.Sprintf("edge %s:%s → %s:%s", shortID(source, srcLabel), sourceHandle, shortID(target, tgtLabel), targetHandle)

	// Required fields
	if source == "" {
		errors = append(errors, fmt.Sprintf("edge[%d]: missing source", index))
	}
	if target == "" {
		errors = append(errors, fmt.Sprintf("edge[%d]: missing target", index))
	}
	if sourceHandle == "" {
		errors = append(errors, prefix+": missing sourceHandle")
	}
	if targetHandle == "" {
		errors = append(errors, prefix+": missing targetHandle")
	}

	// Cross-references
	if source != "" && !nodeIDs[source] {
		errors = append(errors, fmt.Sprintf("%s: source node %q not found in elements", prefix, source))
	}
	if target != "" && !nodeIDs[target] {
		errors = append(errors, fmt.Sprintf("%s: target node %q not found in elements", prefix, target))
	}

	// Flow reference
	flowName := GetStr(elem["flow"])
	if flowName == "" {
		errors = append(errors, prefix+": missing flow")
	} else if !flowSet[flowName] {
		errors = append(errors, fmt.Sprintf("%s: flow %q not in tinyFlows", prefix, flowName))
	}

	// Edge data
	edgeData, ok := elem["data"].(map[string]interface{})
	if !ok {
		errors = append(errors, prefix+": missing data object")
		return
	}

	// Configuration — optional (some edges pass data without transformation)
	config := edgeData["configuration"]
	if config == nil {
		warnings = append(warnings, prefix+": no data.configuration — edge will pass data without transformation")
	} else {
		e, w := validateConfigExpressions(prefix, config)
		errors = append(errors, e...)
		warnings = append(warnings, w...)
	}

	// Schema — optional (derived at runtime from native port schema)
	schemaRaw := edgeData["schema"]
	if schemaRaw == nil {
		warnings = append(warnings, prefix+": no data.schema — will be derived from target port at runtime")
	} else {
		e, w := validateSchemaCompleteness(prefix+" schema", schemaRaw)
		errors = append(errors, e...)
		warnings = append(warnings, w...)

		// Validate config keys match schema root type properties
		if config != nil {
			errors = append(errors, validateConfigKeysMatchSchema(prefix, config, schemaRaw)...)
		}
	}

	return
}

// validateImportPage validates a page and its widgets.
func validateImportPage(index int, page *ExportPage, nodeIDs map[string]bool) (errors, warnings []string) {
	prefix := fmt.Sprintf("page %q", page.Title)
	if page.Title == "" {
		prefix = fmt.Sprintf("page[%d]", index)
	}

	for i, widget := range page.Widgets {
		wPrefix := fmt.Sprintf("%s widget %q", prefix, widget.Name)
		if widget.Name == "" {
			wPrefix = fmt.Sprintf("%s widget[%d]", prefix, i)
		}

		if widget.Port == "" {
			errors = append(errors, wPrefix+": missing port")
			continue
		}

		parts := strings.SplitN(widget.Port, ":", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			errors = append(errors, fmt.Sprintf("%s: port %q invalid format — expected 'nodeId:portName'", wPrefix, widget.Port))
			continue
		}

		nodeID := parts[0]
		if !nodeIDs[nodeID] {
			errors = append(errors, fmt.Sprintf("%s: port references node %q which is not in elements", wPrefix, nodeID))
		}

		// Warn about schemaPatch presence
		if len(widget.SchemaPatch) > 0 {
			patchStr := string(widget.SchemaPatch)
			if patchStr != "[]" && patchStr != "null" && patchStr != "" {
				warnings = append(warnings, fmt.Sprintf("%s: has schemaPatch — only include when default _control rendering is incorrect, remove if unnecessary", wPrefix))
			}
		}
	}

	return
}

// validateSchemaCompleteness checks that a schema has $ref and complete $defs.
func validateSchemaCompleteness(prefix string, schemaRaw interface{}) (errors, warnings []string) {
	schemaMap, ok := schemaRaw.(map[string]interface{})
	if !ok {
		errors = append(errors, prefix+": schema is not an object")
		return
	}

	// Check $ref
	if _, ok := schemaMap["$ref"]; !ok {
		errors = append(errors, prefix+": schema missing $ref")
	}

	// Check $defs
	defs, ok := schemaMap["$defs"]
	if !ok {
		errors = append(errors, prefix+": schema missing $defs")
		return
	}

	defsMap, ok := defs.(map[string]interface{})
	if !ok {
		errors = append(errors, prefix+": schema $defs is not an object")
		return
	}

	if len(defsMap) == 0 {
		errors = append(errors, prefix+": schema $defs is empty")
		return
	}

	// Check $defs key naming follows SDK title-casing
	titleCaser := cases.Title(language.English)
	for key := range defsMap {
		expected := titleCaser.String(key)
		if key != expected {
			errors = append(errors, fmt.Sprintf("%s: $defs key %q should be %q (SDK title-cases type names)", prefix, key, expected))
		}
	}

	return
}

// validateConfigExpressions checks expressions in edge configuration for common mistakes.
func validateConfigExpressions(prefix string, config interface{}) (errors, warnings []string) {
	switch v := config.(type) {
	case map[string]interface{}:
		for key, val := range v {
			// Check for "context": "{{$}}" passthrough pattern
			if key == "context" {
				if str, ok := val.(string); ok && str == "{{$}}" {
					warnings = append(warnings, fmt.Sprintf("%s: config.context is \"{{$}}\" — consider re-assembling context with only needed fields", prefix))
				}
			}
			e, w := validateConfigExpressions(prefix, val)
			errors = append(errors, e...)
			warnings = append(warnings, w...)
		}
	case string:
		e, w := checkExpressionSyntax(prefix, v)
		errors = append(errors, e...)
		warnings = append(warnings, w...)
	case []interface{}:
		for _, item := range v {
			e, w := validateConfigExpressions(prefix, item)
			errors = append(errors, e...)
			warnings = append(warnings, w...)
		}
	}
	return
}

// checkExpressionSyntax checks a string value for expression syntax issues.
// Returns errors (blocking) and warnings (informational).
func checkExpressionSyntax(prefix, value string) (errs, warns []string) {
	start := 0
	for {
		idx := strings.Index(value[start:], "{{")
		if idx < 0 {
			break
		}
		idx += start
		end := strings.Index(value[idx:], "}}")
		if end < 0 {
			errs = append(errs, fmt.Sprintf("%s: unclosed expression starting at position %d", prefix, idx))
			break
		}
		end += idx

		expr := value[idx+2 : end]
		if strings.TrimSpace(expr) == "" {
			errs = append(errs, fmt.Sprintf("%s: empty expression {{}}", prefix))
		}

		// Check for single quotes — ajson requires double quotes
		if strings.Contains(expr, "'") {
			warns = append(warns, fmt.Sprintf("%s: expression {{%s}} uses single quotes — ajson requires double quotes (use \\\" in JSON)", prefix, strings.TrimSpace(expr)))
		}

		start = end + 2
	}
	return errs, warns
}

// validateConfigKeysMatchSchema checks that edge configuration keys match the schema's root type properties.
func validateConfigKeysMatchSchema(prefix string, config, schemaRaw interface{}) []string {
	configMap, ok := config.(map[string]interface{})
	if !ok {
		return nil // Config is not a map (might be a string expression), skip
	}

	schemaMap, ok := schemaRaw.(map[string]interface{})
	if !ok {
		return nil
	}

	// Get root type from $ref
	ref, ok := schemaMap["$ref"].(string)
	if !ok {
		return nil
	}

	// Parse $ref like "#/$defs/Request" → "Request"
	refParts := strings.Split(ref, "/")
	if len(refParts) < 2 {
		return nil
	}
	rootTypeName := refParts[len(refParts)-1]

	// Find root type in $defs
	defs, ok := schemaMap["$defs"].(map[string]interface{})
	if !ok {
		return nil
	}

	rootDef, ok := defs[rootTypeName].(map[string]interface{})
	if !ok {
		return nil
	}

	// Get properties
	props, ok := rootDef["properties"].(map[string]interface{})
	if !ok {
		return nil
	}

	var errs []string
	for key := range configMap {
		if _, ok := props[key]; !ok {
			available := make([]string, 0, len(props))
			for k := range props {
				available = append(available, k)
			}
			sort.Strings(available)
			errs = append(errs, fmt.Sprintf("%s: config key %q not in schema properties (available: %s) — check json struct tags", prefix, key, strings.Join(available, ", ")))
		}
	}
	return errs
}

// shortID returns a readable short identifier for error messages.
func shortID(nodeID, label string) string {
	if label != "" {
		return label
	}
	// Extract component suffix from node ID like "tinysystems-common-module-v0.array-split-sp01"
	parts := strings.Split(nodeID, ".")
	if len(parts) > 1 {
		return parts[len(parts)-1]
	}
	return nodeID
}
