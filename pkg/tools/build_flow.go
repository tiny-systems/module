package tools

import (
	"context"
	"fmt"
	"strings"
)

// BuildFlowTool creates an entire flow (nodes + edges + configuration) in one call.
// This dramatically reduces the number of LLM tool calls needed to build a flow.
type BuildFlowTool struct{}

func NewBuildFlowTool() *BuildFlowTool {
	return &BuildFlowTool{}
}

func (t *BuildFlowTool) Name() string {
	return "build_flow"
}

func (t *BuildFlowTool) Description() string {
	return `Create a complete flow with nodes, edges, and configuration in ONE call.

Use this instead of incremental edit_flow calls when constructing a whole flow at once.

Input:
- nodes: array of nodes to create (alias, component, module, optional settings/settings_schema)
- edges: array of edges using "alias:port" references (optional configuration/schema)

Node aliases are local references (e.g., "my-server") used in edge definitions.
The system generates real node IDs and resolves aliases automatically.

IMPORTANT: Settings are applied BEFORE edges, so settings that create ports (e.g., router routes)
will be available for edge connections.

Example - HTTP echo server:
  build_flow(
    nodes: [
      {alias: "srv", component: "server", module: "tinysystems/http-module-v0"},
      {alias: "resp", component: "response", module: "tinysystems/http-module-v0"}
    ],
    edges: [
      {from: "srv:request", to: "resp:response",
       configuration: {status_code: 200, body: "{{$.body}}", context: "{{$.context}}"}}
    ]
  )

Example - Ticker with context (ticker emits settings at root level, so first hop uses {{$}} not {{$.context}}):
  build_flow(
    nodes: [
      {alias: "tick", component: "ticker", module: "tinysystems/common-module-v1",
       settings: {delay: 30000, context: {token: "placeholder"}},
       settings_schema: {context: {type: "object", properties: {token: {type: "string", title: "Token"}}}}},
      {alias: "client", component: "client", module: "tinysystems/http-module-v0"}
    ],
    edges: [
      {from: "tick:out", to: "client:request",
       configuration: {url: "https://api.example.com", method: "GET",
                        headers: {"Authorization": "Bearer {{$.token}}"},
                        context: "{{$}}"}}
    ]
  )

Returns created node IDs, edge IDs, ports, and any errors/warnings.
If some operations fail, partial results are returned so you can fix specific items with individual tools.`
}

func (t *BuildFlowTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"nodes": map[string]interface{}{
				"type":        "array",
				"description": "Nodes to create",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"alias": map[string]interface{}{
							"type":        "string",
							"description": "Unique reference name for this node (used in edge from/to)",
						},
						"component": map[string]interface{}{
							"type":        "string",
							"description": "Component name (e.g., 'server', 'router', 'ticker')",
						},
						"module": map[string]interface{}{
							"type":        "string",
							"description": "Module name (e.g., 'tinysystems/http-module-v0')",
						},
						"settings": map[string]interface{}{
							"type":        "object",
							"description": "Optional settings for _settings port (same as edit_flow action=configure_node)",
						},
						"settings_schema": map[string]interface{}{
							"type":        "object",
							"description": "Optional schema for configurable fields in settings (e.g., context type definition)",
						},
					},
					"required": []string{"alias", "component", "module"},
				},
			},
			"edges": map[string]interface{}{
				"type":        "array",
				"description": "Edges to create between nodes",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"from": map[string]interface{}{
							"type":        "string",
							"description": "Source in 'alias:port' format (e.g., 'my-server:request')",
						},
						"to": map[string]interface{}{
							"type":        "string",
							"description": "Target in 'alias:port' format (e.g., 'my-handler:input')",
						},
						"configuration": map[string]interface{}{
							"type":        "object",
							"description": "Edge data mapping using {{expression}} syntax",
						},
						"schema": map[string]interface{}{
							"type":        "object",
							"description": "Optional schema extensions for configurable fields on target port",
						},
					},
					"required": []string{"from", "to"},
				},
			},
		},
		"required": []string{"nodes"},
	}
}

