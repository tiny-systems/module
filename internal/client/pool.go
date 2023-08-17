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

// DefaultStoreTTL for how long we keep grpc client in a pool, default 1h
const DefaultStoreTTL = 60 * 60

type Pool struct {
	log          logr.Logger
	addressTable cmap.ConcurrentMap[string, string]
	clients      cmap.ConcurrentMap[string, module.ModuleServiceClient]
}

func NewPool(addressTable cmap.ConcurrentMap[string, string]) *Pool {
	return &Pool{
		addressTable: addressTable, clients: cmap.New[module.ModuleServiceClient](),
	}
}

func (p *Pool) SetLogger(l logr.Logger) *Pool {
	p.log = l
	return p
}

func (p *Pool) Start(ctx context.Context, inputCh chan *runner.Msg) error {

	wg, ctx := errgroup.WithContext(ctx)

	for {
		select {
		case <-ctx.Done():
			return wg.Wait()
		case msg := <-inputCh:
			moduleName, _, err := module2.ParseFullName(msg.To)
			if err != nil {
				p.log.Error(err, "unable to parse node name")
				continue
			}

			addr, ok := p.addressTable.Get(moduleName)
			if !ok {
				p.log.Error(err, "module address is unknown", "name", moduleName)
				continue
			}
			client, err := p.getClient(ctx, wg, addr)
			if err != nil {
				p.log.Error(err, "unable to get client", "addr", addr)
			}

			_, err = client.Message(ctx, &module.MessageRequest{
				From:    msg.From,
				Payload: msg.Data,
				EdgeID:  msg.EdgeID,
				To:      msg.To,
			})
			if err != nil {
				p.log.Error(err, "error sending request", "to", addr)
			}
		}
	}
}

func (p *Pool) getClient(ctx context.Context, wg *errgroup.Group, addr string) (module.ModuleServiceClient, error) {

	client, ok := p.clients.Get(addr)
	if ok {
		return client, nil
	}

	p.log.Info("creating new client", "addr", addr)
	conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}

	wg.Go(func() error {
		<-ctx.Done()
		p.log.Info("closing connection", "addr", addr)
		conn.Close()
		return nil
	})

	client = module.NewModuleServiceClient(conn)
	p.clients.Set(addr, client)
	return client, nil
}
