package scheduler

import (
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/module"
)

type instanceRequest struct {
	Name      string
	Spec      v1alpha1.TinyNodeSpec
	Component module.Component
	StatusCh  chan v1alpha1.TinyNodeStatus
}
