package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/tiny-systems/module/api/v1alpha1"
	moduleutils "github.com/tiny-systems/module/pkg/utils"
)

// ScaffoldLiveScenarios tops up the auto-scaffold scenario from the project
// as it exists in the cluster RIGHT NOW, rather than from the edge specs one
// build_flow call happened to see.
//
// build_flow's scaffold runs against the edges of that single build, using
// each component's static port catalogue. Ports that appear only after a
// component is configured — llm_tools publishes one out_<tool> port per
// declared tool, router one per route — do not exist yet at that moment, so
// their edges ship unscaffolded and validate amber forever: "cannot be
// verified without a scenario". The same gap hits any flow edited after its
// build, or drawn by hand in the editor.
//
// Reading the live project closes it: node handles carry the published
// schemas the runtime actually emits, and edge elements carry the
// configurations authors actually wrote.
//
// Returns the number of ports written plus warnings. Best-effort throughout:
// scaffolding never blocks its caller.
func ScaffoldLiveScenarios(ctx context.Context, execCtx ExecutionContext) (int, []string) {
	if execCtx.ScenarioManager == nil || execCtx.TinyNodeCRManager == nil || execCtx.ProjectName == "" {
		return 0, []string{"scaffold: node reader or scenario manager not configured"}
	}

	// Straight from the cluster, deliberately: the agent-facing project
	// elements carry a compact port list with no schemas, and the schema is
	// the whole input to the shapeless-field decision.
	nodeList, err := execCtx.TinyNodeCRManager.ListProjectNodeCRs(ctx, execCtx.ProjectName)
	if err != nil {
		return 0, []string{fmt.Sprintf("scaffold: list nodes failed (%v)", err)}
	}
	nodes := make(map[string]*v1alpha1.TinyNode, len(nodeList))
	for i := range nodeList {
		nodes[nodeList[i].Name] = &nodeList[i]
	}
	if len(nodes) == 0 {
		return 0, nil
	}

	// Per-port schema and per-node settings example, straight from what the
	// runtime published.
	schemaByPort := map[string][]byte{}
	settingsByNode := map[string]map[string]interface{}{}
	for name, node := range nodes {
		for _, ps := range node.Status.Ports {
			if len(ps.Schema) > 0 {
				schemaByPort[name+":"+ps.Name] = ps.Schema
			}
			if ps.Name == v1alpha1.SettingsPort && len(ps.Configuration) > 0 {
				var settings map[string]interface{}
				if json.Unmarshal(ps.Configuration, &settings) == nil {
					settingsByNode[name] = settings
				}
			}
		}
	}

	portData := map[string]map[string]interface{}{}
	for sourceName, sourceNode := range nodes {
		for _, edge := range sourceNode.Spec.Edges {
			sourcePort := sourceName + ":" + edge.Port
			shapeless := shapelessFieldsIn(schemaByPort[sourcePort])
			if len(shapeless) == 0 {
				continue
			}
			targetName, targetPort := moduleutils.ParseFullPortName(edge.To)
			targetNode, ok := nodes[targetName]
			if !ok {
				continue
			}
			var config map[string]interface{}
			for _, pc := range targetNode.Spec.Ports {
				if pc.From == sourcePort && pc.Port == targetPort && len(pc.Configuration) > 0 {
					_ = json.Unmarshal(pc.Configuration, &config)
					break
				}
			}
			if len(config) == 0 {
				continue
			}
			paths := extractPathsFromConfig(config)
			if len(pathsUnderFields(paths, shapeless)) == 0 {
				continue
			}

			mock := portData[sourcePort]
			if mock == nil {
				mock = map[string]interface{}{}
			}
			shapelessPaths := map[string]struct{}{}
			for _, p := range pathsUnderFields(paths, shapeless) {
				shapelessPaths[p] = struct{}{}
			}
			targetTypes := targetExampleTypes(config, settingsByNode[targetName])
			constrained := targetConstrainedValues(config, schemaByPort[edge.To])
			for _, p := range paths {
				if v, ok := constrained[p]; ok {
					setPath(mock, p, v)
					continue
				}
				if _, isShapeless := shapelessPaths[p]; isShapeless {
					setPath(mock, p, shapelessPlaceholder(p, targetTypes[p]))
					continue
				}
				if v, ok := typedPlaceholder(schemaByPort[sourcePort], p); ok {
					setPath(mock, p, v)
				}
			}
			portData[sourcePort] = mock
		}
	}
	if len(portData) == 0 {
		return 0, nil
	}

	var warnings []string
	scenarios, listErr := execCtx.ScenarioManager.ListScenarios(ctx, execCtx.ProjectName)
	if listErr != nil {
		return 0, []string{fmt.Sprintf("scaffold: list scenarios failed (%s)", listErr.Error())}
	}
	var scenarioResource string
	for _, sc := range scenarios {
		if sc.Name == ScaffoldScenarioName {
			scenarioResource = sc.ResourceName
			break
		}
	}
	if scenarioResource == "" {
		created, cerr := execCtx.ScenarioManager.CreateEmptyScenario(ctx, execCtx.ProjectName, ScaffoldScenarioName)
		if cerr != nil {
			return 0, []string{fmt.Sprintf("scaffold: create scenario failed (%s)", cerr.Error())}
		}
		scenarioResource = created.ResourceName
	}

	ports := make([]string, 0, len(portData))
	for p := range portData {
		ports = append(ports, p)
	}
	sort.Strings(ports)

	written := 0
	for _, port := range ports {
		data, merr := json.Marshal(portData[port])
		if merr != nil {
			warnings = append(warnings, fmt.Sprintf("scaffold: marshal %s failed (%s)", port, merr.Error()))
			continue
		}
		if uerr := execCtx.ScenarioManager.UpdateScenarioPort(ctx, execCtx.ProjectName, scenarioResource, port, data); uerr != nil {
			warnings = append(warnings, fmt.Sprintf("scaffold: write %s failed (%s)", port, uerr.Error()))
			continue
		}
		written++
	}
	return written, warnings
}

