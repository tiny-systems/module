package module

import (
	"context"
	"fmt"
	"github.com/tiny-systems/module/internal/instance"
	"github.com/tiny-systems/module/pkg/api/module-go"
	m "github.com/tiny-systems/module/pkg/module"
	"github.com/tiny-systems/module/pkg/service-discovery/discovery"
)

func (s *Server) InstallComponent(ctx context.Context, info m.Info, c m.Component) error {

	installID, err := m.GetComponentID(info, c.GetInfo())
	if err != nil {
		return err
	}
	data := map[string]interface{}{
		"name":      "", // no actual Flow scope instance name
		"run":       false,
		"component": installID,
	}
	s.installComponentsCh <- &installComponentMsg{
		id:        installID,
		component: c.Instance(),
		module:    info,
		data:      data,
	}
	return err
}

func (s *Server) RunInstance(conf *module.ConfigureInstanceRequest) {
	s.newInstanceCh <- instance.NewMsgWithSubject(conf.ComponentID, conf, func(data interface{}) error {
		return nil
	})
}

// spinNewInstance async to avoid slow consumer
func (s *Server) spinNewInstance(ctx context.Context, runConfigMsg *instance.Msg, cmp *installComponentMsg, inputCh chan *instance.Msg, outputCh chan *instance.Msg) error {
	// make new instance discoverable
	// deploy instance

	// create component runner
	runner := instance.NewRunner(cmp.component.Instance(), cmp.module).
		SetLogger(s.log).
		SetConfig(s.runnerConfig)

	// deploy
	runCtx, runCancel := context.WithCancel(ctx)
	defer runCancel()

	go func() {
		if err := s.discovery.KeepAlive(runCtx, func() discovery.Node {
			node := runner.Discovery(runCtx)
			node.ServerID = s.runnerConfig.ServerID
			node.WorkspaceID = s.runnerConfig.WorkspaceID
			return node
		}); err != nil {
			s.errorCh <- fmt.Errorf("discovery keep alive error: %v", err)
		}
	}()
	return runner.Run(runCtx, runConfigMsg, inputCh, outputCh)
}
