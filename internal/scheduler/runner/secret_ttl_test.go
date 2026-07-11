package runner

import (
	"testing"
	"time"

	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/tiny-systems/module/api/v1alpha1"
)

func TestSecretResolveTTLLapsed(t *testing.T) {
	r := &Runner{settingsSecretAt: cmap.New[int64]()}
	p := v1alpha1.SettingsPort

	// never recorded → lapsed (don't withhold the first re-delivery)
	if !r.secretResolveTTLLapsed(p) {
		t.Fatal("missing timestamp must count as lapsed")
	}

	// just recorded → within TTL, not lapsed
	r.settingsSecretAt.Set(p, time.Now().UnixNano())
	if r.secretResolveTTLLapsed(p) {
		t.Fatal("fresh timestamp must not be lapsed")
	}

	// recorded > TTL ago → lapsed
	r.settingsSecretAt.Set(p, time.Now().Add(-secretResolveTTL-time.Second).UnixNano())
	if !r.secretResolveTTLLapsed(p) {
		t.Fatal("timestamp older than TTL must be lapsed")
	}
}
