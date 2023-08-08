package server

import (
	"context"
	"github.com/go-logr/logr"
	"github.com/tiny-systems/module/internal/scheduler"
)

// IServer receive api requests from other modules and sends it to scheduler
type IServer interface {
	Start(ctx context.Context) error
}

type Server struct {
	log       logr.Logger
	scheduler scheduler.Scheduler
}

func New(log logr.Logger, scheduler scheduler.Scheduler) *Server {
	return &Server{log: log, scheduler: scheduler}
}

func (s Server) Start(ctx context.Context) error {
	<-ctx.Done()
	return nil
}
