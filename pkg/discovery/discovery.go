package discovery

import (
	"context"
	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
	modulepb "github.com/tiny-systems/module/pkg/api/module-go"
	"github.com/tiny-systems/module/pkg/utils"
	"google.golang.org/protobuf/proto"
	"time"
)

type Getter func() [][]byte

type Registry struct {
	nc  *nats.Conn
	log zerolog.Logger
}

func NewRegistry(nc *nats.Conn) *Registry {
	return &Registry{nc: nc}
}

func (s *Registry) SetLogger(l zerolog.Logger) *Registry {
	s.log = l
	return s
}

func (s *Registry) LookupServers(ctx context.Context, workspaceID string) ([]*modulepb.DiscoveryNode, error) {
	callback, err := utils.GetCallbackSubject()
	if err != nil {
		return nil, err
	}
	return s.lookupNodes(ctx, callback, utils.GetServerLookupSubject(workspaceID), time.Millisecond*200)
}

func (s *Registry) LookupFlows(ctx context.Context, workspaceID string) ([]*modulepb.DiscoveryNode, error) {
	callback, err := utils.GetCallbackSubject()
	if err != nil {
		return nil, err
	}
	return s.lookupNodes(ctx, callback, utils.GetFlowLookupSubject(workspaceID), time.Millisecond*200)
}

func (s *Registry) LookupComponents(ctx context.Context, workspaceID string) ([]*modulepb.DiscoveryNode, error) {
	callback, err := utils.GetCallbackSubject()
	if err != nil {
		return nil, err
	}
	return s.lookupNodes(ctx, callback, utils.GetComponentLookupSubject(workspaceID), time.Millisecond*200)
}

func (s *Registry) LookupNodes(ctx context.Context, flowID string) ([]*modulepb.DiscoveryNode, error) {
	callback, err := utils.GetCallbackSubject()
	if err != nil {
		return nil, err
	}
	return s.lookupNodes(ctx, callback, utils.GetNodesLookupSubject(flowID), time.Millisecond*200)
}

func (s *Registry) LookupStatNodes(ctx context.Context, flowID string) ([]*modulepb.DiscoveryNode, error) {
	callback, err := utils.GetCallbackSubject()
	if err != nil {
		return nil, err
	}
	return s.lookupNodes(ctx, callback, utils.GetNodeStatsLookupSubject(flowID), time.Millisecond*200)
}

func (s *Registry) lookupNodes(ctx context.Context, replySubject, subj string, minDur time.Duration) ([]*modulepb.DiscoveryNode, error) {
	data, err := s.lookup(ctx, replySubject, subj, minDur)
	if err != nil {
		return nil, err
	}
	var result []*modulepb.DiscoveryNode

	for _, d := range data {
		el := &modulepb.DiscoveryNode{}
		if err = proto.Unmarshal(d, el); err != nil {
			continue
		}
		result = append(result, el)
	}
	return result, nil
}

// Discover universal method listens for subj messages and responses back to the reply subject
func (s *Registry) Discover(ctx context.Context, subj string, instanceID string, getData func() []byte) error {

	sub, err := s.nc.QueueSubscribe(subj, instanceID, func(msg *nats.Msg) {
		// publish few messages
		if err := s.nc.PublishMsg(&nats.Msg{
			Subject: msg.Reply,
			Data:    getData(),
			Header: map[string][]string{
				"id": {instanceID},
			},
		}); err != nil {
			s.log.Error().Err(err).Msg("publish msg back error")
		}
	})
	if err != nil {
		return err
	}

	defer func() {
		s.nc.Flush()
		sub.Drain()
		sub.Unsubscribe()
	}()

	<-ctx.Done()
	return nil
}

// lookup broadcasts message into subject and listens replysubject, first data available in minDur, subscription cached for maxDur
// after maxDur you need to lookup once more
func (s *Registry) lookup(ctx context.Context, replySubject, subject string, dur time.Duration) (result map[string][]byte, err error) {

	ctx, cancel := context.WithTimeout(ctx, dur)
	defer cancel()

	result = make(map[string][]byte)

	sub, err := s.nc.Subscribe(replySubject, func(msg *nats.Msg) {
		id := msg.Header.Get("id")
		if id == "" {
			return
		}
		result[id] = msg.Data
	})
	if err != nil {
		return result, err
	}

	msg := &nats.Msg{
		Reply:   replySubject,
		Subject: subject,
	}
	if err := s.nc.PublishMsg(msg); err != nil {
		return result, err
	}

	s.nc.Flush()

	defer func() {
		sub.Drain()
		sub.Unsubscribe()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		}
	}

}
