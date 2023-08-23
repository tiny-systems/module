package tracker

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/internal/scheduler/runner"
	"github.com/tiny-systems/module/pkg/utils"
	"time"
)

const (
	cleanupDuration = time.Second
)

type Manager interface {
	Track(msg *runner.Msg, err error)
	Register(tracker v1alpha1.TinyTracker) error
	Deregister(name string) error
}

type manager struct {
	log      logr.Logger
	trackers []v1alpha1.TinyTracker
	cache    cmap.ConcurrentMap[string, time.Time]
}

func (t *manager) SetLogger(l logr.Logger) *manager {
	t.log = l
	return t
}

func (t *manager) Register(tt v1alpha1.TinyTracker) error {
	t.trackers = append(t.trackers, tt)
	return nil
}

func (t *manager) Deregister(name string) error {
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
		cache:    cmap.New[time.Time](),
	}
}

func (t *manager) Track(msg *runner.Msg, err error) {
	if msg == nil {
		return
	}
	for _, tt := range t.trackers {
		if tt.Spec.Webhook == nil {
			continue
		}
		if tt.Spec.FlowSubject == nil {
			continue
		}
		if tt.Spec.FlowSubject.FlowID != msg.Meta[runner.MetaFlowID] {
			t.log.Info("skipping cause is not subscribed for flow", "tracker", tt.Name)
			continue
		}

		cacheKey := buildTrackerPortCacheKey(tt, msg.To)
		if t.cache.Has(cacheKey) {
			t.log.Info("already sent", "tracker", tt.Name, "port", msg.To)
			continue
		}

		t.log.Info("send request to", "url", tt.Spec.Webhook.URL, "port", msg.To, "data", msg.Data)
		t.cache.Set(cacheKey, time.Now().Add(tt.Spec.Webhook.Interval.Duration))
	}
}

func (t *manager) Run(ctx context.Context) error {
	ticker := time.NewTicker(cleanupDuration)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			now := time.Now()
			for k, v := range t.cache.Items() {
				if now.After(v) {
					t.log.Info("cleanup", "key", k)
					t.cache.Remove(k)
				}
			}
		case <-ctx.Done():
			return nil
		}
	}
}

func buildTrackerPortCacheKey(t v1alpha1.TinyTracker, to string) string {
	return fmt.Sprintf("%s-%s", t.Name, to)
}
