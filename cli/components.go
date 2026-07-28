package cli

import (
	"encoding/json"

	"github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/schema"
	"github.com/tiny-systems/module/registry"
	api "github.com/tiny-systems/platform-api"
)

// AgentToolTag is the tag the platform's MCP layer looks for to
// surface a component as an MCP tool. Auto-added when a component
// implements the module.AgentTool capability interface; module
// authors don't need to set it manually.
const AgentToolTag = "agent_tool"

// SyncRPCTag marks a component that blocks on a synchronous response
// (module.SyncRPC capability). The platform keeps the connected
// subgraph around such components on classic request/reply delivery —
// durable fire-and-forget hops would never return the result the
// component is waiting on. Auto-added; authors implement the
// interface, never set the tag by hand.
const SyncRPCTag = "sync_rpc"

// collectComponentsApi introspects the registered components into the
// api.PublishComponent shape — the same shape a consumer (the platform,
// the conformance check) sees. Pure introspection, no docker/publish.
func collectComponentsApi() []api.PublishComponent {
	out := make([]api.PublishComponent, 0)
	for _, c := range registry.Get() {
		out = append(out, getComponentApi(c))
	}
	return out
}

// filterNullValues recursively removes null values from a map
func filterNullValues(m map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range m {
		if v == nil {
			continue
		}
		if nested, ok := v.(map[string]interface{}); ok {
			filtered := filterNullValues(nested)
			if len(filtered) > 0 {
				result[k] = filtered
			}
		} else {
			result[k] = v
		}
	}
	return result
}

func getComponentApi(c module.Component) api.PublishComponent {
	componentInfo := c.GetInfo()

	// Opt-in MCP tool exposure: if the component implements
	// AgentTool, mark it for the platform-side registry. The tag
	// is the wire-level signal; the AgentToolInfo fields stay
	// runtime-side (the platform reads the default input port's
	// schema for tool input validation).
	if agentTool, ok := c.(module.AgentTool); ok {
		info := agentTool.AgentTool()
		hasTag := false
		for _, t := range componentInfo.Tags {
			if t == AgentToolTag {
				hasTag = true
				break
			}
		}
		if !hasTag {
			componentInfo.Tags = append(componentInfo.Tags, AgentToolTag)
		}
		if info.Description != "" {
			componentInfo.Info = info.Description
		}
	}

	// Synchronous-response declaration: the component tells the world
	// it blocks waiting for a downstream result, and the platform
	// derives delivery modes from that — the component's ONE line of
	// transport policy, replacing flow-level mode configuration.
	if _, ok := c.(module.SyncRPC); ok {
		hasTag := false
		for _, t := range componentInfo.Tags {
			if t == SyncRPCTag {
				hasTag = true
				break
			}
		}
		if !hasTag {
			componentInfo.Tags = append(componentInfo.Tags, SyncRPCTag)
		}
	}

	// Build ports with schemas
	ports := make([]api.PublishComponentPort, 0)
	for _, p := range c.Ports() {
		port := api.PublishComponentPort{
			Name:   p.Name,
			Source: p.Source,
		}
		if p.Label != "" {
			port.Label = &p.Label
		}
		pos := int(p.Position)
		port.Position = &pos

		// Generate schema and default data from Configuration
		if p.Configuration != nil {
			s, err := schema.CreateSchema(p.Configuration)
			if err == nil {
				schemaBytes, err := s.MarshalJSON()
				if err == nil {
					var schemaMap map[string]interface{}
					if json.Unmarshal(schemaBytes, &schemaMap) == nil {
						port.Schema = &schemaMap
					}
				}
			}
			// Include default data (the Configuration itself)
			configBytes, err := json.Marshal(p.Configuration)
			if err == nil {
				var defaultData map[string]interface{}
				if json.Unmarshal(configBytes, &defaultData) == nil {
					// Filter out null values as OpenAPI schema doesn't allow them
					filtered := filterNullValues(defaultData)
					if len(filtered) > 0 {
						port.DefaultData = &filtered
					}
				}
			}
		}
		ports = append(ports, port)
	}

	return api.PublishComponent{
		Name:        componentInfo.Name,
		Description: componentInfo.Description,
		Info:        &componentInfo.Info,
		Tags:        &componentInfo.Tags,
		Ports:       &ports,
	}
}
