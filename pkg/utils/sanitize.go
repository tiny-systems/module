package utils

import (
	"regexp"
)

func SanitizeResourceName(in string) string {
	reg, err := regexp.Compile("[^A-Za-z0-9]+")
	if err != nil {
		return ""
	}
	return reg.ReplaceAllString(in, "-")
}
