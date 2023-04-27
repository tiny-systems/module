package utils

import "fmt"

func GetPortFullName(nodeID string, portName string) string {
	if nodeID == "" && portName == "" {
		return ""
	}
	return fmt.Sprintf("%s_%s", nodeID, portName)
}
