package state

import (
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/module"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

// MetadataFactory builds MetadataState instances backed by the
// controller-runtime cache. The cache is populated by the TinyNode
// watch already established by the controller, so reads are local and
// stay consistent across all replicas of a module.
type MetadataFactory struct {
	reader client.Reader
}

// NewMetadataFactory returns a Factory that produces metadata-backed State
// reading from the supplied K8s cache reader. Pass the same client used
// by the controller (mgr.GetClient() or equivalent) so the factory shares
// the controller's informer cache.
func NewMetadataFactory(reader client.Reader) *MetadataFactory {
	return &MetadataFactory{reader: reader}
}

// For returns a State scoped to the given node. The reader is shared
// across all states; the nodeKey scopes reads/writes to this node only.
func (f *MetadataFactory) For(node *v1alpha1.TinyNode, emit EmitFunc) module.State {
	return NewMetadataState(f.reader, types.NamespacedName{
		Name:      node.Name,
		Namespace: node.Namespace,
	}, emit)
}

var _ Factory = (*MetadataFactory)(nil)
