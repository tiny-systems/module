package tools

import (
	"context"
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
			"hint":    "Component notes are abbreviated here. Use get_component_info(component, module) for the full note and port schemas before wiring a component into a flow.",
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
