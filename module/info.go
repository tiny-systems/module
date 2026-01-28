package module

import (
	"fmt"
	"strings"

	"github.com/tiny-systems/module/pkg/utils"
	"k8s.io/apimachinery/pkg/util/version"
)

const nameSeparator = "."

// ParseFullName parses a full node name (prefix.module.component) into its components.
// Node names have the format: {project-prefix}.{module-name}.{component-name}
func ParseFullName(fullName string) (module string, component string, err error) {
	parts := strings.Split(fullName, nameSeparator)
	if len(parts) < 3 {
		return "", "", fmt.Errorf("node name %s is invalid, separator not found", fullName)
	}
	return parts[1], parts[2], nil
}

type Info struct {
	Name       string
	VersionID  string // if module's build is registered
	Version    string
	SDKVersion string // SDK version this module was built with
	Addr       string //listed address
	//
	Components []ComponentInfo
}

// GetNameAndVersion Container image full name
func (i Info) GetNameAndVersion() string {
	return fmt.Sprintf("%s:%s", i.Name, i.Version)
}

func (i Info) GetMajorName() string {
	v, _ := version.ParseSemantic(i.Version)
	if v == nil {
		return i.GetNameAndVersion()
	}
	return fmt.Sprintf("%s:v%d", i.Name, v.Major())
}

func (i Info) GetMajorNameSanitised() string {
	return utils.SanitizeResourceName(i.GetMajorName())
}

func (i Info) GetNameSanitised() string {
	return utils.SanitizeResourceName(i.Name)
}
