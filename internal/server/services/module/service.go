package module

import (
	"context"
	"github.com/tiny-systems/module/internal/server/api/module-go"
)

type Handler func(ctx context.Context, req *module.MessageRequest) (*module.MessageResponse, error)

type Service struct {
	module.UnimplementedModuleServiceServer
	handler Handler
}

func (s *Service) Message(ctx context.Context, req *module.MessageRequest) (*module.MessageResponse, error) {
	return s.handler(ctx, req)
}

// NewService implement messaging
func NewService(handler Handler) *Service {
	return &Service{handler: handler}
}
