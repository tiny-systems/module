package registry

import (
	"context"
	"fmt"
	"github.com/nats-io/nats.go"
	"github.com/tiny-systems/module/pkg/service-discovery/discovery"
	"github.com/tiny-systems/module/pkg/service-discovery/util"
	"io"
	"strings"
	"time"
)

type NodeItem struct {
	subj   string
	expire time.Duration
	node   discovery.Node
}

type Registry struct {
	nc     *nats.Conn
	expire time.Duration
}

// NewRegistry create a service instance
func NewRegistry(nc *nats.Conn, expire time.Duration) (*Registry, error) {
	s := &Registry{
		nc:     nc,
		expire: expire,
	}

	if s.expire <= 0 {
		s.expire = discovery.DefaultExpire
	}
	return s, nil
}

func (s *Registry) checkExpires(nodes map[string]*NodeItem, now time.Duration, handleNodeAction func(discovery.Request) (bool, error)) error {
	for key, item := range nodes {
		if item.expire <= now {
			discoverySubj := strings.ReplaceAll(item.subj, discovery.DefaultPublishPrefix, discovery.DefaultDiscoveryPrefix)
			request := discovery.Request{
				Action: discovery.DeleteAction,
				Node:   item.node,
			}

			d, err := util.Marshal(request)
			if err != nil {
				return err
			}

			if err := s.nc.Publish(discoverySubj, d); err != nil {
				return nil
			}
			_, err = handleNodeAction(request)
			delete(nodes, key)
		}
	}
	return nil
}

func (s *Registry) Listen(ctx context.Context, handleNodeAction func(action discovery.Request) (bool, error)) error {

	ctx, cancel := context.WithCancel(ctx)

	defer cancel()

	if handleNodeAction == nil {
		err := fmt.Errorf("listen callback must be set for registry.listen")
		return err
	}

	subj := discovery.DefaultPublishPrefix + ".>"
	msgCh := make(chan *nats.Msg)

	sub, err := s.nc.Subscribe(subj, func(msg *nats.Msg) {
		msgCh <- msg
	})
	if err != nil {
		return err
	}

	defer func() {
		sub.Unsubscribe()
		sub.Drain()
		close(msgCh)
	}()

	nodes := make(map[string]*NodeItem)

	handleNatsMsg := func(msg *nats.Msg) error {
		var req discovery.Request
		err := util.Unmarshal(msg.Data, &req)
		if err != nil {
			return err
		}
		nid := req.Node.FullID()

		resp := discovery.Response{
			Success: true,
		}
		switch req.Action {
		case discovery.SaveAction:
			if _, ok := nodes[nid]; !ok {
				// accept or reject
				if ok, err := handleNodeAction(req); !ok {
					resp.Success = false
					resp.Reason = fmt.Sprint(err)
					break
				}
				// notify all
				discoverySubj := strings.ReplaceAll(msg.Subject, discovery.DefaultPublishPrefix, discovery.DefaultDiscoveryPrefix)
				s.nc.Publish(discoverySubj, msg.Data)

				nodes[nid] = &NodeItem{
					expire: time.Duration(time.Now().UnixNano()) + s.expire,
					node:   req.Node,
					subj:   msg.Subject,
				}
			}
		case discovery.UpdateAction:
			if node, ok := nodes[nid]; ok {
				// node is in a list
				node.expire = time.Duration(time.Now().UnixNano()) + s.expire
				if ok, err := handleNodeAction(req); !ok {
					resp.Success = false
					resp.Reason = fmt.Sprint(err)
				}

				discoverySubj := strings.ReplaceAll(msg.Subject, discovery.DefaultPublishPrefix, discovery.DefaultDiscoveryPrefix)
				s.nc.Publish(discoverySubj, msg.Data)

			} else {
				req.Action = discovery.SaveAction
				if ok, err := handleNodeAction(req); !ok {
					resp.Success = false
					resp.Reason = fmt.Sprint(err)
					break
				}

				// @todo check if state change
				discoverySubj := strings.ReplaceAll(msg.Subject, discovery.DefaultPublishPrefix, discovery.DefaultDiscoveryPrefix)
				s.nc.Publish(discoverySubj, msg.Data)

				nodes[nid] = &NodeItem{
					expire: time.Duration(time.Now().UnixNano()) + s.expire,
					node:   req.Node,
					subj:   msg.Subject,
				}
			}
		case discovery.DeleteAction:
			if _, ok := nodes[nid]; ok {
				if ok, err := handleNodeAction(req); !ok {
					resp.Success = false
					resp.Reason = fmt.Sprint(err)
					break
				}
				discoverySubj := strings.ReplaceAll(msg.Subject, discovery.DefaultPublishPrefix, discovery.DefaultDiscoveryPrefix)
				s.nc.Publish(discoverySubj, msg.Data)
			}
			delete(nodes, nid)
		default:
			return fmt.Errorf("unkonw message: %v", msg.Data)
		}

		data, err := util.Marshal(&resp)
		if err != nil {
			return err
		}
		s.nc.Publish(msg.Reply, data)
		return nil
	}

	t := time.NewTicker(s.expire / 2)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if err := s.checkExpires(nodes, time.Duration(time.Now().UnixNano()), handleNodeAction); err != nil {
				return err
			}
		case msg, ok := <-msgCh:
			if ok {
				err := handleNatsMsg(msg)
				if err != nil {
					return err
				}
				break
			}
			return io.EOF
		}
	}

}
