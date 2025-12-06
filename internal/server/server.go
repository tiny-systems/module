package server

import (
	"context"
	"github.com/go-logr/logr"
	"github.com/goccy/go-json"
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

func (s *server) Start(globalCtx context.Context, handler runner.Handler, listenAddr string, clb func(net.Addr)) error {

	wg, globalCtx := errgroup.WithContext(globalCtx)

	srv := grpc.NewServer(grpc.StatsHandler(otelgrpc.NewServerHandler()))
	grpc_health_v1.RegisterHealthServer(srv, health.NewChecker())

	//
	modulepb.RegisterModuleServiceServer(srv, module.NewService(func(ctx context.Context, req *modulepb.MessageRequest) (*modulepb.MessageResponse, error) {
		// incoming request from gRPC
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if globalCtx.Err() != nil {
			return nil, globalCtx.Err()
		}

		ctx, cancel := mergeContext(ctx, globalCtx)
		defer cancel()

		res, err := handler(ctx, &runner.Msg{
			EdgeID: req.EdgeID,
			To:     req.To,
			Data:   req.Payload,
			From:   req.From,
		})

		if err != nil {
			return nil, err
		}

		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		if resData, ok := res.([]byte); ok {
			return &modulepb.MessageResponse{
				Data: resData,
			}, err
		}

		data, err := json.Marshal(res)
		if err != nil {
			return nil, err
		}

		return &modulepb.MessageResponse{
			Data: data,
		}, nil
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

	<-globalCtx.Done()

	log.Info().Msg("graceful shutdown")
	srv.GracefulStop()

	return wg.Wait()
}

// mergeContext creates a new Context that is canceled when either context 'a' or context 'b' is canceled.
// This is crucial for long-running gRPC requests, as it ensures they are stopped
// if the client disconnects (a) OR the server is shutting down (b).
func mergeContext(a, b context.Context) (context.Context, context.CancelFunc) {
	// 1. Create a new child context that can be manually canceled.
	ctx, cancel := context.WithCancel(a) // 'a' is the initial parent for propagation

	// 2. Start a goroutine to listen for cancellation on both the server's context (b)
	// and the new merged context (mctx).
	go func() {
		select {
		case <-ctx.Done():
			// Case 1: The 'a' context (client request) was canceled first, which
			// automatically canceled mctx (since mctx is a child of a).
			// We exit this goroutine to prevent a leak.

		case <-b.Done():
			// Case 2: The 'b' context (server shutdown) was canceled first.
			// We explicitly call the new context's cancel function.
			cancel()
		}
	}()

	// 3. Return the new merged context and its cancel function.
	// The user of this function must call 'defer mcancel()' in their RPC handler
	// to release the resources associated with mctx.
	return ctx, cancel
}
