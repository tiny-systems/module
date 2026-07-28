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

// Which edges deserve scaffolding is decided from the SOURCE port's
// published schema, not a component whitelist: any output port with
// SHAPELESS fields — a $defs entry with no properties/items/typed shape
// (context passthrough, js_eval outputData, json_decode payloads, …) —
// has data the validator can't chain-walk without sample data. The
// `configurable:true` flag alone is not enough: output ports republish
// resolved schemas where the passthrough defs (e.g. pod_logs_get's
// Context) carry no flag at all, just no shape. A hardcoded component
// list rots the same way — new components silently fall outside it and
// their edges ship with amber "cannot verify without a scenario"
// warnings the model then has to resolve by hand. Fixed-typed fields on
// the same port are NOT scaffolded — scenarios only matter for variadic
// fields (see platform/docs/scenarios.md), and a string placeholder
// written into a typed field could contradict the real schema.

// shapelessFieldsIn returns the top-level field names of a port schema
// whose $defs entry the chain-walker has nothing to verify against:
// marked configurable, or carrying neither properties, items,
// additionalProperties, nor a scalar type. Field names come from each
// def's `path` ("$.context" → "context"); defs with deeper or missing
// paths are ignored — scaffolding writes top-level port data.
func shapelessFieldsIn(schemaBytes []byte) []string {
	if len(schemaBytes) == 0 {
		return nil
	}
	var root map[string]interface{}
	if err := json.Unmarshal(schemaBytes, &root); err != nil {
		return nil
	}
	defs, ok := root["$defs"].(map[string]interface{})
	if !ok {
		return nil
	}
	var out []string
	for _, raw := range defs {
		def, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		path, _ := def["path"].(string)
		if !strings.HasPrefix(path, "$.") {
			continue
		}
		field := strings.TrimPrefix(path, "$.")
		if strings.ContainsAny(field, ".[") {
			continue // nested def, not a top-level port field
		}
		if configurable, _ := def["configurable"].(bool); configurable {
			out = append(out, field)
			continue
		}
		if def["properties"] != nil || def["items"] != nil || def["additionalProperties"] != nil {
			continue // has shape — the walker can verify it
		}
		if t, _ := def["type"].(string); t != "" && t != "object" {
			continue // scalar-typed — verifiable
		}
		out = append(out, field)
	}
	return out
}

// expressionRe matches `{{...}}` expressions in edge configuration values.
var expressionRe = regexp.MustCompile(`\{\{([^}]+)\}\}`)

// jsonPathRe matches a JSONPath rooted at `$`, capturing dotted-or-
// bracketed segments after the root (e.g. `$.decoded.imageTag` →
// `decoded.imageTag`, `$.decoded.items[0].name` →
// `decoded.items[0].name`). Bracket indices are captured so callers
// can decide whether a path's intermediate node should be scaffolded
// as an object or an array.
var jsonPathRe = regexp.MustCompile(`\$(?:\.[a-zA-Z_][a-zA-Z0-9_]*(?:\[\d+\]|\[\*\])?)+`)

