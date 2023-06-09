package module

import (
	"context"
	"fmt"
	"github.com/nats-io/nats.go"
	"github.com/tiny-systems/module/internal/instance"
	"github.com/tiny-systems/module/pkg/api/module-go"
	"github.com/tiny-systems/module/pkg/utils"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/proto"
	"strings"
	"time"
)

func (s *Server) newInstance(ctx context.Context, configMsg *instance.Msg) error {
	if configMsg.Data == nil {
		s.errorCh <- fmt.Errorf("new instance message has no data")
		// do not return error to avoid failing all server
		return nil
	}
	//
	installedComponent, ok := s.installedComponentsMap[configMsg.Subject]
	if !ok {
		s.errorCh <- fmt.Errorf("component %s is not installed", configMsg.Subject)
		// do not return error to avoid failing all server
		return nil
	}

	conf, ok := configMsg.Data.(*module.ConfigureInstanceRequest)
	if !ok {
		s.errorCh <- fmt.Errorf("new instance message is invalid")
		// do not return error to avoid failing all server
		return nil
	}

	subj := utils.GetInstanceInputSubject(s.runnerConfig.WorkspaceID, conf.FlowID, conf.InstanceID, "*")

	inputCh := make(chan *instance.Msg)
	defer close(inputCh)

	instanceID := getInstanceIDFromSubject(subj)
	s.communicationChLock.Lock()

	_, exists := s.communicationCh[instanceID]
	if !exists {
		s.communicationCh[instanceID] = inputCh
	}
	s.communicationChLock.Unlock()

	if exists {
		s.errorCh <- fmt.Errorf("instance already spinning")
		return nil
	}

	defer func() {
		// delete instance from map of channels
		s.communicationChLock.Lock()
		delete(s.communicationCh, instanceID)
		s.communicationChLock.Unlock()
	}()

	outputCh := make(chan *instance.Msg)
	defer close(outputCh)

	subInput, err := s.nats.QueueSubscribe(subj, conf.InstanceID, func(msg *nats.Msg) {
		// because we use different DTO we need this switch
		var msgIn = &module.MessageRequest{}
		if err := proto.Unmarshal(msg.Data, msgIn); err != nil {
			s.errorCh <- err
			return
		}

		// writing dto into instance's input
		inputCh <- instance.NewMsgWithSubject(getPort(msg.Subject), msgIn, func(data interface{}) error {
			resp, ok := data.(*module.MessageResponse)
			if !ok {
				return fmt.Errorf("invalid input's custom response, port: %s")
			}
			bytes, err := proto.Marshal(resp)

			if err != nil {
				return err
			}
			return msg.Respond(bytes)
		})
	})

	if err != nil {
		s.errorCh <- fmt.Errorf("failed to subscribe nats input subject: %s", subj)
		// do not return error to avoid failing all server
		return nil
	}

	subj = utils.GetInstanceControlSubject(s.runnerConfig.WorkspaceID, conf.FlowID, conf.InstanceID, "*")

	subControl, err := s.nats.Subscribe(subj, func(msg *nats.Msg) {
		// process incoming messages

		port := getPort(msg.Subject)
		// because we use different DTO we need this switch

		var msgIn = &module.ConfigureInstanceRequest{}
		if err := proto.Unmarshal(msg.Data, msgIn); err != nil {
			s.errorCh <- err
			return
		}

		// writing dto into instance's input
		inputCh <- instance.NewMsgWithSubject(port, msgIn, func(data interface{}) error {
			resp, ok := data.(*module.ConfigureInstanceResponse)
			if !ok {
				return fmt.Errorf("invalid input's response")
			}
			bytes, err := proto.Marshal(resp)
			if err != nil {
				return err
			}
			return msg.Respond(bytes)
		})
	})

	if err != nil {
		s.errorCh <- fmt.Errorf("failed to subscribe nats control subject: %s", subj)
		// do not return error to avoid failing all server
		return nil
	}

	// instance's runtime context
	sCtx, cancel := context.WithCancel(ctx)
	// cancel
	defer cancel()

	// read output component may send back
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.log.Error().Str("recovery", fmt.Sprintf("%v", r)).Msg("output channel read")
			}
		}()

		for {
			// read instance's output
			select {
			case <-sCtx.Done():
				return
			case output, ok := <-outputCh:
				if !ok {
					// closed channel
					return
				}
				// send each message async
				// check if instance is running locally
				s.communicationChLock.RLock()
				localInputCh, isLocal := s.communicationCh[getInstanceIDFromSubject(output.Subject)]
				s.communicationChLock.RUnlock()

				if isLocal {
					output.Subject = getPort(output.Subject)
					localInputCh <- output
				} else {
					// not local, use nats
					if payload, ok := output.Data.(*module.MessageRequest); ok {
						bytes, err := proto.Marshal(payload)
						if err != nil {
							s.errorCh <- err
							continue
						}
						if _, err := s.nats.Request(output.Subject, bytes, time.Second*3); err != nil {
							s.errorCh <- fmt.Errorf("nats request error: %v", err)
						}
					}
				}
			}
		}
	}()

	s.log.Info().Str("cmp", installedComponent.component.GetInfo().Name).
		Str("workspace", s.runnerConfig.WorkspaceID).Msg("run new instance")

	// waiting here until instance is running
	if err := s.spinNewInstance(sCtx, configMsg, installedComponent, inputCh, outputCh); err != nil {
		s.errorCh <- err
	}

	s.log.Info().Str("subj", subControl.Subject).Msg("unsubscribe control")
	if err = subControl.Unsubscribe(); err != nil {
		s.log.Error().Err(err).Msg("unsubscribe control error")
	}

	s.log.Info().Str("subj", subInput.Subject).Msg("unsubscribe input")
	if err = subInput.Unsubscribe(); err != nil {
		s.log.Error().Err(err).Msg("unsubscribe input error")
	}

	s.log.Info().Str("cmp", installedComponent.component.GetInfo().Name).
		Str("serverId", s.runnerConfig.ServerID).
		Str("workspace", s.runnerConfig.WorkspaceID).Msg("instance destroyed")
	return nil
}

