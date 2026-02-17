package utils

import (
	"encoding/json"
	"strings"

	"github.com/rs/zerolog/log"
)

// ExportFlow represents a flow in the export format
type ExportFlow struct {
	ResourceName string `json:"resourceName"`
	Name         string `json:"name"`
}

// ExportPage represents a dashboard page in the export format
type ExportPage struct {
	Name    string         `json:"name"`
	Title   string         `json:"title"`
	SortIdx int            `json:"sortIdx"`
	Widgets []ExportWidget `json:"widgets"`
}

// ExportWidget represents a widget in the export format
type ExportWidget struct {
	Port        string          `json:"port"`
	Name        string          `json:"name"`
	GridX       int             `json:"gridX"`
	GridY       int             `json:"gridY"`
	GridW       int             `json:"gridW"`
	GridH       int             `json:"gridH"`
	SchemaPatch json.RawMessage `json:"schemaPatch,omitempty"`
}

// ProjectExport represents the full project export format
type ProjectExport struct {
	Version     int                      `json:"version"`
	Description string                   `json:"description,omitempty"`
	TinyFlows   []ExportFlow             `json:"tinyFlows"`
	Elements    []map[string]interface{} `json:"elements"`
	Pages       []ExportPage             `json:"pages"`
}

// CurrentExportVersion is the current version of the export format
const CurrentExportVersion = 1

// StripSchemaInternalFields removes runtime-internal fields from schema
// definitions in all elements. The "path" field is generated at runtime
// by the SDK and should not appear in import/export data.
func StripSchemaInternalFields(data *ProjectExport) {
	for _, elem := range data.Elements {
		elemType, _ := elem["type"].(string)
		if elemType != TinyNodeType {
			continue
		}
		dataMap, ok := elem["data"].(map[string]interface{})
		if !ok {
			continue
		}
		handles, ok := dataMap["handles"].([]interface{})
		if !ok {
			continue
		}
		for _, h := range handles {
			handleMap, ok := h.(map[string]interface{})
			if !ok {
				continue
			}
			schemaMap, ok := handleMap["schema"].(map[string]interface{})
			if !ok {
				continue
			}
			defs, ok := schemaMap["$defs"].(map[string]interface{})
			if !ok {
				continue
			}
			for _, defRaw := range defs {
				defMap, ok := defRaw.(map[string]interface{})
				if !ok {
					continue
				}
				if path, ok := defMap["path"].(string); ok && strings.HasPrefix(path, "$") {
					delete(defMap, "path")
				}
			}
		}
	}
}

// ValidateProjectExport validates the import data and logs warnings for issues.
// This is a warn-only validation - issues are logged but import can proceed.
// Returns the set of valid flow resource names for reference validation.
func ValidateProjectExport(data *ProjectExport) map[string]bool {
	// Build a set of valid flow resource names for reference validation
	flowSet := make(map[string]bool)
	for _, flow := range data.TinyFlows {
		flowSet[flow.ResourceName] = true
	}

	// Warn if no flows defined
	if len(data.TinyFlows) == 0 {
		log.Warn().Msg("import validation: tinyFlows array is empty - no flows will be created")
	}

	// Validate each element
	for i, elem := range data.Elements {
		elemID, _ := elem["id"].(string)
		elemType, _ := elem["type"].(string)

		// Determine if this is a node or edge based on type
		if elemType == TinyNodeType {
			validateNodeElement(i, elemID, elem, flowSet)
		} else if elemType == TinyEdgeType || elemType == "edge" {
			validateEdgeElement(i, elemID, elem, flowSet)
		}
	}

	// Validate pages
	for i := range data.Pages {
		validatePage(i, &data.Pages[i])
	}

	return flowSet
}

// validateNodeElement validates a node element and logs warnings for issues.
func validateNodeElement(index int, elemID string, elem map[string]interface{}, flowSet map[string]bool) {
	logCtx := log.With().Int("elementIndex", index).Str("elementId", elemID).Logger()

	// Check for empty ID
	if elemID == "" {
		logCtx.Warn().Msg("import validation: node element has empty id")
	}

	// Validate flow reference
	flowName, _ := elem["flow"].(string)
	if flowName == "" {
		logCtx.Warn().Str("field", "flow").Msg("import validation: node element missing flow field")
	} else if !flowSet[flowName] {
		logCtx.Warn().
			Str("field", "flow").
			Str("flowName", flowName).
			Msg("import validation: node element references non-existent flow - ensure flow is defined in tinyFlows array")
	}

	// Validate data object
	dataMap, ok := elem["data"].(map[string]interface{})
	if !ok {
		logCtx.Warn().Str("field", "data").Msg("import validation: node element missing data object")
		return
	}

	// Check required data fields
	component, _ := dataMap["component"].(string)
	if component == "" {
		logCtx.Warn().Str("field", "data.component").Msg("import validation: node element missing component")
	}

	module, _ := dataMap["module"].(string)
	if module == "" {
		logCtx.Warn().Str("field", "data.module").Msg("import validation: node element missing module")
	}
}

