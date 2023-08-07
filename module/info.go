package module

import (
	"fmt"
	"strings"
)

type Info struct {
	VersionID string // if module's build is registered
	Version   string
	Name      string
}

func (i Info) GetFullName() string {
	return fmt.Sprintf("%s:%s", i.Name, i.Version)
}

func sanitiseResource(in string) string {
	return strings.ReplaceAll(strings.ReplaceAll(in, ".", "-"), "_", "-")
}
