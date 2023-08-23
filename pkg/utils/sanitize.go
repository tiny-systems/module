package utils

import (
	"regexp"
	"strings"
)

func SanitizeResourceName(in string) string {
	reg, err := regexp.Compile("[^A-Za-z0-9]+")
	if err != nil {
		return ""
	}
	name := reg.ReplaceAllString(strings.ToLower(in), "-")
	if len(name) > 100 {
		name = name[:99]
	}
	return name
}