func (t *BuildFlowTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	// Validate required interfaces
	if execCtx.NodeAdder == nil {
		return ToolResult{Success: false, Error: "node adder not configured"}
	}
	if execCtx.EdgeAdder == nil {
		return ToolResult{Success: false, Error: "edge adder not configured"}
	}

	// Parse input
	nodesRaw, _ := input["nodes"].([]interface{})
	edgesRaw, _ := input["edges"].([]interface{})

	if len(nodesRaw) == 0 {
		return ToolResult{Success: false, Error: "at least one node is required"}
	}

	// Parse node specs
	type nodeSpec struct {
		Alias          string
		Component      string
		Module         string
		Settings       map[string]interface{}
		SettingsSchema map[string]interface{}
	}

	nodes := make([]nodeSpec, 0, len(nodesRaw))
	aliases := make(map[string]bool)

	for i, raw := range nodesRaw {
		m, ok := raw.(map[string]interface{})
		if !ok {
			return ToolResult{Success: false, Error: fmt.Sprintf("nodes[%d]: expected object", i)}
		}

		alias, _ := m["alias"].(string)
		component, _ := m["component"].(string)
		module, _ := m["module"].(string)

		if alias == "" {
			return ToolResult{Success: false, Error: fmt.Sprintf("nodes[%d]: alias is required", i)}
		}
		if component == "" {
			return ToolResult{Success: false, Error: fmt.Sprintf("nodes[%d] (%s): component is required", i, alias)}
		}
		if module == "" {
			return ToolResult{Success: false, Error: fmt.Sprintf("nodes[%d] (%s): module is required", i, alias)}
		}
		if aliases[alias] {
			return ToolResult{Success: false, Error: fmt.Sprintf("nodes[%d]: duplicate alias '%s'", i, alias)}
		}
		aliases[alias] = true

		settings, _ := m["settings"].(map[string]interface{})
		settingsSchema, _ := m["settings_schema"].(map[string]interface{})

		nodes = append(nodes, nodeSpec{
			Alias:          alias,
			Component:      component,
			Module:         module,
			Settings:       settings,
			SettingsSchema: settingsSchema,
		})
	}

	// Parse edge specs
	type edgeSpec struct {
		From          string // alias:port
		To            string // alias:port
		Configuration map[string]interface{}
		Schema        map[string]interface{}
		FromAlias     string
		FromPort      string
		ToAlias       string
		ToPort        string
	}

	edges := make([]edgeSpec, 0, len(edgesRaw))
	for i, raw := range edgesRaw {
		m, ok := raw.(map[string]interface{})
		if !ok {
			return ToolResult{Success: false, Error: fmt.Sprintf("edges[%d]: expected object", i)}
		}

		from, _ := m["from"].(string)
		to, _ := m["to"].(string)

		if from == "" || to == "" {
			return ToolResult{Success: false, Error: fmt.Sprintf("edges[%d]: from and to are required", i)}
		}

		fromAlias, fromPort, ok := parseAliasPort(from)
		if !ok {
			return ToolResult{Success: false, Error: fmt.Sprintf("edges[%d]: invalid from format '%s', expected 'alias:port'", i, from)}
		}
		toAlias, toPort, ok := parseAliasPort(to)
		if !ok {
			return ToolResult{Success: false, Error: fmt.Sprintf("edges[%d]: invalid to format '%s', expected 'alias:port'", i, to)}
		}

		if !aliases[fromAlias] {
			return ToolResult{Success: false, Error: fmt.Sprintf("edges[%d]: unknown node alias '%s' in from", i, fromAlias)}
		}
		if !aliases[toAlias] {
			return ToolResult{Success: false, Error: fmt.Sprintf("edges[%d]: unknown node alias '%s' in to", i, toAlias)}
		}

		configuration, _ := m["configuration"].(map[string]interface{})
		edgeSchema, _ := m["schema"].(map[string]interface{})

		edges = append(edges, edgeSpec{
			From:          from,
			To:            to,
			FromAlias:     fromAlias,
			FromPort:      fromPort,
			ToAlias:       toAlias,
			ToPort:        toPort,
			Configuration: configuration,
			Schema:        edgeSchema,
		})
	}

	// Validate components upfront before any mutations
	// componentByAlias is needed for the schema-strictness check on
	// edges, which has to look at the target component's settings
	// schema. Built during the existence pass so we don't re-fetch.
	componentByAlias := map[string]*ComponentInfo{}

	if execCtx.ModuleCatalog != nil {
		moduleCache := make(map[string]*ModuleInfo)
		for _, n := range nodes {
			if _, cached := moduleCache[n.Module]; !cached {
				info, err := execCtx.ModuleCatalog.GetModule(ctx, n.Module)
				if err != nil {
					return ToolResult{Success: false, Error: fmt.Sprintf("failed to lookup module '%s': %s", n.Module, err.Error())}
				}
				if info == nil {
					return ToolResult{Success: false, Error: fmt.Sprintf("module '%s' not found. Check installed modules.", n.Module)}
				}
				moduleCache[n.Module] = info
			}
			info := moduleCache[n.Module]
			found := false
			var available []string
			for i := range info.Components {
				c := &info.Components[i]
				available = append(available, c.Name)
				if c.Name == n.Component {
					found = true
					componentByAlias[n.Alias] = c
				}
			}
			if !found {
				return ToolResult{Success: false, Error: fmt.Sprintf("component '%s' not found in module '%s'. Available: %v", n.Component, n.Module, available)}
			}
		}
	}

	// Schema handling for filled configurable fields. Missing schemas are
	// DERIVED, not rejected: settings are literal data (value types are
	// ground truth), and edge configurations are typed by resolving each
	// expression against the source port schema + source node example —
	// unresolvable expressions stay `{}` (untyped, never wrong). This
	// replaces the "declare a schema for context on every edge" reject,
	// which was the single biggest annotation tax in flow authoring.
	// User-provided schema entries always win over derived ones.
	// unknownSettings still rejects — mis-named settings silently no-op.
	var issues []string
	for i := range nodes {
		n := &nodes[i]
		comp := componentByAlias[n.Alias]
		if comp == nil || len(n.Settings) == 0 {
			continue
		}
		settingsSchema := portSchemaBytes(comp, "_settings", true)
		configurable := configurableFieldsIn(settingsSchema)
		n.SettingsSchema = fillMissingSchemas(n.Settings, n.SettingsSchema, configurable, nil)
		if u := unknownSettings(n.Alias, n.Settings, settingsSchema); u != "" {
			issues = append(issues, u)
		}
	}
	srcSettings := map[string]map[string]interface{}{}
	for _, n := range nodes {
		srcSettings[n.Alias] = n.Settings
	}
	for i := range edges {
		e := &edges[i]
		target := componentByAlias[e.ToAlias]
		if target == nil || len(e.Configuration) == 0 {
			continue
		}
		// e.ToPort is the receiving (input) port of the target
		// component — look it up in InputPortDetails, not output.
		targetSchema := portSchemaBytes(target, e.ToPort, true)
		configurable := configurableFieldsIn(targetSchema)
		resolve := exampleResolver(
			portSchemaBytes(componentByAlias[e.FromAlias], e.FromPort, false),
			srcSettings[e.FromAlias],
		)
		e.Schema = fillMissingSchemas(e.Configuration, e.Schema, configurable, resolve)
	}
	if len(issues) > 0 {
		return ToolResult{
			Success: false,
			Error: fmt.Sprintf("build_flow found %d issue(s) — fix all of these, then resend the whole call:\n- %s",
				len(issues), strings.Join(issues, "\n- ")),
		}
	}

	// Auto-create flow if it doesn't exist
	if execCtx.FlowCreator != nil && execCtx.FlowName != "" {
		_, _ = execCtx.FlowCreator.CreateFlow(ctx, execCtx.ProjectName, execCtx.FlowName)
		// Ignore error — flow may already exist
	}

	// Execute: create nodes
	aliasToNodeID := make(map[string]string)
	aliasToPortList := make(map[string][]string)
	var nodesCreated []map[string]interface{}
	var errors []string
	var warnings []string

	for _, n := range nodes {
		if ctx.Err() != nil {
			errors = append(errors, fmt.Sprintf("cancelled during node creation (%s)", n.Alias))
			break
		}

		result, err := execCtx.NodeAdder.AddNode(ctx, execCtx.ProjectName, execCtx.FlowName, n.Component, n.Module, execCtx.PositionTracker)
		if err != nil {
			errors = append(errors, fmt.Sprintf("node '%s': %s", n.Alias, err.Error()))
			continue
		}

		aliasToNodeID[n.Alias] = result.NodeID
		aliasToPortList[n.Alias] = result.Ports

		nodesCreated = append(nodesCreated, map[string]interface{}{
			"alias":   n.Alias,
			"node_id": result.NodeID,
			"ports":   result.Ports,
		})
	}

	// Execute: configure node settings (before edges — settings may create new ports)
	for _, n := range nodes {
		if n.Settings == nil {
			continue
		}
		nodeID, ok := aliasToNodeID[n.Alias]
		if !ok {
			continue // node failed to create
		}
		if ctx.Err() != nil {
			errors = append(errors, fmt.Sprintf("cancelled during settings (%s)", n.Alias))
			break
		}
		if execCtx.NodeSettingsConfigurer == nil {
			warnings = append(warnings, fmt.Sprintf("node '%s': settings configurer not available, skipping settings", n.Alias))
			continue
		}

		result, err := execCtx.NodeSettingsConfigurer.ConfigureNodeSettings(ctx, execCtx.ProjectName, execCtx.FlowName, nodeID, n.Settings, n.SettingsSchema)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("node '%s' settings: %s", n.Alias, err.Error()))
			continue
		}
		if !result.Valid {
			msg := fmt.Sprintf("node '%s' settings rejected: %s", n.Alias, result.Error)
			if result.Hint != "" {
				msg += " (hint: " + result.Hint + ")"
			}
			warnings = append(warnings, msg)
		}
		// Update ports if settings changed them (e.g., router)
		if len(result.Ports) > 0 {
			aliasToPortList[n.Alias] = result.Ports
			// Update the nodesCreated entry
			for i, nc := range nodesCreated {
				if nc["alias"] == n.Alias {
					nodesCreated[i]["ports"] = result.Ports
					break
				}
			}
		}

		// Mark settings status in nodesCreated
		for i, nc := range nodesCreated {
			if nc["alias"] == n.Alias {
				nodesCreated[i]["settings_valid"] = result.Valid
				break
			}
		}
	}

	// Execute: create edges
	type createdEdge struct {
		From   string
		To     string
		EdgeID string
		Index  int // index in edges slice for config step
	}
	var edgesCreated []createdEdge
	var edgesOutput []map[string]interface{}

	for i, e := range edges {
		fromNodeID, fromOK := aliasToNodeID[e.FromAlias]
		toNodeID, toOK := aliasToNodeID[e.ToAlias]

		if !fromOK || !toOK {
			errors = append(errors, fmt.Sprintf("edge %s -> %s: skipped, source or target node was not created", e.From, e.To))
			continue
		}
		if ctx.Err() != nil {
			errors = append(errors, fmt.Sprintf("cancelled during edge creation (%s -> %s)", e.From, e.To))
			break
		}

		result, err := execCtx.EdgeAdder.AddEdge(ctx, execCtx.ProjectName, execCtx.FlowName, fromNodeID, e.FromPort, toNodeID, e.ToPort)
		if err != nil {
			errors = append(errors, fmt.Sprintf("edge %s -> %s: %s", e.From, e.To, err.Error()))
			continue
		}

		edgesCreated = append(edgesCreated, createdEdge{
			From:   e.From,
			To:     e.To,
			EdgeID: result.EdgeID,
			Index:  i,
		})
		edgesOutput = append(edgesOutput, map[string]interface{}{
			"from":    e.From,
			"to":      e.To,
			"edge_id": result.EdgeID,
		})
	}

	// Auto-scaffold scenarios BEFORE configuring edges, so the very first
	// validation pass already has placeholder shapes to chain-walk against
	// — scaffolding after validation would leave the first build's output
	// carrying warnings the scaffold has just fixed. See
	// build_flow_scenarios.go for what qualifies (shapeless source
	// fields), driven by the source port's schema rather than a component
	// whitelist.
	settingsByAlias := map[string]map[string]interface{}{}
	for _, n := range nodes {
		settingsByAlias[n.Alias] = n.Settings
	}
	scaffoldEdges := make([]scaffoldEdge, 0, len(edges))
	for _, e := range edges {
		scaffoldEdges = append(scaffoldEdges, scaffoldEdge{
			FromAlias:     e.FromAlias,
			FromPort:      e.FromPort,
			Configuration: e.Configuration,
			ToSettings:    settingsByAlias[e.ToAlias],
		})
	}
	if scaffoldWarnings := scaffoldScenarios(ctx, execCtx, aliasToNodeID, componentByAlias, scaffoldEdges); len(scaffoldWarnings) > 0 {
		warnings = append(warnings, scaffoldWarnings...)
	}

	// Execute: configure edges
	for j, ce := range edgesCreated {
		e := edges[ce.Index]
		if e.Configuration == nil {
			continue
		}
		if ctx.Err() != nil {
			errors = append(errors, fmt.Sprintf("cancelled during edge config (%s -> %s)", e.From, e.To))
			break
		}
		if execCtx.EdgeConfigurer == nil {
			warnings = append(warnings, fmt.Sprintf("edge %s -> %s: edge configurer not available, skipping configuration", e.From, e.To))
			continue
		}

		result, err := execCtx.EdgeConfigurer.ConfigureEdge(ctx, execCtx.ProjectName, execCtx.FlowName, ce.EdgeID, e.Configuration, e.Schema, "")
		if err != nil {
			edgesOutput[j]["config_valid"] = false
			msg := fmt.Sprintf("edge %s -> %s config: %s", e.From, e.To, err.Error())
			if note := unwireEdge(ctx, execCtx, ce.EdgeID); note != "" {
				msg += " (" + note + ")"
			} else {
				edgesOutput[j]["removed"] = true
			}
			errors = append(errors, msg)
			continue
		}

		edgesOutput[j]["config_valid"] = result.Valid
		if result.Valid && result.Hint != "" {
			// Surface advisories on valid edges (a field the target doesn't
			// declare, or an expression that can't be verified without a
			// scenario) BOTH on the edge and in the top-level warnings[]. On the
			// edge alone they sit next to config_valid:true and get read as
			// noise, so the flow ships with amber validation warnings the model
			// never acted on.
			edgesOutput[j]["hint"] = result.Hint
			warnings = append(warnings, fmt.Sprintf("edge %s -> %s: %s", e.From, e.To, result.Hint))
		}
		if !result.Valid {
			msg := fmt.Sprintf("edge %s -> %s validation: %s", e.From, e.To, result.Error)
			if result.Hint != "" {
				msg += " (hint: " + result.Hint + ")"
			}
			if note := unwireEdge(ctx, execCtx, ce.EdgeID); note != "" {
				msg += " (" + note + ")"
			} else {
				edgesOutput[j]["removed"] = true
				msg += " — edge removed; re-send it with a corrected configuration"
			}
			errors = append(errors, msg)
		}
	}

	// Build output
	output := map[string]interface{}{
		"nodes_created": nodesCreated,
		"edges_created": edgesOutput,
	}
	if len(errors) > 0 {
		output["errors"] = errors
	}
	if len(warnings) > 0 {
		output["warnings"] = warnings
	}

	success := len(nodesCreated) > 0
	if !success {
		return ToolResult{
			Success: false,
			Error:   fmt.Sprintf("no nodes were created. Errors: %s", strings.Join(errors, "; ")),
			Output:  output,
		}
	}

	// Hint about the next obvious step depending on whether the build was clean.
	switch {
	case len(errors) > 0:
		output["hint"] = "Some operations failed — use edit_flow to fix the specific issues listed in errors[]."
	case len(warnings) > 0:
		output["hint"] = fmt.Sprintf("Flow built, but %d warning(s) in warnings[] — each is an edge whose expression the validator can't verify. Resolve them before relying on the flow: add a `schema` hint to the edge when it navigates into a generic/any-typed value (e.g. $.context.foo), or a scenario (sample port data) when a typed field has no sample. Then send_signal to verify runtime.", len(warnings))
	default:
		output["hint"] = "Flow built. Find the trigger node in nodes_created and call send_signal(node_id) to verify behavior, then get_trace_detail(trace_id) to inspect the result."
	}

	return ToolResult{
		Success: true,
		Output:  output,
	}
}

