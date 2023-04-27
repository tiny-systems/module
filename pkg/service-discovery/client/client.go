package client

import (
	"context"
	"fmt"
	"github.com/nats-io/nats.go"
	"github.com/tiny-systems/module/pkg/service-discovery/discovery"
	"github.com/tiny-systems/module/pkg/service-discovery/util"
	"time"
)

type NodeStateChangeCallback func(req discovery.Request)

type Client struct {
	nc        *nats.Conn
	liveCycle time.Duration
}

// NewClient create a client instance
func NewClient(nc *nats.Conn, liveCycle time.Duration) (*Client, error) {

	c := &Client{
		nc:        nc,
		liveCycle: liveCycle,
	}

	if c.liveCycle <= 0 {
		c.liveCycle = discovery.DefaultExpire
	}

	return c, nil
}

func (c *Client) handleNatsMsg(msg *nats.Msg, callback NodeStateChangeCallback) error {
	var event discovery.Request
	err := util.Unmarshal(msg.Data, &event)
	if err != nil {
		return err
	}

	switch event.Action {
	case discovery.SaveAction, discovery.UpdateAction, discovery.DeleteAction:
		callback(event)
	default:
		err = fmt.Errorf("unkonw message: %v", msg.Data)
		return err
	}
	return nil
}

func (c *Client) KeepAlive(ctx context.Context, getNode func() discovery.Node) error {
	t := time.NewTicker(c.liveCycle)

	defer func() {
		_ = c.sendAction(getNode, discovery.DeleteAction)
		t.Stop()
	}()
	_ = c.sendAction(getNode, discovery.SaveAction)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			_ = c.sendAction(getNode, discovery.UpdateAction)
		}
	}
}

func (c *Client) sendAction(getNode func() discovery.Node, action discovery.Action) error {
	node := getNode()
	data, err := util.Marshal(&discovery.Request{
		Action: action, Node: node,
	})
	if err != nil {
		return err
	}
	subj := discovery.DefaultPublishPrefix + "." + node.FullID()
	msg, err := c.nc.Request(subj, data, time.Second*15)
	if err != nil {
		return nil
	}

	var resp discovery.Response
	err = util.Unmarshal(msg.Data, &resp)
	if err != nil {
		return err
	}

	if !resp.Success {
		err := fmt.Errorf("[%v] response error %v", action, resp.Reason)
		return err
	}
	return nil
}

type Discovery interface {
	KeepAlive(ctx context.Context, getNode func() discovery.Node) error
}
