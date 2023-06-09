package module

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/tiny-systems/module/internal/instance"
	"github.com/tiny-systems/module/pkg/api/module-go"
	"github.com/tiny-systems/module/pkg/discovery"
	m "github.com/tiny-systems/module/pkg/module"
	"github.com/tiny-systems/module/pkg/utils"
	"google.golang.org/protobuf/proto"
)

func (s *Server) InstallComponent(ctx context.Context, info m.Info, c m.Component) error {
	installID, err := m.GetComponentID(info, c.GetInfo())
	if err != nil {
		return err
	}
	s.installComponentsCh <- &installComponentMsg{
		id:        installID,
		component: c.Instance(),
		module:    info,
		data: map[string]interface{}{
			"name":      "", // no actual Flow scope instance name
			"run":       false,
			"component": installID,
		},
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
	componentInstance := cmp.component.Instance()

	if httpService, ok := componentInstance.(m.HTTPService); ok {
		httpService.HTTPService(func(port int) (string, error) {
			fmt.Println("exchange port", port)
			return "https://node12.workspace.tinysystems.dev", nil
		})
	}

	instanceRunner := instance.NewRunner(componentInstance, cmp.module).
		SetLogger(s.log).
		SetConfig(s.runnerConfig)

	registry := discovery.NewRegistry(s.nats)
	// deploy
	runCtx, runCancel := context.WithCancel(ctx)
	defer runCancel()

	waitCh, err := instanceRunner.Run(runCtx, runConfigMsg, inputCh, outputCh)
	if err != nil {
		return err
	}

	uniqID, err := uuid.NewUUID()
	if err != nil {
		return err
	}

	discoveryNode := instanceRunner.GetDiscoveryNode(false)

	go func() {
		// discover all nodes with its stats
		if err := registry.Discover(runCtx, utils.GetNodeStatsLookupSubject(discoveryNode.FlowID), uniqID.String(), func() []byte {
			data, _ := proto.Marshal(instanceRunner.GetDiscoveryNode(false))
			return data
		}); err != nil {
			s.errorCh <- fmt.Errorf("discover error: %v", err)
		}
	}()
	go func() {
		// discover all nodes within flow including port states
		if err := registry.Discover(runCtx, utils.GetNodesLookupSubject(discoveryNode.FlowID), uniqID.String(), func() []byte {
			data, _ := proto.Marshal(instanceRunner.GetDiscoveryNode(true))
			return data
		}); err != nil {
			s.errorCh <- fmt.Errorf("discover error: %v", err)
		}
	}()
	go func() {
		// make running flows discoverable
		if err := registry.Discover(runCtx, utils.GetFlowLookupSubject(discoveryNode.WorkspaceID), discoveryNode.FlowID, func() []byte {
			data, _ := proto.Marshal(instanceRunner.GetDiscoveryNode(false))
			return data
		}); err != nil {
			s.errorCh <- fmt.Errorf("discover error: %v", err)
		}
	}()
	<-waitCh
	return nil
}
