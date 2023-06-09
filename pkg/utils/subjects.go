package utils

import (
	"fmt"
	"github.com/google/uuid"
)

const StreamName = "tiny"

func GetInstanceInputSubject(workspaceID, flowID string, ID string, port string) string {
	return fmt.Sprintf("%s.%s.flow.%s.input.%s.%s", StreamName, workspaceID, flowID, ID, port)
}
func GetInstanceControlSubject(workspaceID, flowID string, ID string, port string) string {
	return fmt.Sprintf("%s.%s.flow.%s.control.%s.%s", StreamName, workspaceID, flowID, ID, port)
}

func CreateComponentSubject(workspaceID, componentName string) string {
	return fmt.Sprintf("%s.%s.component.%s", StreamName, workspaceID, componentName)
}
func GetNodesLookupSubject(flowID string) string {
	return fmt.Sprintf("%s.discovery.nodes.flow%s", StreamName, flowID)
}
func GetNodeStatsLookupSubject(flowID string) string {
	return fmt.Sprintf("%s.discovery.nodestat.flow%s", StreamName, flowID)
}
func GetComponentLookupSubject(workspaceID string) string {
	return fmt.Sprintf("%s.discovery.components.workspace%s", StreamName, workspaceID)
}
func GetFlowLookupSubject(workspaceID string) string {
	return fmt.Sprintf("%s.discovery.flows.workspace%s", StreamName, workspaceID)
}
func GetServerLookupSubject(workspaceID string) string {
	return fmt.Sprintf("%s.discovery.servers.workspace%s", StreamName, workspaceID)
}
func GetCallbackSubject() (string, error) {
	u, err := uuid.NewUUID()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s.discovery.callback.%s", StreamName, u.String()), nil
}
