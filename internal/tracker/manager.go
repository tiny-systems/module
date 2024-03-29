package tracker

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/jellydator/ttlcache/v3"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/pkg/client"
	"github.com/tiny-systems/module/pkg/utils"
	"time"
)

type Manager interface {
	Track(ctx context.Context, msg PortMsg)
	Register(tracker v1alpha1.TinyTracker) error
	Deregister(name string) error
}

type manager struct {
	log      logr.Logger
	trackers []v1alpha1.TinyTracker
	cache    *ttlcache.Cache[string, struct{}]
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
		cache: ttlcache.New[string, struct{}](
			ttlcache.WithTTL[string, struct{}](time.Second),
		),
	}
}

func (t *manager) Track(ctx context.Context, msg PortMsg) {
	for _, tt := range t.trackers {
		if err := t.sendPortData(ctx, msg, tt); err != nil {
			t.log.Error(err, "port webhook error", "data", msg)
		}
	}
}

func (t *manager) sendPortData(ctx context.Context, msg PortMsg, tracker v1alpha1.TinyTracker) error {
	if tracker.Spec.PortDataWebhook == nil || tracker.Spec.PortDataWebhook.FlowID != msg.FlowID {
		return nil
	}

	if len(msg.Data) > tracker.Spec.PortDataWebhook.MaxDataSize {
		return nil
	}

	t.cache.DeleteExpired()
	if _, created := t.cache.GetOrSet(buildTrackerPortCacheKey(tracker, msg.PortName), struct{}{}, ttlcache.WithTTL[string, struct{}](tracker.Spec.PortDataWebhook.Interval.Duration)); created {
		return nil
	}

	headers := map[string]string{
		client.HeaderFlowID:       msg.FlowID,
		client.HeaderPortFullName: msg.PortName,
		"Content-Type":            "application/octet-stream",
	}
	if msg.Err != nil {
		headers[client.HeaderError] = msg.Err.Error()
	}
	if msg.EdgeID != "" {
		headers[client.HeaderEdgeID] = msg.EdgeID
	}
	return client.SendWebhookData(ctx, tracker.Spec.PortDataWebhook.URL, headers, msg.Data)
}

func (t *manager) Run(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

func buildTrackerPortCacheKey(t v1alpha1.TinyTracker, to string) string {
	return fmt.Sprintf("%s-%s", t.Name, to)
}
