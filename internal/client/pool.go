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

// DefaultStoreTTL for how long we keep grpc client in a pool, default 1h
const DefaultStoreTTL = 60 * 60

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

func (p *AddressPool) Handler(ctx context.Context, msg *runner.Msg) error {
	moduleName, _, err := module2.ParseFullName(msg.To)
	if err != nil {
		return err
	}

	addr, ok := p.addressTable.Get(moduleName)
	if !ok {
		return fmt.Errorf("%s module address is unknown", moduleName)
	}
	client, err := p.getClient(ctx, addr)
	if err != nil {
		p.log.Error(err, "unable to get client", "addr", addr)
	}

	// sending request using gRPC
	_, err = client.Message(ctx, &module.MessageRequest{
		From:    msg.From,
		Payload: msg.Data,
		EdgeID:  msg.EdgeID,
		To:      msg.To,
	})

	return err
}

func (p *AddressPool) Start(ctx context.Context) error {
	p.runCtx = ctx
	<-p.runCtx.Done()
	return p.errGroup.Wait()
}

func (p *AddressPool) getClient(_ context.Context, addr string) (module.ModuleServiceClient, error) {

	client, ok := p.clients.Get(addr)
	if ok {
		return client, nil
	}

	p.log.Info("creating a new gRPC client", "addr", addr)

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithStatsHandler(otelgrpc.NewClientHandler()))
	if err != nil {
		return nil, err
	}

	p.errGroup.Go(func() error {
		<-p.runCtx.Done()

		p.log.Info("closing client connection", "addr", addr)
		_ = conn.Close()
		return nil
	})

	client = module.NewModuleServiceClient(conn)
	p.clients.Set(addr, client)
	return client, nil
}
