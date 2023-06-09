package module

import (
	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
	"github.com/tiny-systems/module/internal/instance"
	"github.com/tiny-systems/module/pkg/api/module-go"
	"github.com/tiny-systems/module/pkg/discovery"
	m "github.com/tiny-systems/module/pkg/module"
	"github.com/tiny-systems/module/pkg/utils"
	"google.golang.org/protobuf/types/known/structpb"
	"sync"
)

type installComponentMsg struct {
	id        string
	component m.Component
	module    m.Info
	data      map[string]interface{}
}

// GetDiscoveryNode based on a installing compontent intention
func (i *installComponentMsg) GetDiscoveryNode() (*module.DiscoveryNode, error) {
	var node = &module.DiscoveryNode{
		ID: i.id,
	}
	graphNode, err := instance.NewApiNode(i.component, nil)
	if err != nil {
		return nil, err
	}

	cmpApi, err := utils.GetComponentApi(i.component)
	if err != nil {
		return nil, err
	}
	cmpApi.Name = i.id
	//cmpApi.Version = install.module.Version
	node.Component = cmpApi

	i.data["label"] = cmpApi.Description
	nodeMap := utils.NodeToMap(graphNode, i.data)

	node.Graph, err = structpb.NewStruct(nodeMap)
	if err != nil {
		return nil, err
	}

	modApi, err := utils.GetModuleApi(i.module)
	if err != nil {
		return node, err
	}
	node.Module = modApi
	return node, nil
}

type Server struct {
	log      zerolog.Logger
	nats     *nats.Conn
	registry *discovery.Registry
	//discovery    client.Discovery
	runnerConfig *module.RunnerConfig

	installComponentsCh    chan *installComponentMsg // dev only purposes
	newInstanceCh          chan *instance.Msg
	installedComponentsMap map[string]*installComponentMsg

	errorCh chan error
	// to communicate with nodes directly
	communicationCh     map[string]chan *instance.Msg
	communicationChLock sync.RWMutex
}

func New(config *module.RunnerConfig, errChan chan error) *Server {
	return &Server{
		runnerConfig: config,
		//
		installComponentsCh: make(chan *installComponentMsg),
		//
		installedComponentsMap: make(map[string]*installComponentMsg),
		//
		newInstanceCh: make(chan *instance.Msg),
		errorCh:       errChan,
		//
		communicationCh:     make(map[string]chan *instance.Msg),
		communicationChLock: sync.RWMutex{},
	}
}

func (s *Server) SetLogger(l zerolog.Logger) {
	s.log = l
}

func (s *Server) SetNats(n *nats.Conn) {
	s.nats = n
}

// SetRegistry @todo replace with interface
func (s *Server) SetRegistry(r *discovery.Registry) {
	s.registry = r
}

func (s *Server) getDiscoveryNode() *module.DiscoveryNode {
	return &module.DiscoveryNode{
		ServerID:    s.runnerConfig.ServerID,
		WorkspaceID: s.runnerConfig.WorkspaceID,
	}
}
