package module

import (
	"fmt"
	"github.com/hashicorp/go-version"
)

type Info struct {
	VersionID string // if module's build is registered
	Version   string
	Name      string
}

func GetComponentID(moduleInfo Info, cmpInfo ComponentInfo) (string, error) {
	ver, err := version.NewVersion(moduleInfo.Version)
	if err != nil {
		return "", fmt.Errorf("unable to parse module version: %v", err)
	}
	return fmt.Sprintf("%s_v%d_%s", moduleInfo.Name, ver.Segments()[0], cmpInfo.Name), nil
}