// parseAliasPort splits "alias:port" into its parts
func parseAliasPort(s string) (alias, port string, ok bool) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

var _ Tool = (*BuildFlowTool)(nil)

// unwireEdge removes an edge whose configuration was rejected, returning a
// non-empty note when it could not be removed.
//
// ConfigureEdge deliberately refuses to persist a configuration that fails
// validation — but AddEdge has already wired the edge by then. Leaving it is
// the worst of both worlds: the edge carries NO mapping, so at runtime the
// target falls back to its own settings example and the flow serves confident,
// fabricated data behind a green build. (Observed live: a cluster-health
// endpoint that returned a hand-written sample pod instead of the real 14.)
//
// Removing the edge makes the flow visibly incomplete instead — a dangling
// port the author must fix — which is a failure you can see.
func unwireEdge(ctx context.Context, execCtx ExecutionContext, edgeID string) string {
	if execCtx.FlowModifier == nil {
		return "edge left wired WITHOUT configuration — no flow modifier available to remove it; delete it manually or it will run on target defaults"
	}
	ops := []FlowOperation{{Op: "delete", ID: edgeID, Element: map[string]interface{}{"type": "tinyEdge"}}}
	results, err := execCtx.FlowModifier.ApplyFlowChanges(ctx, execCtx.ProjectName, execCtx.FlowName, ops)
	if err != nil {
		return "edge left wired WITHOUT configuration — removal failed: " + err.Error()
	}
	if len(results) > 0 && !results[0].Success {
		return "edge left wired WITHOUT configuration — removal failed: " + results[0].Error
	}
	return ""
}
