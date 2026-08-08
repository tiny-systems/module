package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/goccy/go-json"
	"github.com/tiny-systems/module/api/v1alpha1"
	moduleutils "github.com/tiny-systems/module/pkg/utils"
)

// CloneSolutionTool installs a solution into a project. A solution IS a
// project: it may carry many flows, plus scenarios and dashboard widgets.
// The clone mirrors the hosted installer's semantics — full TinyNode CRs
// are reconstructed from the solution's canonical export and applied with
// relabeling, so port configurations, settings schemas, edge configs and
// dashboard labels all arrive intact. Nothing is re-validated or re-built
// piecewise, so nothing can silently drop.
type CloneSolutionTool struct{}

func NewCloneSolutionTool() *CloneSolutionTool {
	return &CloneSolutionTool{}
}

func (t *CloneSolutionTool) Name() string {
	return "clone_solution"
}

func (t *CloneSolutionTool) Description() string {
	return `Install a solution into the current project. A solution is a whole project: ALL of its flows are recreated (with their nodes, edges, configurations, settings schemas and dashboard widgets), and its verification scenarios are applied.

Use search_solutions to find solutions, then clone_solution to install one.

Input:
- solution: slug or UUID from search_solutions / the solution's page URL
- solution_uuid: (deprecated alias of 'solution')

The clone reconstructs full node resources from the solution's canonical export — the same artifact 'tiny publish' produced — so what you get is exactly what the author published. The result lists any modules the solution needs that are NOT installed in this cluster: install them with install_module, the nodes reconcile automatically once their module arrives.`
}

func (t *CloneSolutionTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"solution": map[string]interface{}{
				"type":        "string",
				"description": "Solution slug or UUID from search_solutions",
			},
			"solution_uuid": map[string]interface{}{
				"type":        "string",
				"description": "Deprecated alias of 'solution'",
			},
		},
	}
}

// exportEnvelope is the subset of the publish/export contract the clone
// consumes (services/solution-export on the platform side).
type exportEnvelope struct {
	Version   int                      `json:"version"`
	Type      string                   `json:"type"`
	Title     string                   `json:"title"`
	TinyFlows []exportEnvelopeFlow     `json:"tinyFlows"`
	Elements  []map[string]interface{} `json:"elements"`
	Pages     []exportEnvelopePage     `json:"pages"`
	Scenarios []exportEnvelopeScenario `json:"scenarios"`
}

// exportEnvelopePage mirrors the publish contract's page: a page owns its
// widgets, and each widget carries where it sits.
type exportEnvelopePage struct {
	Name    string                 `json:"name"`
	Title   string                 `json:"title"`
	SortIdx int                    `json:"sortIdx"`
	Widgets []exportEnvelopeWidget `json:"widgets"`
}

type exportEnvelopeWidget struct {
	Port        string `json:"port"`
	Name        string `json:"name"`
	GridX       int    `json:"gridX"`
	GridY       int    `json:"gridY"`
	GridW       int    `json:"gridW"`
	GridH       int    `json:"gridH"`
	SchemaPatch []byte `json:"schemaPatch,omitempty"`
}

type exportEnvelopeFlow struct {
	ResourceName string `json:"resourceName"`
	Name         string `json:"name"`
}

type exportEnvelopeScenario struct {
	Name  string                      `json:"name"`
	Ports []v1alpha1.ScenarioPortData `json:"ports"`
}

