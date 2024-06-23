package utils

import (
	"fmt"
	"strings"
)

func GetPortFullName(nodeID string, portName string) string {
	if nodeID == "" && portName == "" {
		return ""
	}
	return fmt.Sprintf("%s:%s", nodeID, portName)
}

func ParseFullPortName(name string) (node string, port string) {
	parts := strings.Split(name, ":")
	if len(parts) > 2 {
		return
	}

	if len(parts) > 1 {
		port = parts[1]
		node = parts[0]
	} else if len(parts) > 0 {
		port = parts[0]
	}
	return
}
