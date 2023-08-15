package module

import "github.com/tiny-systems/module/internal/server/api/module-go"

type Service struct {
	module.UnimplementedModuleServiceServer
}

// NewService implement messaging
func NewService() *Service {
	return &Service{}
}
