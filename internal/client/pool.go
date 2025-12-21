package client

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/tiny-systems/module/internal/scheduler/runner"
	"github.com/tiny-systems/module/internal/server/api/module-go"
	module2 "github.com/tiny-systems/module/module"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Pool interface {
	Register(moduleName, addr string)
	Deregister(moduleName string)
}

type AddressPool struct {
	log          logr.Logger
	addressTable cmap.ConcurrentMap[string, string]
	clients      cmap.ConcurrentMap[string, module.ModuleServiceClient]
	errGroup     *errgroup.Group
	runCtx       context.Context
}

func (p *AddressPool) Register(moduleName, addr string) {
	p.addressTable.Set(moduleName, addr)
}

func (p *AddressPool) Deregister(moduleName string) {
	p.addressTable.Remove(moduleName)
}

func NewPool() *AddressPool {
	return &AddressPool{
		addressTable: cmap.New[string](),
		clients:      cmap.New[module.ModuleServiceClient](),
		errGroup:     &errgroup.Group{},
	}
}

func (p *AddressPool) SetLogger(l logr.Logger) *AddressPool {
	p.log = l
	return p
}

func (p *AddressPool) Handler(ctx context.Context, msg *runner.Msg) ([]byte, error) {
	p.log.Info("grpc client: handling outgoing message",
		"to", msg.To,
		"from", msg.From,
		"edgeID", msg.EdgeID,
		"dataSize", len(msg.Data),
	)

	moduleName, _, err := module2.ParseFullName(msg.To)
	if err != nil {
		p.log.Error(err, "grpc client: failed to parse module name from destination",
			"to", msg.To,
		)
		return nil, err
	}

	addr, ok := p.addressTable.Get(moduleName)
	if !ok {
		p.log.Error(fmt.Errorf("module address unknown"), "grpc client: module not registered",
			"module", moduleName,
			"to", msg.To,
		)
		return nil, fmt.Errorf("%s module address is unknown", moduleName)
	}

	p.log.Info("grpc client: resolved module address",
		"module", moduleName,
		"addr", addr,
	)

	client, err := p.getClient(ctx, addr)
	if err != nil {
		p.log.Error(err, "grpc client: failed to get client connection",
			"addr", addr,
			"module", moduleName,
		)
		return nil, err
	}

	// sending request using gRPC
	p.log.Info("grpc client: sending message request",
		"addr", addr,
		"to", msg.To,
		"from", msg.From,
		"edgeID", msg.EdgeID,
	)

	var resp *module.MessageResponse

	resp, err = client.Message(ctx, &module.MessageRequest{
		From:    msg.From,
		Payload: msg.Data,
		EdgeID:  msg.EdgeID,
		To:      msg.To,
	})
	if err != nil {
		p.log.Error(err, "grpc client: message request failed",
			"to", msg.To,
			"from", msg.From,
			"addr", addr,
			"edgeID", msg.EdgeID,
		)
		return nil, err
	}

	p.log.Info("grpc client: message request completed",
		"to", msg.To,
		"addr", addr,
		"responseSize", len(resp.Data),
	)

	return resp.Data, nil
}

func (p *AddressPool) Start(ctx context.Context) error {
	p.runCtx = ctx
	<-p.runCtx.Done()
	return p.errGroup.Wait()
}

func (p *AddressPool) getClient(_ context.Context, addr string) (module.ModuleServiceClient, error) {

	client, ok := p.clients.Get(addr)
	if ok {
		p.log.Info("grpc client: reusing existing connection",
			"addr", addr,
		)
		return client, nil
	}

	p.log.Info("grpc client: creating new connection",
		"addr", addr,
	)

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithStatsHandler(otelgrpc.NewClientHandler()))
	if err != nil {
		p.log.Error(err, "grpc client: failed to create connection",
			"addr", addr,
		)
		return nil, err
	}

	p.log.Info("grpc client: connection created successfully",
		"addr", addr,
	)

	p.errGroup.Go(func() error {
		<-p.runCtx.Done()

		p.log.Info("grpc client: closing connection on shutdown",
			"addr", addr,
		)
		_ = conn.Close()
		return nil
	})

	client = module.NewModuleServiceClient(conn)
	p.clients.Set(addr, client)
	return client, nil
}
