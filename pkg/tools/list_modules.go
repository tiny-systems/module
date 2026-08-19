package tools

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/tiny-systems/module/api/v1alpha1"
)

// ListModulesTool lists available modules via the injected ModuleCatalog.
// Backend is pluggable: hosted platform reads from DB, public MCP reads
// TinyModule CRDs from the current namespace.
type ListModulesTool struct{}

func NewListModulesTool() *ListModulesTool {
	return &ListModulesTool{}
}

func (t *ListModulesTool) Name() string {
	return "list_modules"
}

func (t *ListModulesTool) Description() string {
	return "List all available modules. Each module contains components that can be added to flows."
}

func (t *ListModulesTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
		"required":   []string{},
	}
}

func (t *ListModulesTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.ModuleCatalog == nil {
		return ToolResult{
			Success: false,
			Error:   "module catalog not configured",
		}
	}

	modules, err := execCtx.ModuleCatalog.ListModules(ctx)
	if err != nil {
		return ToolResult{
			Success: false,
			Error:   "failed to list modules: " + err.Error(),
		}
	}

	result := make([]map[string]interface{}, 0, len(modules))
	for _, m := range modules {
		moduleInfo := map[string]interface{}{
			"name": m.Name,
		}
		// Modules published from a CRD carry no description — every one of
		// them shipped an empty string, paid for on every session.
		if m.Description != "" {
			moduleInfo["description"] = m.Description
		}
		if m.Version != "" {
			moduleInfo["version"] = m.Version
		}

		components := make([]map[string]interface{}, 0, len(m.Components))
		for _, c := range m.Components {
			compInfo := map[string]interface{}{
				"name":        c.Name,
				"description": c.Description,
			}
			// The first sentence, not the whole note. This catalog exists to
			// let a caller CHOOSE a component; the full behaviour note is what
			// it needs to WIRE one, and get_component_info returns that
			// verbatim — which the flow-building rules already require calling
			// before wiring anything. Carried in full, these notes were 69% of
			// this tool's output and were paid twice per component.
			if summary := firstSentence(c.Info); summary != "" {
				compInfo["info"] = summary
			}
			if in := wireablePorts(c.InputPorts); len(in) > 0 {
				compInfo["input_ports"] = in
			}
			// A component with no output ports is a sink, which a caller must
			// know — but it reads that off an absent output_ports just as well
			// as off a boolean restating it.
			if out := wireablePorts(c.OutputPorts); len(out) > 0 {
				compInfo["output_ports"] = out
			}
			// A mandatory setting is the difference between a component that
			// fits and one that cannot be used at all, and prose routinely
			// omits it: "get status of pods matching a label selector" reads
			// as a capability, not as a selector you are obliged to supply.
			// Derived from the schema rather than written by hand, so it
			// cannot drift from the component.
			if req := requiredInputs(c); len(req) > 0 {
				compInfo["requires"] = req
			}
			// What a component hands back decides whether it answers the
			// question at all. Two pod components differ entirely in that one
			// respect — one returns the pods, the other returns counters
			// bucketed by phase — and choosing between them was impossible
			// without fetching both in full.
			if shapes := outputShapes(c); len(shapes) > 0 {
				compInfo["outputs"] = shapes
			}
			components = append(components, compInfo)
		}
		moduleInfo["components"] = components
		result = append(result, moduleInfo)
	}

	return ToolResult{
		Success: true,
		Output: map[string]interface{}{
			"modules": result,
			"total":   len(result),
			"hint": "Component notes are abbreviated here, and `requires` lists the fields a component cannot run without (its schema's required fields, minus those with a default). " +
				"`outputs` names each output port's top-level fields, with [] marking an array. " +
				"Use get_component_info(component, module) for the full note, value types and constraints before wiring a component into a flow.",
		},
	}
}

var _ Tool = (*ListModulesTool)(nil)

// wireablePorts drops the system ports a caller can never wire. They were a
// third of every port name this tool listed, and get_component_info has always
// filtered them — listing them here only invited an agent to try.
func wireablePorts(names []string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		switch name {
		case v1alpha1.SettingsPort, v1alpha1.ControlPort, v1alpha1.ReconcilePort, v1alpha1.ClientPort, v1alpha1.IdentityPort:
			continue
		}
		out = append(out, name)
	}
	return out
}

// firstSentence returns the opening sentence of a component note, which is
// written to say what the component IS — enough to choose it from a list.
//
// Abbreviation is only worth doing when it actually saves something: a note
// already short enough is returned whole rather than cut mid-thought, and a
// sentence ending is only honoured when what follows looks like prose rather
// than a decimal point or an abbreviation.
func firstSentence(info string) string {
	const (
		keepWhole   = 120 // a note this short is already a summary
		minSentence = 40  // shorter than this says less than the name does
	)
	info = strings.TrimSpace(info)
	if len(info) <= keepWhole {
		return info
	}
	for i, r := range info {
		if r != '.' && r != '!' && r != '?' {
			continue
		}
		rest := info[i+1:]
		if rest == "" {
			return info
		}
		next := rest[0]
		if next != ' ' && next != '\n' {
			continue
		}
		if i+1 < minSentence {
			continue // "Display." tells a caller nothing; keep reading
		}
		return info[:i+1]
	}
	return info
}