func (s *Server) Run(ctx context.Context) error {

	var subscriptions = make([]*nats.Subscription, 0)
	wg, ctx := errgroup.WithContext(ctx)

	wg.Go(func() error {
		<-ctx.Done()
		// when context done - close all component subscriptions
		for _, sub := range subscriptions {
			s.unsubscribe(sub)
		}
		return nil
	})

	// make server discoverable without even running a single node
	wg.Go(func() error {
		return s.registry.Discover(ctx, utils.GetServerLookupSubject(s.runnerConfig.WorkspaceID), s.runnerConfig.ServerID, func() []byte {
			data, err := proto.Marshal(s.getDiscoveryNode())
			if err != nil {
				s.errorCh <- fmt.Errorf("discover error: %v", err)
			}
			return data
		})
	})

	defer func() {
		// close all control channels
		close(s.newInstanceCh)
		close(s.installComponentsCh)
	}()

loop:
	for {
		select {
		case configMsg := <-s.newInstanceCh:
			wg.Go(func() error {
				return s.newInstance(ctx, configMsg)
			})

		case installMsg := <-s.installComponentsCh:
			// install component
			s.log.Debug().Str("id", installMsg.id).Msg("installing")

			// registered in the installed components map
			s.installedComponentsMap[installMsg.id] = installMsg

			node, err := installMsg.GetDiscoveryNode()
			if err != nil {
				return err
			}

			node.WorkspaceID = s.runnerConfig.WorkspaceID
			node.ServerID = s.runnerConfig.ServerID

			wg.Go(func() error {

				// make component discoverable
				return s.registry.Discover(ctx, utils.GetComponentLookupSubject(node.WorkspaceID), installMsg.id, func() []byte {
					discoveryNode, err := installMsg.GetDiscoveryNode()
					if err != nil {
						s.errorCh <- fmt.Errorf("get component discover node error: %v", err)
						return nil
					}
					data, err := proto.Marshal(discoveryNode)
					if err != nil {
						s.errorCh <- fmt.Errorf("discover error: %v", err)
					}
					return data
				})
			})

			subj := utils.CreateComponentSubject(s.runnerConfig.WorkspaceID, installMsg.id)
			s.log.Info().Str("subject", subj).Msg("subscribe")

			subscription, err := s.nats.Subscribe(subj, func(msg *nats.Msg) {
				//decode from nats msg
				// create new instance on a graph
				var conf = &module.ConfigureInstanceRequest{}
				if err := proto.Unmarshal(msg.Data, conf); err != nil {
					s.errorCh <- err
					return
				}
				s.newInstanceCh <- instance.NewMsgWithSubject(installMsg.id, conf, func(data interface{}) error {
					resp, ok := data.(*module.ConfigureInstanceResponse)
					if !ok {
						return fmt.Errorf("invalid new instance's response")
					}
					bytes, err := proto.Marshal(resp)
					if err != nil {
						return err
					}
					return msg.Respond(bytes)
				})
			})
			if err != nil {
				return err
			}
			subscriptions = append(subscriptions, subscription)
		case <-ctx.Done():
			break loop
		}
	}
	_ = wg.Wait()
	s.log.Info().Msg("server stopped")
	return nil
}

func (s *Server) unsubscribe(sub *nats.Subscription) {
	s.log.Info().Str("subj", sub.Subject).Msg("unsubscribe")

	if err := sub.Drain(); err != nil {
		s.errorCh <- fmt.Errorf("drain sub error: %v", err)
		return
	}
	if err := sub.Unsubscribe(); err != nil {
		s.errorCh <- fmt.Errorf("unsubscribe error: %v", err)
	}
}

func getPort(s string) string {
	//
	parts := strings.Split(s, ".")
	return parts[len(parts)-1]
}

func getInstanceIDFromSubject(s string) string {
	parts := strings.Split(s, ".")
	return strings.Join(parts[:len(parts)-1], ".")
}
