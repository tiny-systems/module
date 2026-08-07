package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

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
			for _, p := range paths {
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