// scaffoldScenarios runs at the end of build_flow with the created node
// IDs and the original edge specs. Returns warnings (not errors) — the
// build is successful regardless of scaffolding outcome.
func scaffoldScenarios(
	ctx context.Context,
	execCtx ExecutionContext,
	createdNodes map[string]string, // alias → nodeID
	componentByAlias map[string]*ComponentInfo, // alias → full component details
	edges []scaffoldEdge,
) []string {
	if execCtx.ScenarioManager == nil || execCtx.ProjectName == "" {
		return nil
	}

	// portData: source port full name → merged mock object
	portData := map[string]map[string]interface{}{}

	for _, e := range edges {
		// Schema-driven gate: scaffold only when the source output port has
		// shapeless fields AND the edge actually navigates into one — a port
		// whose referenced paths are all fixed-typed is fully verifiable
		// from its schema, and writing a sample would only switch the
		// validator into scenario-resolution mode for nothing.
		schemaBytes := portSchemaBytes(componentByAlias[e.FromAlias], e.FromPort, false)
		shapeless := shapelessFieldsIn(schemaBytes)
		if len(shapeless) == 0 {
			continue
		}
		paths := extractPathsFromConfig(e.Configuration)
		if len(pathsUnderFields(paths, shapeless)) == 0 {
			continue
		}
		nodeID, ok := createdNodes[e.FromAlias]
		if !ok || nodeID == "" {
			continue
		}
		portFullName := nodeID + ":" + e.FromPort
		mock := portData[portFullName]
		if mock == nil {
			mock = map[string]interface{}{}
		}
		// Once a scenario sample exists for a port, the validator resolves
		// EVERY expression against it — so the sample must cover all
		// referenced paths, not just the shapeless ones. Fixed-typed paths
		// get a placeholder of the source schema's own type; a fixed path
		// the schema doesn't declare is skipped — it should keep failing
		// validation, that's a genuine config error.
		//
		// Shapeless paths have no source type at all, but the EDGE knows
		// where each lands: when a config leaf is exactly one expression,
		// the target node's settings example at that config path tells us
		// the expected shape ($.context.messages mapped into
		// inputData.messages, whose example is an array → sample []).
		// Falling back to a "<leaf>" string marker on a target that
		// expects an array is what made the strict validator hard-reject
		// scaffolded edges.
		shapelessPaths := map[string]struct{}{}
		for _, p := range pathsUnderFields(paths, shapeless) {
			shapelessPaths[p] = struct{}{}
		}
		targetTypes := targetExampleTypes(e.Configuration, e.ToSettings)
		for _, p := range paths {
			if _, isShapeless := shapelessPaths[p]; isShapeless {
				setPath(mock, p, shapelessPlaceholder(p, targetTypes[p]))
				continue
			}
			if v, ok := typedPlaceholder(schemaBytes, p); ok {
				setPath(mock, p, v)
			}
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
	// ToSettings is the target node's settings from the build spec —
	// its examples (e.g. a js_eval inputData) type the shapeless
	// placeholders that get mapped into them.
	ToSettings map[string]interface{}
}

// targetExampleTypes maps each single-expression source path in the edge
// configuration to the target's example value at the config position it's
// mapped into. `{"inputData":{"messages":"{{$.context.messages}}"}}` with
// target settings `{"inputData":{"messages":[...]}}` yields
// "context.messages" → [...]. Only whole-string expressions count — a
// concatenation produces a string at runtime regardless of operand types.
func targetExampleTypes(config map[string]interface{}, toSettings map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	if toSettings == nil {
		return out
	}
	var walk func(cfg, example interface{})
	walk = func(cfg, example interface{}) {
		switch c := cfg.(type) {
		case map[string]interface{}:
			ex, _ := example.(map[string]interface{})
			for k, v := range c {
				if ex == nil {
					walk(v, nil)
					continue
				}
				walk(v, ex[k])
			}
		case []interface{}:
			exArr, _ := example.([]interface{})
			for i, v := range c {
				if len(exArr) > 0 {
					walk(v, exArr[0])
				} else {
					walk(v, nil)
				}
				_ = i
			}
		case string:
			if example == nil {
				return
			}
			m := expressionRe.FindStringSubmatch(c)
			// Whole-string single expression only.
			if m == nil || m[0] != c {
				return
			}
			for _, full := range jsonPathRe.FindAllString(m[1], -1) {
				path := strings.TrimPrefix(full, "$.")
				if path != "" && path != full {
					out[path] = example
				}
			}
		}
	}
	walk(config, toSettings)
	return out
}

// shapelessPlaceholder picks a sample value for a shapeless source path:
// shaped like the target example it's mapped into when one is known,
// otherwise the "<leaf>" string marker.
func shapelessPlaceholder(path string, targetExample interface{}) interface{} {
	switch targetExample.(type) {
	case []interface{}:
		return []interface{}{}
	case map[string]interface{}:
		return map[string]interface{}{}
	case float64, int, int64:
		return 0
	case bool:
		return false
	default:
		return placeholderFor(path)
	}
}

// typedPlaceholder resolves a dotted path against the port schema and returns
// a placeholder of the declared type — "<leaf>" for strings, 0 for numbers,
// false for booleans, empty containers for arrays/objects — so a scaffolded
// sample can never contradict a fixed field's schema. Returns ok=false when
// the schema doesn't declare the path (the caller should NOT fabricate data
// for it — an undeclared path is a real config error worth failing on).
func typedPlaceholder(schemaBytes []byte, path string) (interface{}, bool) {
	if len(schemaBytes) == 0 {
		return nil, false
	}
	var root map[string]interface{}
	if err := json.Unmarshal(schemaBytes, &root); err != nil {
		return nil, false
	}
	defs, _ := root["$defs"].(map[string]interface{})
	// Find the root def (path "$"), falling back to the top-level $ref.
	var cur map[string]interface{}
	for _, raw := range defs {
		if def, ok := raw.(map[string]interface{}); ok {
			if p, _ := def["path"].(string); p == "$" {
				cur = def
				break
			}
		}
	}
	if cur == nil {
		cur = deref(root, defs)
	}
	if cur == nil {
		return nil, false
	}
	parts := strings.Split(path, ".")
	for i, raw := range parts {
		m := segmentRe.FindStringSubmatch(raw)
		if m == nil {
			return nil, false
		}
		key, isArray := m[1], m[2] != ""
		props, _ := cur["properties"].(map[string]interface{})
		next, ok := props[key].(map[string]interface{})
		if !ok {
			return nil, false
		}
		next = deref(next, defs)
		if next == nil {
			return nil, false
		}
		if isArray {
			items, ok := next["items"].(map[string]interface{})
			if !ok {
				return nil, false
			}
			next = deref(items, defs)
			if next == nil {
				return nil, false
			}
		}
		if i == len(parts)-1 {
			leaf := key
			switch t, _ := next["type"].(string); t {
			case "string":
				return fmt.Sprintf("<%s>", leaf), true
			case "integer", "number":
				return 0, true
			case "boolean":
				return false, true
			case "array":
				return []interface{}{}, true
			case "object":
				return map[string]interface{}{}, true
			default:
				return fmt.Sprintf("<%s>", leaf), true
			}
		}
		cur = next
	}
	return nil, false
}

// deref follows a `$ref: "#/$defs/X"` indirection one level, returning the
// node itself when it carries no ref. Returns nil for a dangling ref.
func deref(node map[string]interface{}, defs map[string]interface{}) map[string]interface{} {
	ref, _ := node["$ref"].(string)
	if ref == "" {
		return node
	}
	name := strings.TrimPrefix(ref, "#/$defs/")
	if name == ref || defs == nil {
		return nil
	}
	target, _ := defs[name].(map[string]interface{})
	return target
}

// pathsUnderFields keeps only the paths whose first segment is one of the
// given (configurable) fields — `context.who` passes when "context" is
// configurable, `logs` doesn't when it's a fixed typed field. Array suffixes
// on the first segment (`items[0].name`) are stripped before matching.
func pathsUnderFields(paths []string, fields []string) []string {
	allowed := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		allowed[f] = struct{}{}
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		first := p
		if i := strings.IndexByte(first, '.'); i >= 0 {
			first = first[:i]
		}
		if m := segmentRe.FindStringSubmatch(first); m != nil {
			first = m[1]
		}
		if _, ok := allowed[first]; ok {
			out = append(out, p)
		}
	}
	return out
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
			for _, full := range jsonPathRe.FindAllString(expr, -1) {
				// Strip the leading `$.` so the path is a list of
				// segments like "decoded.items[0].name".
				path := strings.TrimPrefix(full, "$.")
				if path == "" || path == full {
					continue
				}
				seen[path] = struct{}{}
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

// segmentRe splits a path segment into its key and optional array
// suffix: "items[0]" → key="items", isArray=true; "items" → isArray=false.
var segmentRe = regexp.MustCompile(`^([a-zA-Z_][a-zA-Z0-9_]*)(\[\d+\]|\[\*\])?$`)

// setPath walks dst by the dotted path, creating intermediate object
// (or array-of-one-object) nodes as needed, and writes value at the
// leaf. If the leaf already has a non-nil value it is left alone —
// first writer wins so an earlier scaffolded path doesn't get
// clobbered by a later one.
//
// A segment ending in `[N]` or `[*]` means the value at that key is an
// array; the remainder of the path applies to element 0 of that array.
// So `decoded.items[0].name` produces `{decoded:{items:[{name:<v>}]}}`.
func setPath(dst map[string]interface{}, path string, value interface{}) {
	parts := strings.Split(path, ".")
	var cur interface{} = dst
	for i, raw := range parts {
		m := segmentRe.FindStringSubmatch(raw)
		if m == nil {
			return
		}
		key := m[1]
		isArray := m[2] != ""
		last := i == len(parts)-1

		obj, ok := cur.(map[string]interface{})
		if !ok {
			return
		}
		if !isArray {
			if last {
				if existing, present := obj[key]; !present || existing == nil {
					obj[key] = value
				}
				return
			}
			next, ok := obj[key].(map[string]interface{})
			if !ok {
				next = map[string]interface{}{}
				obj[key] = next
			}
			cur = next
			continue
		}

		// Array intermediate. Ensure obj[key] is an array with at least
		// one element. The remainder of the path writes into element 0.
		arr, ok := obj[key].([]interface{})
		if !ok || len(arr) == 0 {
			elem := map[string]interface{}{}
			obj[key] = []interface{}{elem}
			arr = obj[key].([]interface{})
		}
		if last {
			// Path ends at the array itself — leave the single element
			// in place; the placeholder string here would be confusing.
			return
		}
		elem, ok := arr[0].(map[string]interface{})
		if !ok {
			elem = map[string]interface{}{}
			arr[0] = elem
		}
		cur = elem
	}
}

// placeholderFor returns a placeholder string with a short hint about
// the original path, so the user can recognise it inside the scenario
// editor when they go to provide real sample data.
func placeholderFor(path string) string {
	parts := strings.Split(path, ".")
	leaf := parts[len(parts)-1]
	if m := segmentRe.FindStringSubmatch(leaf); m != nil {
		leaf = m[1]
	}
	return fmt.Sprintf("<%s>", leaf)
}
