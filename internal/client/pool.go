package client

import (
	"context"
	"github.com/davecgh/go-spew/spew"
	"github.com/go-logr/logr"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/tiny-systems/module/internal/scheduler/runner"
	module2 "github.com/tiny-systems/module/module"
)

type Pool struct {
	log          logr.Logger
	addressTable cmap.ConcurrentMap[string, string]
}

func NewPool(addressTable cmap.ConcurrentMap[string, string]) *Pool {
	return &Pool{addressTable: addressTable}
}

func (p *Pool) SetLogger(l logr.Logger) *Pool {
	p.log = l
	return p
}

func (p *Pool) Start(ctx context.Context, inputCh chan *runner.Msg) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case msg := <-inputCh:
			module, _, err := module2.ParseFullName(msg.To)
			if err != nil {
				p.log.Error(err, "unable to parse node name")
				continue
			}
			addr, ok := p.addressTable.Get(module)
			if !ok {
				p.log.Error(err, "module address is unknown", "name", module)
				continue
			}
			spew.Dump("send to ", addr, msg)
		}
	}
	return nil
}
