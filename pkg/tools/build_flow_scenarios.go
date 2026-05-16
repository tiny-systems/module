package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// scaffoldScenarios is the auto-scaffold step at the end of build_flow:
// for every node whose output port emits configurable-any data
// (json_decode, js_eval, http_request response, etc.), it collects the
// JSON paths referenced by downstream edge configurations and writes a
// TinyScenario with placeholder values at those paths. Edge validation
// then has a real shape to chain-walk against, so the configurable-any
// gap doesn't produce amber warnings on flows the author has already
// fully specified.
//
// The scenario is named "auto-scaffold" and is shared across the
// project — subsequent build_flow calls upsert ports into the same
// scenario instead of creating new ones. Users can delete it, override
// it, or replace it with a trace-derived scenario any time.
//
// All scenario operations are best-effort. Failures append to warnings
// and never block the build.

// configurableAnyEmitters maps component name → output port name for
// components whose output schema is configurable-any. The list is short
// and well-known; adding a new emitter is a one-line change.
var configurableAnyEmitters = map[string][]string{
	"json_decode":  {"message"},
	"js_eval":      {"out"},
	"client":       {"response"},
	"http_request": {"response"},
	"template":     {"out"},
}

// expressionRe matches `{{...}}` expressions in edge configuration values.
var expressionRe = regexp.MustCompile(`\{\{([^}]+)\}\}`)

// jsonPathRe matches a JSONPath rooted at `$`, capturing dotted segments
// after the root (e.g. `$.decoded.imageTag` → `decoded.imageTag`).
// Bracket access is intentionally skipped — placeholder data uses object
// access exclusively, which covers ~all real expressions.
var jsonPathRe = regexp.MustCompile(`\$(?:\.([a-zA-Z_][a-zA-Z0-9_]*(?:\.[a-zA-Z_][a-zA-Z0-9_]*)*))`)

// scaffoldScenarios runs at the end of build_flow with the created node
// IDs and the original edge specs. Returns warnings (not errors) — the
// build is successful regardless of scaffolding outcome.
func scaffoldScenarios(
	ctx context.Context,
	execCtx ExecutionContext,
	createdNodes map[string]string, // alias → nodeID
	emitterByAlias map[string]string, // alias → component name
	edges []scaffoldEdge,
) []string {
	if execCtx.ScenarioManager == nil || execCtx.ProjectName == "" {
		return nil
	}

	// portData: source port full name → merged mock object
	portData := map[string]map[string]interface{}{}

	for _, e := range edges {
		outPorts, isEmitter := configurableAnyEmitters[emitterByAlias[e.FromAlias]]
		if !isEmitter {
			continue
		}
		// Only scaffold for the canonical output ports of the emitter
		matched := false
		for _, p := range outPorts {
			if p == e.FromPort {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		nodeID, ok := createdNodes[e.FromAlias]
		if !ok || nodeID == "" {
			continue
		}
		paths := extractPathsFromConfig(e.Configuration)
		if len(paths) == 0 {
			continue
		}
		portFullName := nodeID + ":" + e.FromPort
		mock := portData[portFullName]
		if mock == nil {
			mock = map[string]interface{}{}
		}
		for _, p := range paths {
			setPath(mock, p, placeholderFor(p))
		}
		portData[portFullName] = mock
	}

	if len(portData) == 0 {
		return nil
	}

	var warnings []string

	// Find or create the auto-scaffold scenario for this project. We
	// avoid duplicates: list first, reuse if present.
	scenarioName := "auto-scaffold"
	scenarios, listErr := execCtx.ScenarioManager.ListScenarios(ctx, execCtx.ProjectName)
	if listErr != nil {
		warnings = append(warnings, fmt.Sprintf("scaffold: list scenarios failed (%s) — skipping auto-scaffold", listErr.Error()))
		return warnings
	}
	var scenarioResource string
	for _, sc := range scenarios {
		if sc.Name == scenarioName {
			scenarioResource = sc.ResourceName
			break
		}
	}
	if scenarioResource == "" {
		created, err := execCtx.ScenarioManager.CreateEmptyScenario(ctx, execCtx.ProjectName, scenarioName)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("scaffold: create scenario failed (%s)", err.Error()))
			return warnings
		}
		scenarioResource = created.ResourceName
	}

	for port, mock := range portData {
		data, err := json.Marshal(mock)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("scaffold: marshal %s failed (%s)", port, err.Error()))
			continue
		}
		if err := execCtx.ScenarioManager.UpdateScenarioPort(ctx, execCtx.ProjectName, scenarioResource, port, data); err != nil {
			warnings = append(warnings, fmt.Sprintf("scaffold: write %s failed (%s)", port, err.Error()))
		}
	}
	return warnings
}

// scaffoldEdge is the minimum edge data scaffoldScenarios needs.
type scaffoldEdge struct {
	FromAlias     string
	FromPort      string
	Configuration map[string]interface{}
}

// extractPathsFromConfig walks any value (map / slice / string) and
// returns every distinct `$.<dotted.path>` referenced inside `{{...}}`
// expressions, with the leading `$.` stripped.
func extractPathsFromConfig(v interface{}) []string {
	seen := map[string]struct{}{}
	collectExpressions(v, seen)
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	return out
}

func collectExpressions(v interface{}, seen map[string]struct{}) {
	switch x := v.(type) {
	case string:
		matches := expressionRe.FindAllStringSubmatch(x, -1)
		for _, m := range matches {
			expr := m[1]
			for _, pm := range jsonPathRe.FindAllStringSubmatch(expr, -1) {
				path := strings.TrimSpace(pm[1])
				if path != "" {
					seen[path] = struct{}{}
				}
			}
		}
	case map[string]interface{}:
		for _, val := range x {
			collectExpressions(val, seen)
		}
	case []interface{}:
		for _, val := range x {
			collectExpressions(val, seen)
		}
	}
}

// setPath walks dst by the dotted path, creating intermediate object
// nodes as needed, and writes value at the leaf. If the leaf already
// has a non-nil value it is left alone — first writer wins so an
// earlier scaffolded path doesn't get clobbered by a later one.
func setPath(dst map[string]interface{}, path string, value interface{}) {
	parts := strings.Split(path, ".")
	cur := dst
	for i, p := range parts {
		if i == len(parts)-1 {
			if existing, ok := cur[p]; !ok || existing == nil {
				cur[p] = value
			}
			return
		}
		next, ok := cur[p].(map[string]interface{})
		if !ok {
			next = map[string]interface{}{}
			cur[p] = next
		}
		cur = next
	}
}

// placeholderFor returns a placeholder string with a short hint about
// the original path, so the user can recognise it inside the scenario
// editor when they go to provide real sample data.
func placeholderFor(path string) string {
	parts := strings.Split(path, ".")
	leaf := parts[len(parts)-1]
	return fmt.Sprintf("<%s>", leaf)
}
