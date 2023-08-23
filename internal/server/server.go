package server

import (
	"context"
	"github.com/go-logr/logr"
	"github.com/rs/zerolog/log"
	"github.com/tiny-systems/module/internal/scheduler/runner"
	modulepb "github.com/tiny-systems/module/internal/server/api/module-go"
	"github.com/tiny-systems/module/internal/server/services/health"
	"github.com/tiny-systems/module/internal/server/services/module"
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

func (s *server) Start(ctx context.Context, output chan *runner.Msg, listenAddr string, clb func(net.Addr)) error {

	wg, ctx := errgroup.WithContext(ctx)

	server := grpc.NewServer()
	grpc_health_v1.RegisterHealthServer(server, health.NewChecker())
	//
	modulepb.RegisterModuleServiceServer(server, module.NewService(func(ctx context.Context, req *modulepb.MessageRequest) (*modulepb.MessageResponse, error) {
		// incoming request from gRPC
		output <- &runner.Msg{
			EdgeID: req.EdgeID,
			To:     req.To,
			Data:   req.Payload,
			From:   req.From,
			Meta:   req.GetMetadata(),
		}
		return &modulepb.MessageResponse{}, nil
	}))
	reflection.Register(server)

	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}

	defer lis.Close()
	clb(lis.Addr())

	wg.Go(func() error {
		// run grpc server
		err = server.Serve(lis)
		if err != nil {
			//
			return err
		}
		return nil
	})

	<-ctx.Done()
	log.Info().Msg("graceful shutdown")
	server.Stop()

	return wg.Wait()
}