// targetConstrainedValues maps a source JSONPath to a value the TARGET port
// schema will actually accept, for the constraints a generic placeholder
// cannot satisfy by luck: enums, const, and non-string scalar types. Without it a scaffolded sample
// writes "<kind>" into a field declared as one of
// Deployment/StatefulSet/DaemonSet and the edge fails validation — the
// scaffold's own placeholder becoming the error.
//
// Only whole-string single expressions are resolvable: "{{$.context.kind}}"
// tells us $.context.kind lands in `kind`, while "ns-{{$.x}}" does not
// identify a target field.
func targetConstrainedValues(config map[string]interface{}, targetSchema []byte) map[string]interface{} {
	out := map[string]interface{}{}
	if len(targetSchema) == 0 {
		return out
	}
	var root map[string]interface{}
	if err := json.Unmarshal(targetSchema, &root); err != nil {
		return out
	}

	var walk func(cfg interface{}, schema map[string]interface{})
	walk = func(cfg interface{}, schema map[string]interface{}) {
		schema = resolveSchemaNode(root, schema)
		switch c := cfg.(type) {
		case map[string]interface{}:
			props, _ := schema["properties"].(map[string]interface{})
			for k, v := range c {
				var child map[string]interface{}
				if props != nil {
					child, _ = props[k].(map[string]interface{})
				}
				walk(v, child)
			}
		case []interface{}:
			items, _ := schema["items"].(map[string]interface{})
			for _, v := range c {
				walk(v, items)
			}
		case string:
			if schema == nil {
				return
			}
			var value interface{}
			switch {
			case len(asSlice(schema["enum"])) > 0:
				value = asSlice(schema["enum"])[0]
			case schema["const"] != nil:
				value = schema["const"]
			default:
				// A non-string scalar target rejects the "<leaf>" marker
				// outright ("/lines: expected integer, but got string"), so
				// the declared type picks the value. Strings keep the marker:
				// it is valid there and says where it came from.
				switch t, _ := schema["type"].(string); t {
				case "integer", "number":
					value = 0
				case "boolean":
					value = false
				default:
					return
				}
			}
			m := expressionRe.FindStringSubmatch(c)
			if m == nil || m[0] != c {
				return
			}
			for _, full := range jsonPathRe.FindAllString(m[1], -1) {
				path := strings.TrimPrefix(full, "$.")
				if path != "" && path != full {
					out[path] = value
				}
			}
		}
	}
	walk(config, root)
	return out
}

func asSlice(v interface{}) []interface{} {
	s, _ := v.([]interface{})
	return s
}

// resolveSchemaNode follows a single $ref into the document's $defs so
// property walking survives the ref indirection component schemas use.
func resolveSchemaNode(root, node map[string]interface{}) map[string]interface{} {
	if node == nil {
		return nil
	}
	ref, _ := node["$ref"].(string)
	const prefix = "#/$defs/"
	if !strings.HasPrefix(ref, prefix) {
		return node
	}
	defs, _ := root["$defs"].(map[string]interface{})
	if defs == nil {
		return node
	}
	target, _ := defs[strings.TrimPrefix(ref, prefix)].(map[string]interface{})
	if target == nil {
		return node
	}
	return target
}
