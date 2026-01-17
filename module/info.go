package module

import (
	"fmt"
	"strings"

	"github.com/tiny-systems/module/pkg/utils"
	"k8s.io/apimachinery/pkg/util/version"
)

const nameSeparator = "."

// ParseFullName parses a full node name (module.node) into its components.
func ParseFullName(fullName string) (module string, node string, err error) {
	parts := strings.Split(fullName, nameSeparator)
	if len(parts) < 2 {
		return "", "", fmt.Errorf("node name is invalid, separator not found")
	}
	return parts[0], parts[1], nil
}

type Info struct {
	Name      string
	VersionID string // if module's build is registered
	Version   string
	Addr      string //listed address
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
