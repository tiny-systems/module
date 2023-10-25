package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/jellydator/ttlcache/v3"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/pkg/client"
	"github.com/tiny-systems/module/pkg/utils"
	"time"
)

type Manager interface {
	Track(msg PortMsg)
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

func (t *manager) Track(msg PortMsg) {

	for _, tt := range t.trackers {
		if err := t.sendPortData(msg, tt); err != nil {
			t.log.Error(err, "port webhook error")
		}
		if err := t.sendNodeStatistics(msg, tt); err != nil {
			t.log.Error(err, "stats webhook error")
		}
	}
}

func (t *manager) sendPortData(msg PortMsg, tracker v1alpha1.TinyTracker) error {

	if tracker.Spec.PortDataWebhook == nil {
		return nil
	}

	if tracker.Spec.PortDataWebhook.FlowID != msg.FlowID {
		return nil
	}
	if len(msg.Data) > tracker.Spec.PortDataWebhook.MaxDataSize {
		return nil
	}

	cacheKey := buildTrackerPortCacheKey(tracker, msg.PortName)
	t.cache.DeleteExpired()
	_, created := t.cache.GetOrSet(cacheKey, struct{}{}, ttlcache.WithTTL[string, struct{}](tracker.Spec.PortDataWebhook.Interval.Duration))

	if created {
		t.log.Info("skip port data")
		return nil
	}

	headers := map[string]string{
		client.HeaderFlowID:       msg.FlowID,
		client.HeaderPortFullName: msg.PortName,
		client.HeaderEdgeID:       msg.EdgeID,
		"Content-Type":            "application/octet-stream",
	}
	if msg.Err != nil {
		headers[client.HeaderError] = msg.Err.Error()
	}
	t.log.Info("send webhook port data", "headers", headers)
	return client.SendWebhookData(tracker.Spec.PortDataWebhook.URL, headers, msg.Data)
}

func (t *manager) sendNodeStatistics(msg PortMsg, tracker v1alpha1.TinyTracker) error {

	if tracker.Spec.NodeStatisticsWebhook == nil {
		return nil
	}

	if tracker.Spec.NodeStatisticsWebhook.FlowID != msg.FlowID {
		return nil
	}

	t.cache.DeleteExpired()
	_, created := t.cache.GetOrSet(buildTrackerPortCacheKey(tracker, msg.NodeName), struct{}{}, ttlcache.WithTTL[string, struct{}](tracker.Spec.NodeStatisticsWebhook.Interval.Duration))

	if created {
		t.log.Info("skip stats")
		return nil
	}

	data, err := json.Marshal(msg.NodeStats)
	if err != nil {
		return err
	}

	headers := map[string]string{
		client.HeaderFlowID:       msg.FlowID,
		client.HeaderPortFullName: msg.PortName,
		client.HeaderEdgeID:       msg.EdgeID,
		client.HeaderNodeName:     msg.NodeName,
		"Content-Type":            "application/json",
	}
	t.log.Info("send webhook statistics data", "headers", headers, "data", data)
	return client.SendWebhookData(tracker.Spec.NodeStatisticsWebhook.URL, headers, data)
}

func (t *manager) Run(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

func buildTrackerPortCacheKey(t v1alpha1.TinyTracker, to string) string {
	return fmt.Sprintf("%s-%s", t.Name, to)
}
