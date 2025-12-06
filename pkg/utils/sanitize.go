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
	return limitByRune(name, 48)
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
