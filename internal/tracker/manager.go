package tracker

import (
	"context"
	"github.com/go-logr/logr"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/pkg/utils"
	"sync"
)

type Manager interface {
	Active(flowID string) bool
	Register(tracker v1alpha1.TinyTracker) error
	Deregister(name string) error
}

type manager struct {
	log      logr.Logger
	trackers []v1alpha1.TinyTracker
	lock     *sync.RWMutex
}

func (t *manager) SetLogger(l logr.Logger) *manager {
	t.log = l
	return t
}

func (t *manager) Register(tt v1alpha1.TinyTracker) error {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.trackers = append(t.trackers, tt)
	return nil
}

func (t *manager) Deregister(name string) error {
	t.lock.Lock()
	defer t.lock.Unlock()

	for k, v := range t.trackers {
		if name == v.Name {
			t.trackers = utils.RemoveSliceElement(t.trackers, k)
			break
		}
	}
	return nil
}

func NewManager() *manager {
	return &manager{
		trackers: make([]v1alpha1.TinyTracker, 0),
		lock:     &sync.RWMutex{},
	}
}

func (t *manager) Active(projectID string) bool {
	t.lock.RLock()
	defer t.lock.RUnlock()
	for _, tt := range t.trackers {
		if tt.Labels[v1alpha1.ProjectIDLabel] == projectID {
			return true
		}
	}
	return false
}

func (t *manager) Run(ctx context.Context) error {
	<-ctx.Done()
	return nil
}
