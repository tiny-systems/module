package utils

import (
	"github.com/tiny-systems/module/pkg/api/module-go"
	m "github.com/tiny-systems/module/pkg/module"
)

func GetComponentApi(c m.Component) (*module.Component, error) {
	componentInfo := c.GetInfo()

	return &module.Component{
		Name:        componentInfo.Name,
		Description: componentInfo.Description,
		Info:        componentInfo.Info,
		Tags:        componentInfo.Tags,
	}, nil
}
