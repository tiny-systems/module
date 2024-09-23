package server

import (
	"context"
	"github.com/go-logr/logr"
	"github.com/rs/zerolog/log"
	"github.com/tiny-systems/module/internal/scheduler/runner"
	modulepb "github.com/tiny-systems/module/internal/server/api/module-go"
	"github.com/tiny-systems/module/internal/server/services/health"
	"github.com/tiny-systems/module/internal/server/services/module"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	"net"
)

// Server receive api requests from other modules and sends it to scheduler
type Server interface {
	Start(ctx context.Context) error
}

type server struct {
	log logr.Logger
}

func New() *server {
	return &server{}
}

func (s *server) SetLogger(l logr.Logger) *server {
	s.log = l
	return s
}

func (s *server) Start(ctx context.Context, handler runner.Handler, listenAddr string, clb func(net.Addr)) error {

	wg, ctx := errgroup.WithContext(ctx)

	srv := grpc.NewServer(grpc.StatsHandler(otelgrpc.NewServerHandler()))

	grpc_health_v1.RegisterHealthServer(srv, health.NewChecker())
	//
	modulepb.RegisterModuleServiceServer(srv, module.NewService(func(ctx context.Context, req *modulepb.MessageRequest) (*modulepb.MessageResponse, error) {
		// incoming request from gRPC
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		err := handler(ctx, &runner.Msg{
			EdgeID: req.EdgeID,
			To:     req.To,
			Data:   req.Payload,
			From:   req.From,
		})
		return &modulepb.MessageResponse{}, err
	}))

	reflection.Register(srv)

	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}

	defer lis.Close()
	clb(lis.Addr())

	wg.Go(func() error {
		// run grpc server
		return srv.Serve(lis)
	})

	<-ctx.Done()
	log.Info().Msg("graceful shutdown")
	srv.Stop()

	return wg.Wait()
}
