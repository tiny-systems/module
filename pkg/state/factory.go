package state

import (
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/module"
)

// Factory builds a module.State for a specific node. The scheduler holds
// one Factory and calls For(...) when a runner is created for a Stateful
// component.
//
// Backend selection (metadata, redis, postgres, embedded) lives in the
// concrete Factory. The component never sees the choice.
type Factory interface {
	For(node *v1alpha1.TinyNode, emit EmitFunc) module.State
}

// MetadataFactory builds MetadataState instances. This is the default and
// only backend for now; future backends will be siblings.
type MetadataFactory struct{}

// NewMetadataFactory returns a Factory that produces metadata-backed State.
func NewMetadataFactory() *MetadataFactory {
	return &MetadataFactory{}
}

// For returns a State seeded with the node's current metadata.
func (f *MetadataFactory) For(node *v1alpha1.TinyNode, emit EmitFunc) module.State {
	return NewMetadataState(node.Status.Metadata, emit)
}

var _ Factory = (*MetadataFactory)(nil)
