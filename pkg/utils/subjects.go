package utils

import (
	"fmt"
)

const StreamName = "tinycloud"

func GetInstanceInputSubject(workspaceID, flowID string, ID string, port string) string {
	return fmt.Sprintf("%s.%s.flow.%s.instance.%s.%s", StreamName, workspaceID, flowID, ID, port)
}
func CreateComponentSubject(workspaceID, componentName string) string {
	return fmt.Sprintf("%s.%s.component.%s", StreamName, workspaceID, componentName)
}

func GetNodesLookupSubject(flowID string) string {
	return fmt.Sprintf("%s.discovery.nodes.flow%s", StreamName, flowID)
}

func GetComponentLookupSubject(workspaceID string) string {
	return fmt.Sprintf("%s.discovery.components.workspace%s", StreamName, workspaceID)
}
func GetFlowLookupSubject(workspaceID string) string {
	return fmt.Sprintf("%s.discovery.flows.workspace%s", StreamName, workspaceID)
}
func GetModuleLookupSubject(workspaceID string) string {
	return fmt.Sprintf("%s.discovery.modules.workspace%s", StreamName, workspaceID)
}
func GetServerLookupSubject(workspaceID string) string {
	return fmt.Sprintf("%s.discovery.servers.workspace%s", StreamName, workspaceID)
}
