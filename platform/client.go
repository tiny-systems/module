package platform

import (
	"context"
	"fmt"
	"github.com/nats-io/nats.go"
	tinyserver "github.com/tiny-systems/module/pkg/api/module-go"
	"github.com/tiny-systems/module/pkg/module"
	"google.golang.org/protobuf/proto"
	"strings"
)

type Client struct {
	nc *nats.Conn
}

func NewClient(nc *nats.Conn) *Client {
	return &Client{nc: nc}
}

func (c *Client) GetManifest(ctx context.Context, req *tinyserver.GetManifestRequest) (*tinyserver.GetManifestResponse, error) {
	data, err := proto.Marshal(req)
	if err != nil {
		return nil, err
	}
	msg, err := c.nc.RequestWithContext(ctx, getSubject(module.GetManifestSubject), data)
	if err != nil {
		return nil, err
	}
	resp := &tinyserver.GetManifestResponse{}
	err = proto.Unmarshal(msg.Data, resp)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf(resp.Error)
	}
	return resp, err
}

func (c *Client) PublishModule(ctx context.Context, req *tinyserver.PublishModuleRequest) (*tinyserver.PublishModuleResponse, error) {
	data, err := proto.Marshal(req)
	if err != nil {
		return nil, err
	}
	msg, err := c.nc.RequestWithContext(ctx, getSubject(module.PublishModuleSubject), data)
	if err != nil {
		return nil, err
	}
	resp := &tinyserver.PublishModuleResponse{}
	err = proto.Unmarshal(msg.Data, resp)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf(resp.Error)
	}
	return resp, err
}

func (c *Client) UpdateModuleVersion(ctx context.Context, req *tinyserver.UpdateModuleVersionRequest) (*tinyserver.UpdateModuleVersionResponse, error) {
	data, err := proto.Marshal(req)
	if err != nil {
		return nil, err
	}
	msg, err := c.nc.RequestWithContext(ctx, getSubject(module.UpdateModuleSubject), data)
	if err != nil {
		return nil, err
	}
	resp := &tinyserver.UpdateModuleVersionResponse{}
	err = proto.Unmarshal(msg.Data, resp)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf(resp.Error)
	}
	return resp, err
}

func getSubject(cmd string) string {
	return strings.ReplaceAll(module.PlatformSubjectPattern, "*", cmd)
}
