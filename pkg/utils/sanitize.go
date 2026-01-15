package utils

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

func SanitizeResourceName(in string) string {
	reg, err := regexp.Compile("[^A-Za-z0-9]+")
	if err != nil {
		return ""
	}
	name := reg.ReplaceAllString(strings.ToLower(in), "-")
	name = strings.Trim(name, "-")
	return limitByRuneKeepSuffix(name, 48)
}

// SanitizeIdentity sanitizes a string for use as leader election identity.
// Unlike SanitizeResourceName, this does not truncate as identity doesn't have
// the same length restrictions as Kubernetes resource names.
func SanitizeIdentity(in string) string {
	reg, err := regexp.Compile("[^A-Za-z0-9]+")
	if err != nil {
		return ""
	}
	return reg.ReplaceAllString(strings.ToLower(in), "-")
}

// limitByRuneKeepSuffix truncates a string to maxLength while preserving the suffix.
// This is important for Kubernetes pod names where the unique identifier is at the end.
// Example: "tinysystems-http-module-v0-controller-manager-67fc65b5bc-6mdh8" (61 chars)
// becomes: "ystems-http-module-v0-controller-manager-67fc65b5bc-6mdh8" (48 chars)
// This ensures different pods have different identities for leader election.
func limitByRuneKeepSuffix(s string, maxLength int) string {
	if utf8.RuneCountInString(s) <= maxLength {
		return s
	}

	runes := []rune(s)
	// Keep the suffix (last maxLength characters) instead of prefix
	return string(runes[len(runes)-maxLength:])
}

func limitByRune(s string, maxLength int) string {
	if utf8.RuneCountInString(s) <= maxLength {
		return s
	}

	// Convert string to a slice of runes
	runes := []rune(s)

	// Slice the rune slice and convert back to string
	return string(runes[:maxLength])
}
