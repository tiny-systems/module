package discovery

import (
	"github.com/tiny-systems/module/pkg/api/module-go"
	"google.golang.org/protobuf/types/known/structpb"
	"time"
)

// NodeState define the node state type
type NodeState int32

const (
	DefaultPublishPrefix   = "node.publish"
	DefaultDiscoveryPrefix = "node.discovery"

	DefaultLivecycle = 2 * time.Second
	DefaultExpire    = 5 * time.Second
)

type Action string

const (
	SaveAction   Action = "save"
	UpdateAction Action = "update"
	DeleteAction Action = "delete"
)

// Node represents a node info
type Node struct {
	ID string
	//
	Component *module.Component //optional

	Module *module.ModuleVersion
	// How node looks like
	Graph *structpb.Struct
	// Realtime stats
	Stats       *structpb.Struct
	WorkspaceID string
	// which server is running the node
	ServerID string

	FlowID *string // optional
}

// FullID return the node id with scheme prefix
func (n *Node) FullID() string {
	if n.Component != nil {
		return n.WorkspaceID + "." + n.Component.Name + "." + n.ID
	}
	return n.WorkspaceID + "." + n.ID
}

type Request struct {
	Action Action
	Node   Node
}

type Response struct {
	Success bool
	Reason  string
}
