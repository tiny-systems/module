package utils

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"strings"
)

const nameSeparator = "."

// GetFlowHashPrefix returns the 8-character MD5 hash prefix for a flow.
// This is used to generate consistent node names within a flow.
func GetFlowHashPrefix(projectName, flowName string) string {
	return getFirstNChars(getMD5Hash(projectName+flowName), 8)
}

// GetNodeFullName creates the full node name with prefix, module, and component.
// Format: {prefix}.{module}.{component}
func GetNodeFullName(prefix, moduleName, componentName string) string {
	return fmt.Sprintf("%s%s%s%s%s", prefix, nameSeparator, moduleName, nameSeparator, componentName)
}

// GetNodeGenerateName returns the Kubernetes GenerateName for a new node.
// Format: {8charHash}.{module}.{component}-
func GetNodeGenerateName(projectName, flowName, moduleName, componentName string) string {
	hashPrefix := GetFlowHashPrefix(projectName, flowName)
	return fmt.Sprintf("%s-", GetNodeFullName(hashPrefix, SanitizeResourceName(moduleName), SanitizeResourceName(componentName)))
}

// ParseNodeFullName parses a full node name into its components.
// Returns module and component, or error if invalid.
func ParseNodeFullName(fullName string) (moduleName string, componentName string, err error) {
	parts := strings.Split(fullName, nameSeparator)
	if len(parts) < 3 {
		return "", "", fmt.Errorf("node name %s is invalid, separator not found", fullName)
	}
	return parts[1], parts[2], nil
}

// getMD5Hash returns the MD5 hash of a string as a hexadecimal string.
func getMD5Hash(s string) string {
	data := []byte(s)
	hashBytes := md5.Sum(data)
	return hex.EncodeToString(hashBytes[:])
}

// getFirstNChars returns the first n characters of a string.
// Handles multi-byte characters correctly using runes.
func getFirstNChars(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}
