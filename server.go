package module

import (
	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
	"github.com/tiny-systems/module/internal/instance"
	"github.com/tiny-systems/module/pkg/api/module-go"
	m "github.com/tiny-systems/module/pkg/module"
	"github.com/tiny-systems/module/pkg/service-discovery/client"
	"sync"
)

type installComponentMsg struct {
	id        string
	component m.Component
	module    m.Info
	data      map[string]interface{}
}

type Server struct {
	log          zerolog.Logger
	nats         *nats.Conn
	discovery    client.Discovery
	runnerConfig *module.RunnerConfig

	installComponentsCh    chan *installComponentMsg // dev only purposes
	newInstanceCh          chan *instance.Msg
	installedComponentsMap map[string]*installComponentMsg

	errorCh             chan error
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

func (s *Server) SetDiscovery(d client.Discovery) {
	s.discovery = d
}

func (s *Server) SetNats(n *nats.Conn) {
	s.nats = n
}
