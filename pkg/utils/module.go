package utils

import (
	"github.com/tiny-systems/module/pkg/api/module-go"
	m "github.com/tiny-systems/module/pkg/module"
)

func GetModuleApi(i m.Info) (*module.ModuleVersion, error) {
	return &module.ModuleVersion{
		ID:         i.VersionID,
		ModuleName: i.Name,
		Version:    i.Version,
	}, nil
}
