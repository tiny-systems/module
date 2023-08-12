package scheduler

import (
	"github.com/tiny-systems/module/api/v1alpha1"
)

type instanceRequest struct {
	Node     v1alpha1.TinyNode
	StatusCh chan v1alpha1.TinyNodeStatus
}
