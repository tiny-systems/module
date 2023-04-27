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
