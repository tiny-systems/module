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

// IServer receive api requests from other modules and sends it to scheduler
type IServer interface {
	Start(ctx context.Context) error
}

type Server struct {
	log logr.Logger
}

func New() *Server {
	return &Server{}
}

func (s *Server) SetLogger(l logr.Logger) *Server {
	s.log = l
	return s
}

func (s *Server) Start(ctx context.Context, output chan *runner.Msg, listenAddr string, clb func(net.Addr)) error {

	wg, ctx := errgroup.WithContext(ctx)

	server := grpc.NewServer()
	grpc_health_v1.RegisterHealthServer(server, health.NewChecker())
	//
	modulepb.RegisterModuleServiceServer(server, module.NewService(func(ctx context.Context, req *modulepb.MessageRequest) (*modulepb.MessageResponse, error) {
		output <- &runner.Msg{
			EdgeID: req.EdgeID,
			To:     req.To,
			Data:   req.Payload,
			From:   req.From,
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
