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

// GetFullName Container image full name
func (i Info) GetFullName() string {
	return fmt.Sprintf("%s:%s", i.Name, i.Version)
}

func (i Info) GetMajorName() string {
	v, _ := version.ParseSemantic(i.Version)
	if v == nil {
		return i.GetFullName()
	}
	return fmt.Sprintf("%s:%d", i.Name, v.Major())
}

func (i Info) GetMajorNameSanitised() string {
	return utils.SanitizeResourceName(i.GetMajorName())
}

func GetNodeFullName(module string, component string) string {
	return fmt.Sprintf("%s%s%s", module, nameSeparator, component)
}

func ParseFullName(fullName string) (module string, component string, err error) {
	parts := strings.Split(fullName, nameSeparator)
	if len(parts) < 2 {
		return "", "", fmt.Errorf("node name is invalid, separator not found")
	}
	return parts[0], parts[1], nil
}