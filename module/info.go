package module

import (
	"fmt"
	"github.com/tiny-systems/module/pkg/utils"
	"k8s.io/apimachinery/pkg/util/version"
	"strings"
)

const nameSeparator = "."

type Info struct {
	Name      string
	VersionID string // if module's build is registered
	Version   string
	Addr      string //listed address
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

func GetNodeFullName(prefix string, module string, component string) string {
	return fmt.Sprintf("%s%s%s%s%s", prefix, nameSeparator, module, nameSeparator, component)
}

func ParseFullName(fullName string) (module string, component string, err error) {
	parts := strings.Split(fullName, nameSeparator)
	if len(parts) < 3 {
		return "", "", fmt.Errorf("node name %s is invalid, separator not found", fullName)
	}
	return parts[1], parts[2], nil
}
