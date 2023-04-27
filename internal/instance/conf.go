package instance

import (
	"sync"
)

type MapPort struct {
	From   string
	To     string
	EdgeID string
}

type PortConfig struct {
	FromNode      string
	Configuration []byte
	Schema        []byte
}

type Configuration struct {
	sync.RWMutex
	ID string // uuid
	// for nats subject
	FlowID string
	//Label  string
	//Name   string
	// where send message next
	DestinationMap map[string][]MapPort
	// fromNode => string => PortConfig
	PortConfigMap map[string]map[string]PortConfig
	Revision      int64
	// asked to be running
	Run         bool
	ComponentID string
	Data        map[string]interface{}
}

func (c *Configuration) ShouldRun() bool {
	c.Lock()
	defer c.Unlock()
	return c.Run
}

func (c *Configuration) GetPortConfig(portName string, from *string) *PortConfig {
	var fromStr = "" // no from
	if from != nil {
		fromStr = *from
	}
	if conf, ok := c.PortConfigMap[fromStr]; ok {
		if confPort, ok := conf[portName]; ok {
			return &confPort
		}
	}
	// if not found specific to the sender settings - use node's own
	if conf, ok := c.PortConfigMap[""]; ok {
		if confPort, ok := conf[portName]; ok {
			return &confPort
		}
	}
	return nil
}