func (t *CloneSolutionTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.SolutionExportFetcher == nil {
		return ToolResult{Success: false, Error: "solution export fetch not configured"}
	}
	if execCtx.FlowCreator == nil {
		return ToolResult{Success: false, Error: "flow creator not configured"}
	}
	if execCtx.TinyNodeCRManager == nil {
		return ToolResult{Success: false, Error: "node CR manager not configured"}
	}
	if execCtx.ProjectName == "" {
		return ToolResult{Success: false, Error: "no project in context — pass 'project'"}
	}

	id, _ := input["solution"].(string)
	if id == "" {
		id, _ = input["solution_uuid"].(string)
	}
	if id == "" {
		return ToolResult{Success: false, Error: "'solution' (slug or UUID) is required"}
	}

	raw, err := execCtx.SolutionExportFetcher.FetchSolutionExport(ctx, id)
	if err != nil {
		return ToolResult{Success: false, Error: "failed to fetch solution export: " + err.Error()}
	}
	var export exportEnvelope
	if err := json.Unmarshal(raw, &export); err != nil {
		return ToolResult{Success: false, Error: "invalid solution export: " + err.Error()}
	}
	if export.Type != "solution" || len(export.TinyFlows) == 0 {
		return ToolResult{Success: false, Error: "solution export has no flows"}
	}

	nodes, order, buildErrs := moduleutils.NodesFromGraphElements(export.Elements)
	if len(buildErrs) > 0 {
		return ToolResult{Success: false, Error: fmt.Sprintf("solution export failed reconstruction (%d errors): %s", len(buildErrs), strings.Join(buildErrs, "; "))}
	}
	if len(order) == 0 {
		return ToolResult{Success: false, Error: "solution has no nodes"}
	}

	// Group nodes by their original flow resource name.
	nodesByFlow := map[string][]string{}
	for _, nodeID := range order {
		f := nodes[nodeID].Labels[v1alpha1.FlowNameLabel]
		nodesByFlow[f] = append(nodesByFlow[f], nodeID)
	}

	var (
		flowsCreated []map[string]interface{}
		errors       []string
		warnings     []string
		nameMapping  = map[string]string{} // old node name -> new node name
		flowMapping  = map[string]string{} // old flow resource name -> new
		widgetCount  int
		modulesUsed  = map[string]struct{}{}
	)

	// Phase 0 — create every flow.
	for _, f := range export.TinyFlows {
		newFlow, err := execCtx.FlowCreator.CreateFlow(ctx, execCtx.ProjectName, f.Name)
		if err != nil {
			return ToolResult{Success: false, Error: fmt.Sprintf("failed to create flow %q: %s", f.Name, err.Error())}
		}
		flowMapping[f.ResourceName] = newFlow
		flowsCreated = append(flowsCreated, map[string]interface{}{
			"title": f.Name, "flow": newFlow, "nodes": len(nodesByFlow[f.ResourceName]),
		})
	}

	// Phase 1 — create every node CR, stripped of edges/ports, with a
	// GenerateName so the cluster assigns fresh names (hosted-install
	// semantics). Collect the old→new name mapping.
	for _, oldID := range order {
		src := nodes[oldID]
		oldFlow := src.Labels[v1alpha1.FlowNameLabel]
		newFlow, ok := flowMapping[oldFlow]
		if !ok {
			errors = append(errors, fmt.Sprintf("node %q references flow %q absent from tinyFlows", oldID, oldFlow))
			continue
		}

		nodeCopy := src.DeepCopy()
		nodeCopy.Name = ""
		nodeCopy.GenerateName = moduleutils.GetNodeGenerateName(execCtx.ProjectName, newFlow, nodeCopy.Spec.Module, nodeCopy.Spec.Component)
		nodeCopy.Spec.Edges = nil
		// Keep the port configs that reference nothing — settings, and any
		// default input config. Only From-qualified configs name another
		// node, so only those must wait for the remapping in phase 2.
		//
		// Stripping settings too meant every component booted on its
		// DEFAULTS in the gap between the phases: conversation's default
		// path is /data/conversation.db, which its container cannot create,
		// so a freshly installed solution reported failed runs before the
		// user had touched it.
		var selfContained []v1alpha1.TinyNodePortConfig
		for _, pc := range nodeCopy.Spec.Ports {
			if pc.From == "" {
				selfContained = append(selfContained, pc)
			}
		}
		nodeCopy.Spec.Ports = selfContained
		nodeCopy.Labels[v1alpha1.ProjectNameLabel] = execCtx.ProjectName
		nodeCopy.Labels[v1alpha1.FlowNameLabel] = newFlow

		newName, err := execCtx.TinyNodeCRManager.CreateNodeCR(ctx, nodeCopy)
		if err != nil {
			return ToolResult{Success: false, Error: fmt.Sprintf("failed to create node %q: %s", oldID, err.Error())}
		}
		nameMapping[oldID] = newName
		modulesUsed[src.Spec.Module] = struct{}{}
		if src.Labels[v1alpha1.DashboardLabel] != "" {
			widgetCount++
		}
	}

	// Phase 2 — re-apply edges and port configs with names remapped.
	// Update with a small conflict-retry: the operator reconciles freshly
	// created nodes concurrently.
	for _, oldID := range order {
		src := nodes[oldID]
		newName, ok := nameMapping[oldID]
		if !ok {
			continue
		}
		if len(src.Spec.Edges) == 0 && len(src.Spec.Ports) == 0 {
			continue
		}
		newFlow := flowMapping[src.Labels[v1alpha1.FlowNameLabel]]

		var lastErr error
		for attempt := 0; attempt < 5; attempt++ {
			live, err := execCtx.TinyNodeCRManager.GetNodeCR(ctx, newName)
			if err != nil {
				lastErr = err
				break
			}
			live.Spec.Edges = nil
			for _, edge := range src.Spec.Edges {
				newEdge := edge
				newEdge.To = remapNodeRef(edge.To, nameMapping)
				newEdge.ID = fmt.Sprintf("%s_%s-%s", newName, edge.Port, strings.ReplaceAll(newEdge.To, ":", "_"))
				newEdge.FlowID = newFlow
				live.Spec.Edges = append(live.Spec.Edges, newEdge)
			}
			live.Spec.Ports = nil
			for _, port := range src.Spec.Ports {
				newPort := port
				newPort.From = remapNodeRef(port.From, nameMapping)
				if newPort.FlowID != "" {
					newPort.FlowID = newFlow
				}
				live.Spec.Ports = append(live.Spec.Ports, newPort)
			}
			lastErr = execCtx.TinyNodeCRManager.UpdateNodeCR(ctx, live)
			if lastErr == nil || !execCtx.TinyNodeCRManager.IsConflict(lastErr) {
				break
			}
		}
		if lastErr != nil {
			return ToolResult{Success: false, Error: fmt.Sprintf("failed to wire node %q: %s", newName, lastErr.Error())}
		}
	}

	// Phase 2.5 — dashboard pages. A page's widget entries address nodes by
	// port, so they need the same remapping the edges did; without this the
	// layout the author arranged is dropped and every widget lands on the
	// default page at a default size.
	pagesApplied := 0
	if execCtx.DashboardPageApplier != nil {
		for _, page := range export.Pages {
			title := page.Title
			if title == "" {
				title = page.Name
			}
			widgets := make([]v1alpha1.TinyWidget, 0, len(page.Widgets))
			for _, w := range page.Widgets {
				port := remapNodeRef(w.Port, nameMapping)
				if port == w.Port && !strings.Contains(w.Port, ":") {
					continue // not a port reference we understand
				}
				widgets = append(widgets, v1alpha1.TinyWidget{
					Port:        port,
					Name:        w.Name,
					GridX:       w.GridX,
					GridY:       w.GridY,
					GridW:       w.GridW,
					GridH:       w.GridH,
					SchemaPatch: w.SchemaPatch,
				})
			}
			if len(widgets) == 0 {
				continue
			}
			if err := execCtx.DashboardPageApplier.ApplyDashboardPage(ctx, execCtx.ProjectName, title, page.SortIdx, widgets); err != nil {
				warnings = append(warnings, fmt.Sprintf("dashboard page %q not applied: %s", title, err.Error()))
				continue
			}
			pagesApplied++
		}
	} else if len(export.Pages) > 0 {
		warnings = append(warnings, fmt.Sprintf("%d dashboard page(s) were not applied (no page applier configured)", len(export.Pages)))
	}

	// Phase 3 — scenarios, with node references inside port names remapped.
	scenariosApplied := 0
	if execCtx.ScenarioApplier != nil {
		for _, sc := range export.Scenarios {
			if sc.Name == "" || len(sc.Ports) == 0 {
				continue
			}
			remapped := make([]v1alpha1.ScenarioPortData, 0, len(sc.Ports))
			for _, p := range sc.Ports {
				np := p
				np.Port = remapNodeRef(p.Port, nameMapping)
				remapped = append(remapped, np)
			}
			if err := execCtx.ScenarioApplier.ApplyScenario(ctx, execCtx.ProjectName, sc.Name, remapped); err != nil {
				warnings = append(warnings, fmt.Sprintf("scenario %q not applied: %s", sc.Name, err.Error()))
				continue
			}
			scenariosApplied++
		}
	} else if len(export.Scenarios) > 0 {
		warnings = append(warnings, fmt.Sprintf("%d scenarios in the solution were not applied (no scenario applier configured)", len(export.Scenarios)))
	}

	// Missing modules — loud, actionable.
	var missingModules []string
	if execCtx.ModuleCatalog != nil {
		installed := map[string]struct{}{}
		if mods, err := execCtx.ModuleCatalog.ListModules(ctx); err == nil {
			for _, m := range mods {
				installed[m.Name] = struct{}{}
			}
			for m := range modulesUsed {
				if _, ok := installed[m]; !ok {
					missingModules = append(missingModules, m)
				}
			}
			sort.Strings(missingModules)
		}
	}

	output := map[string]interface{}{
		"solution":          export.Title,
		"project":           execCtx.ProjectName,
		"flows_created":     flowsCreated,
		"nodes_created":     len(nameMapping),
		"scenarios_applied": scenariosApplied,
		"dashboard_widgets": widgetCount,
		"dashboard_pages":   pagesApplied,
	}
	if len(missingModules) > 0 {
		output["missing_modules"] = missingModules
		output["hint"] = "Install the missing modules with install_module — the cloned nodes reconcile automatically once their module arrives."
	}
	if len(warnings) > 0 {
		output["warnings"] = warnings
	}
	if len(errors) > 0 {
		output["errors"] = errors
		return ToolResult{Success: false, Error: strings.Join(errors, "; "), Output: output}
	}
	return ToolResult{Success: true, Output: output}
}

// remapNodeRef rewrites "nodeName:port" references through the mapping,
// leaving refs to unknown nodes untouched.
func remapNodeRef(ref string, mapping map[string]string) string {
	parts := strings.SplitN(ref, ":", 2)
	if len(parts) != 2 {
		return ref
	}
	if newName, ok := mapping[parts[0]]; ok {
		return newName + ":" + parts[1]
	}
	return ref
}
