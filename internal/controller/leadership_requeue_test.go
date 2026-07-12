package controller

import (
	"testing"
)

// TestRequeueAllBeforeSetup: OnStartedLeading can fire BEFORE SetupWithManager
// on a restarted pod (old holder releases the lease at shutdown). The requeue
// event must be buffered, not silently dropped — a dropped event left every
// node's published status stale until its next Spec change.
func TestRequeueAllBeforeSetup(t *testing.T) {
	r := &TinyNodeReconciler{}

	// leadership won before SetupWithManager — used to hit the nil guard
	r.RequeueAllOnLeadershipChange()

	ch := r.ensureLeadershipCh() // what SetupWithManager wires as the source
	select {
	case <-ch:
		// buffered event survived — controller will requeue all nodes
	default:
		t.Fatal("leadership requeue event was dropped: channel empty")
	}

	// coalescing: multiple wins while unconsumed keep exactly one event
	r.RequeueAllOnLeadershipChange()
	r.RequeueAllOnLeadershipChange()
	select {
	case <-ch:
	default:
		t.Fatal("expected one buffered event after repeat wins")
	}
	select {
	case <-ch:
		t.Fatal("expected coalescing to one event, got a second")
	default:
	}
}
