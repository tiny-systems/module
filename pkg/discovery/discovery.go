package discovery

import (
	"context"
	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
	modulepb "github.com/tiny-systems/module/pkg/api/module-go"
	"github.com/tiny-systems/module/pkg/utils"
	"google.golang.org/protobuf/proto"
	"sync"
	"time"
)

type Getter func() [][]byte

type Registry struct {
	nc    *nats.Conn
	log   zerolog.Logger
	cache *sync.Map
}

func NewRegistry(nc *nats.Conn) *Registry {
	return &Registry{nc: nc, cache: &sync.Map{}}
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
	return s.Lookup(callback, utils.GetServerLookupSubject(workspaceID), time.Millisecond*500, time.Second*60)
}

func (s *Registry) LookupModules(ctx context.Context, serverID string) ([]*modulepb.DiscoveryNode, error) {
	callback, err := utils.GetCallbackSubject()
	if err != nil {
		return nil, err
	}
	return s.Lookup(callback, utils.GetModuleLookupSubject(serverID), time.Millisecond*500, time.Second*60)
}

func (s *Registry) LookupFlows(ctx context.Context, workspaceID string) ([]*modulepb.DiscoveryNode, error) {
	callback, err := utils.GetCallbackSubject()
	if err != nil {
		return nil, err
	}
	return s.Lookup(callback, utils.GetFlowLookupSubject(workspaceID), time.Millisecond*500, time.Second*60)
}

func (s *Registry) LookupComponents(ctx context.Context, workspaceID string) ([]*modulepb.DiscoveryNode, error) {
	callback, err := utils.GetCallbackSubject()
	if err != nil {
		return nil, err
	}
	return s.Lookup(callback, utils.GetComponentLookupSubject(workspaceID), time.Millisecond*500, time.Second*60)
}

func (s *Registry) LookupNodes(flowID string) ([]*modulepb.DiscoveryNode, error) {
	callback, err := utils.GetCallbackSubject()
	if err != nil {
		return nil, err
	}
	return s.Lookup(callback, utils.GetNodesLookupSubject(flowID), time.Millisecond*500, time.Second*60)
}

func (s *Registry) Lookup(replySubject, subj string, minDur time.Duration, maxDur time.Duration) ([]*modulepb.DiscoveryNode, error) {
	getter, err := s.lookup(context.Background(), replySubject, subj, minDur, maxDur)
	if err != nil {
		return nil, err
	}
	var result []*modulepb.DiscoveryNode

	for _, d := range getter() {
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

// lookup broadcasts message into subject and listens replysubject, first data availabile in minDur, subscription cached for maxDur
// after maxDur you need to lookup once more
func (s *Registry) lookup(ctx context.Context, replySubject, subject string, minDur time.Duration, maxDur time.Duration) (f Getter, err error) {
	if cached, ok := s.cache.Load(subject); ok {
		return cached.(Getter), nil
	}
	ready := make(chan struct{}, 0)
	//
	type resultMapItem struct {
		data       []byte
		lastUpdate time.Time
	}

	var resultMap = make(map[string]*resultMapItem)
	f = func() [][]byte {
		<-ready
		var result = make([][]byte, len(resultMap))
		var i int
		for _, v := range resultMap {
			if v.lastUpdate.Before(time.Now().Add(-minDur * 2)) {
				// skip old records
				continue
			}
			result[i] = v.data
			i++
		}
		return result
	}
	s.cache.Store(subject, f)

	go func() {
		defer s.cache.Delete(subject)
		sub, err := s.nc.Subscribe(replySubject, func(msg *nats.Msg) {
			id := msg.Header.Get("id")
			if id == "" {
				return
			}
			resultMap[id] = &resultMapItem{
				data:       msg.Data,
				lastUpdate: time.Now(),
			}
		})
		if err != nil {
			return
		}
		var request = func() {
			msg := &nats.Msg{
				Reply:   replySubject,
				Subject: subject,
			}
			if err := s.nc.PublishMsg(msg); err != nil {
				return
			}
		}

		request()
		s.nc.Flush()

		defer func() {
			sub.Drain()
			sub.Unsubscribe()
		}()

		maxTimer := time.NewTimer(maxDur)
		defer maxTimer.Stop()

		minTimer := time.NewTimer(minDur)
		defer minTimer.Stop()

		minTicker := time.NewTicker(minDur)
		defer minTicker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-minTimer.C:
				close(ready)
			case <-minTicker.C:
				request()
			case <-maxTimer.C:
				return
			}
		}
	}()
	return f, err
}
