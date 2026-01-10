package client

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/tiny-systems/module/internal/scheduler/runner"
	"github.com/tiny-systems/module/internal/server/api/module-go"
	module2 "github.com/tiny-systems/module/module"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

type Pool interface {
	Register(moduleName, addr string)
	Deregister(moduleName string)
}

type AddressPool struct {
	log          logr.Logger
	addressTable cmap.ConcurrentMap[string, string]
	clients      cmap.ConcurrentMap[string, module.ModuleServiceClient]
	conns        cmap.ConcurrentMap[string, *grpc.ClientConn] // Store connections for lifecycle management
	errGroup     *errgroup.Group
	runCtx       context.Context
}

func (p *AddressPool) Register(moduleName, addr string) {
	p.log.Info("address pool: registering module",
		"module", moduleName,
		"addr", addr,
	)
	p.addressTable.Set(moduleName, addr)

	// Pre-warm connection in background to avoid cold start latency on first request
	if p.runCtx != nil {
		go func() {
			if _, err := p.getClient(p.runCtx, addr); err != nil {
				p.log.Error(err, "address pool: failed to pre-warm connection",
					"module", moduleName,
					"addr", addr,
				)
			} else {
				p.log.Info("address pool: connection pre-warmed",
					"module", moduleName,
					"addr", addr,
				)
			}
		}()
	}
}

func (p *AddressPool) Deregister(moduleName string) {
	p.log.Info("address pool: deregistering module",
		"module", moduleName,
	)

	// Get the address before removing from table
	addr, ok := p.addressTable.Get(moduleName)
	p.addressTable.Remove(moduleName)

	if !ok {
		return
	}

	// Check if any other module uses the same address
	stillInUse := false
	p.addressTable.IterCb(func(_ string, a string) {
		if a == addr {
			stillInUse = true
		}
	})

	// Only close connection if no other module uses this address
	if !stillInUse {
		if conn, exists := p.conns.Get(addr); exists {
			p.log.Info("address pool: closing connection",
				"module", moduleName,
				"addr", addr,
			)
			_ = conn.Close()
			p.conns.Remove(addr)
			p.clients.Remove(addr)
		}
	}
}

func NewPool() *AddressPool {
	return &AddressPool{
		addressTable: cmap.New[string](),
		clients:      cmap.New[module.ModuleServiceClient](),
		conns:        cmap.New[*grpc.ClientConn](),
		errGroup:     &errgroup.Group{},
	}
}

func (p *AddressPool) SetLogger(l logr.Logger) *AddressPool {
	p.log = l
	return p
}

func (p *AddressPool) Handler(ctx context.Context, msg *runner.Msg) ([]byte, error) {
	moduleName, _, err := module2.ParseFullName(msg.To)
	if err != nil {
		return nil, err
	}

	addr, ok := p.addressTable.Get(moduleName)
	if !ok {
		return nil, fmt.Errorf("%s module address is unknown", moduleName)
	}

	client, err := p.getClient(ctx, addr)
	if err != nil {
		return nil, err
	}

	resp, err := client.Message(ctx, &module.MessageRequest{
		From:    msg.From,
		Payload: msg.Data,
		EdgeID:  msg.EdgeID,
		To:      msg.To,
	})
	if err != nil {
		return nil, err
	}

	return resp.Data, nil
}

func (p *AddressPool) Start(ctx context.Context) error {
	p.runCtx = ctx
	<-p.runCtx.Done()
	return p.errGroup.Wait()
}

func (p *AddressPool) getClient(ctx context.Context, addr string) (module.ModuleServiceClient, error) {
	if client, ok := p.clients.Get(addr); ok {
		return client, nil
	}

	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                10 * time.Second, // ping every 10s if idle
			Timeout:             3 * time.Second,  // wait 3s for ping ack
			PermitWithoutStream: true,             // ping even without active RPCs
		}),
	)
	if err != nil {
		p.log.Error(err, "grpc client: connection failed", "addr", addr)
		return nil, err
	}

	// Trigger immediate connection instead of waiting for first RPC
	// This eliminates first-message latency
	conn.Connect()

	p.conns.Set(addr, conn)

	p.errGroup.Go(func() error {
		<-p.runCtx.Done()
		_ = conn.Close()
		return nil
	})

	client := module.NewModuleServiceClient(conn)
	p.clients.Set(addr, client)
	return client, nil
}
