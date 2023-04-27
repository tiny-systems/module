package utils

import "strings"

func BytesToString(b []byte) string {
	buf := strings.Builder{}
	buf.Write(b)
	return buf.String()
}