// requiredInputs names what a component cannot run without, as dotted paths
// into its settings and its input ports.
//
// This is the fact that decides whether a component FITS, and prose routinely
// omits it: "get status of pods matching a label selector" reads as a
// capability, when the selector is in truth mandatory — which makes that
// component unusable for "all pods in a namespace". Derived from the schemas
// rather than written by hand, so it cannot drift from the component.
func requiredInputs(c ComponentInfo) []string {
	out := make([]string, 0, 4)
	for _, name := range requiredFieldsIn(c.SettingsSchema) {
		out = append(out, "settings."+name)
	}
	for _, d := range c.InputPortDetails {
		if len(wireablePorts([]string{d.Name})) == 0 {
			continue // system port: not something a caller supplies on an edge
		}
		for _, name := range requiredFieldsIn(d.Schema) {
			out = append(out, d.Name+"."+name)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// requiredFieldsIn lists a schema's required fields, skipping those a caller
// does not actually have to supply: a field with a default is already
// answered, and a boolean toggle always has a usable value in false. Listing
// those would bury the one field that genuinely blocks you.
func requiredFieldsIn(schema json.RawMessage) []string {
	if len(schema) == 0 {
		return nil
	}
	var root map[string]interface{}
	if err := json.Unmarshal(schema, &root); err != nil {
		return nil
	}

	required, properties := requiredAndProperties(root)

	out := make([]string, 0, len(required))
	for _, item := range required {
		name, ok := item.(string)
		if !ok || name == "" {
			continue
		}
		if prop, ok := properties[name].(map[string]interface{}); ok {
			if _, hasDefault := prop["default"]; hasDefault {
				continue
			}
			if prop["type"] == "boolean" {
				continue
			}
		}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// requiredAndProperties reads a schema's required list and properties, whether
// they sit at the root or behind a $ref into the schema's own $defs.
func requiredAndProperties(root map[string]interface{}) ([]interface{}, map[string]interface{}) {
	// Properties at the root stand on their own: a schema commonly lists what
	// it carries without marking any of it required, and keying off `required`
	// alone made those schemas read as empty.
	if props, ok := root["properties"].(map[string]interface{}); ok {
		required, _ := root["required"].([]interface{})
		return required, props
	}

	ref, ok := root["$ref"].(string)
	if !ok {
		return nil, nil
	}
	const prefix = "#/$defs/"
	if !strings.HasPrefix(ref, prefix) {
		return nil, nil
	}
	defs, ok := root["$defs"].(map[string]interface{})
	if !ok {
		return nil, nil
	}
	def, ok := defs[strings.TrimPrefix(ref, prefix)].(map[string]interface{})
	if !ok {
		return nil, nil
	}
	required, _ := def["required"].([]interface{})
	props, _ := def["properties"].(map[string]interface{})
	return required, props
}

// outputShapes names the top-level fields each output port carries, so a
// caller can tell what a component actually answers with before fetching it.
//
// Names and array-ness only: enough to choose between components, not enough
// to wire one, which is what get_component_info is for. Nested structure is
// deliberately not walked — it would cost more than the catalog saves and
// says little about fit.
func outputShapes(c ComponentInfo) map[string][]string {
	shapes := make(map[string][]string)
	for _, d := range c.OutputPortDetails {
		if len(wireablePorts([]string{d.Name})) == 0 {
			continue
		}
		if fields := topLevelFields(d.Schema); len(fields) > 0 {
			shapes[d.Name] = fields
		}
	}
	if len(shapes) == 0 {
		return nil
	}
	return shapes
}

// topLevelFields lists a schema's own property names, sorted so the catalog
// is stable between calls, marking arrays with a trailing [].
func topLevelFields(schema json.RawMessage) []string {
	if len(schema) == 0 {
		return nil
	}
	var root map[string]interface{}
	if err := json.Unmarshal(schema, &root); err != nil {
		return nil
	}
	_, properties := requiredAndProperties(root)
	if len(properties) == 0 {
		return nil
	}

	fields := make([]string, 0, len(properties))
	for name, raw := range properties {
		// Nearly every port carries a passthrough context, so naming it
		// distinguishes nothing while costing something on every component.
		// The guide covers context passthrough as a rule of the system.
		if name == "context" {
			continue
		}
		if prop, ok := raw.(map[string]interface{}); ok && prop["type"] == "array" {
			name += "[]"
		}
		fields = append(fields, name)
	}
	sort.Strings(fields)
	return fields
}