// validateEdgeElement validates an edge element and logs warnings for issues.
func validateEdgeElement(index int, elemID string, elem map[string]interface{}, flowSet map[string]bool) {
	logCtx := log.With().Int("elementIndex", index).Str("elementId", elemID).Logger()

	// Validate connection fields
	source, _ := elem["source"].(string)
	if source == "" {
		logCtx.Warn().Str("field", "source").Msg("import validation: edge element missing source node")
	}

	target, _ := elem["target"].(string)
	if target == "" {
		logCtx.Warn().Str("field", "target").Msg("import validation: edge element missing target node")
	}

	sourceHandle, _ := elem["sourceHandle"].(string)
	if sourceHandle == "" {
		logCtx.Warn().Str("field", "sourceHandle").Msg("import validation: edge element missing sourceHandle")
	}

	targetHandle, _ := elem["targetHandle"].(string)
	if targetHandle == "" {
		logCtx.Warn().Str("field", "targetHandle").Msg("import validation: edge element missing targetHandle")
	}

	// Validate flow reference
	flowName, _ := elem["flow"].(string)
	if flowName == "" {
		logCtx.Warn().Str("field", "flow").Msg("import validation: edge element missing flow field")
	} else if !flowSet[flowName] {
		logCtx.Warn().
			Str("field", "flow").
			Str("flowName", flowName).
			Msg("import validation: edge element references non-existent flow - ensure flow is defined in tinyFlows array")
	}
}

// validatePage validates a page and its widgets, logging warnings for issues.
func validatePage(index int, page *ExportPage) {
	logCtx := log.With().Int("pageIndex", index).Str("pageName", page.Name).Logger()

	for i, widget := range page.Widgets {
		widgetLogCtx := logCtx.With().Int("widgetIndex", i).Str("widgetPort", widget.Port).Logger()

		// Validate port format: must be "nodeId:portName"
		if widget.Port == "" {
			widgetLogCtx.Warn().Str("field", "port").Msg("import validation: widget missing port field")
			continue
		}

		// Use SplitN to only split on first colon (node IDs may contain colons)
		parts := strings.SplitN(widget.Port, ":", 2)
		if len(parts) != 2 {
			widgetLogCtx.Warn().
				Str("field", "port").
				Msg("import validation: widget port has invalid format - expected 'nodeId:portName'")
			continue
		}

		if parts[0] == "" {
			widgetLogCtx.Warn().
				Str("field", "port").
				Msg("import validation: widget port has empty node ID")
		}
		if parts[1] == "" {
			widgetLogCtx.Warn().
				Str("field", "port").
				Msg("import validation: widget port has empty port name")
		}
	}
}

// ParseProjectExport parses JSON data into a ProjectExport struct
func ParseProjectExport(data []byte) (*ProjectExport, error) {
	var export ProjectExport
	if err := json.Unmarshal(data, &export); err != nil {
		return nil, err
	}
	return &export, nil
}

// GetNodeIDFromElement extracts the node ID from an element map
func GetNodeIDFromElement(elem map[string]interface{}) string {
	id, _ := elem["id"].(string)
	return id
}

// GetFlowFromElement extracts the flow name from an element map
func GetFlowFromElement(elem map[string]interface{}) string {
	flow, _ := elem["flow"].(string)
	return flow
}

// TranslateWidgetPort translates a widget port reference from old node ID to new node name
// Returns the new port string and a boolean indicating if translation was successful
func TranslateWidgetPort(port string, nodeIDMap map[string]string) (string, bool) {
	// Use SplitN to only split on first colon (node IDs may contain colons)
	parts := strings.SplitN(port, ":", 2)
	if len(parts) != 2 {
		return "", false
	}

	oldNodeID := parts[0]
	portName := parts[1]

	newNodeName, ok := nodeIDMap[oldNodeID]
	if !ok || newNodeName == "" {
		return "", false
	}

	return newNodeName + ":" + portName, true
}
