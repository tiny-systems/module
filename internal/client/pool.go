package client

import (
	"context"
	"github.com/go-logr/logr"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/tiny-systems/module/internal/scheduler/runner"
	"github.com/tiny-systems/module/internal/server/api/module-go"
	module2 "github.com/tiny-systems/module/module"
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

type pool struct {
	log          logr.Logger
	addressTable cmap.ConcurrentMap[string, string]
	clients      cmap.ConcurrentMap[string, module.ModuleServiceClient]
	errGroup     *errgroup.Group
	runCtx       context.Context
}

func (p *pool) Register(moduleName, addr string) {
	p.addressTable.Set(moduleName, addr)
}

func (p *pool) Deregister(moduleName string) {
	p.addressTable.Remove(moduleName)
}

func NewPool() *pool {
	return &pool{
		addressTable: cmap.New[string](),
		clients:      cmap.New[module.ModuleServiceClient](),
		errGroup:     &errgroup.Group{},
	}
}

func (p *pool) SetLogger(l logr.Logger) *pool {
	p.log = l
	return p
}

func (p *pool) Handler(ctx context.Context, msg *runner.Msg) error {
	moduleName, _, err := module2.ParseFullName(msg.To)
	if err != nil {
		return err
	}

	addr, ok := p.addressTable.Get(moduleName)
	if !ok {
		p.log.Error(err, "module address is unknown", "name", moduleName)
		return err
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

func (p *pool) Start(ctx context.Context) error {
	p.runCtx = ctx
	<-p.runCtx.Done()
	return p.errGroup.Wait()
}

func (p *pool) getClient(ctx context.Context, addr string) (module.ModuleServiceClient, error) {

	client, ok := p.clients.Get(addr)
	if ok {
		return client, nil
	}

	p.log.Info("creating new client", "addr", addr)
	conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
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
