package scheduler

// The run reconciler is the level-triggered safety net for durable execution
// (Phase 2c). Redelivery (AckWait) and the step ledger handle most failures,
// but one gap remains event-driven-only: a pod that dies AFTER acking its
// input and writing the step record but BEFORE its emitted hop is consumed
// leaves nothing in flight to redeliver — the run silently stalls. Same
// lesson as http_server's periodicReassert: drift heals only if something
// periodically drives actual state toward intended state.
//
// Each pass reads the step ledger (execution-scope KV), reconstructs every
// run's frontier — hops that were durably emitted but never got their own
// completion record — and re-publishes frontier hops for runs with no
// progress past the staleness threshold. Re-publishing is safe at every
// overlap: within the broker's duplicate window the publish dedupes on
// Nats-Msg-Id; beyond it, a duplicate delivery short-circuits on the step
// ledger. The one caveat is a step still legitimately RUNNING longer than
// staleAfter — its re-drive would double-execute, so staleAfter must exceed
// the worst-case step duration (transport caps requests at 5m; default 6m).

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-json"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/tiny-systems/module/internal/scheduler/runner"
)

const (
	// defaultReconcileInterval is how often a pass runs.
	defaultReconcileInterval = time.Minute
	// defaultStaleAfter — no progress for this long ⇒ re-drive the frontier.
	// Must exceed the worst-case single-step duration (see caveat above).
	defaultStaleAfter = 6 * time.Minute
	// defaultGCAfter — completed runs older than this get their ledger
	// records deleted, bounding KV growth.
	defaultGCAfter = 24 * time.Hour

	// maxRedrives caps how many times a frontier hop is re-driven before the
	// run is marked failed. Without a cap, a hop whose target can never
	// complete it — the canonical case is a durable run routed into a module
	// on a pre-v0.11 SDK, which processes the hop but writes no step record —
	// would be re-driven every pass forever, repeating side effects.
	maxRedrives = 3

	// redrivePrefix namespaces re-drive counters inside a run's exec scope,
	// beside the step/ ledger records: exec/<runID>/redrive/<stepKey>.
	redrivePrefix = "redrive/"
)

// SetExecKV wires the execution-scope KV bucket (the step ledger) so the run
// reconciler can read it. Without it, StartRunReconciler is a no-op.
func (s *Schedule) SetExecKV(kv jetstream.KeyValue) *Schedule {
	s.execKV = kv
	return s
}

// SetLeaderCheck gates reconciler passes to the leader pod, so N replicas
// don't all re-drive the same runs. Optional — nil means every pod runs
// passes (safe but redundant: re-drives dedupe/skip as described above).
func (s *Schedule) SetLeaderCheck(f func() bool) *Schedule {
	s.leaderCheck = f
	return s
}

// StartRunReconciler runs reconcile passes until ctx is done. Call in a
// goroutine after the scheduler is wired.
func (s *Schedule) StartRunReconciler(ctx context.Context) {
	if s.execKV == nil {
		return
	}
	interval := s.reconcileEvery
	if interval == 0 {
		interval = defaultReconcileInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.leaderCheck != nil && !s.leaderCheck() {
				continue
			}
			redriven, err := s.reconcileRuns(ctx)
			if err != nil {
				s.log.Error(err, "run reconciler: pass failed")
				continue
			}
			if len(redriven) > 0 {
				s.log.Info("run reconciler: re-drove stalled hops", "count", len(redriven))
			}
		}
	}
}

// runLedger is one run's parsed ledger state.
type runLedger struct {
	records map[string]runner.StepRecord // stepKey -> record
	keys    []string                     // raw KV keys, for GC
	latest  time.Time
}

// reconcileRuns performs one pass: parse the ledger, compute each run's
// frontier, re-publish stale frontier hops, GC completed runs. Returns the
// re-driven hops (for tests and logging).
func (s *Schedule) reconcileRuns(ctx context.Context) ([]runner.EmitRecord, error) {
	runs, err := s.readLedger(ctx)
	if err != nil {
		return nil, err
	}

	staleAfter := s.staleAfter
	if staleAfter == 0 {
		staleAfter = defaultStaleAfter
	}
	gcAfter := s.gcAfter
	if gcAfter == 0 {
		gcAfter = defaultGCAfter
	}

	var redriven []runner.EmitRecord
	for runID, ledger := range runs {
		frontier := frontierOf(ledger)

		if len(frontier) == 0 {
			// Run finished (every emitted hop has a record). GC old ones.
			if time.Since(ledger.latest) > gcAfter {
				for _, k := range ledger.keys {
					_ = s.execKV.Delete(ctx, k)
				}
			}
			continue
		}

		if time.Since(ledger.latest) < staleAfter {
			continue // recent progress — hops are probably in flight
		}

		for _, hop := range frontier {
			attempts := s.redriveAttempts(ctx, runID, hop.StepKey)
			if attempts >= maxRedrives {
				// The hop keeps not completing — most likely its target can't
				// participate (pre-v0.11 SDK writes no step records). Mark the
				// step failed so the run leaves the frontier: run_status shows
				// failed instead of the run silently re-executing forever.
				s.failStep(ctx, runID, hop, attempts)
				continue
			}

			msg := &runner.Msg{
				To:      hop.To,
				From:    hop.From,
				EdgeID:  hop.EdgeID,
				Data:    hop.Data,
				RunID:   runID,
				StepKey: hop.StepKey,
			}
			if _, err := s.msgHandler(ctx, msg); err != nil {
				s.log.Error(err, "run reconciler: re-drive publish failed",
					"runID", runID, "stepKey", hop.StepKey)
				continue
			}
			s.recordRedrive(ctx, runID, hop.StepKey, attempts+1)
			s.log.Info("run reconciler: re-drove stalled hop",
				"runID", runID, "stepKey", hop.StepKey, "to", hop.To,
				"attempt", attempts+1, "of", maxRedrives)
			redriven = append(redriven, hop)
		}
	}
	return redriven, nil
}

// redriveKey returns the raw exec-KV key of a hop's re-drive counter.
func redriveKey(runID, stepKey string) string {
	return "exec/" + runID + "/" + redrivePrefix + stepKey
}

// redriveAttempts reads how many times a hop has been re-driven.
func (s *Schedule) redriveAttempts(ctx context.Context, runID, stepKey string) int {
	entry, err := s.execKV.Get(ctx, redriveKey(runID, stepKey))
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(string(entry.Value()))
	if err != nil {
		return 0
	}
	return n
}

// recordRedrive persists the hop's re-drive counter. Best-effort — a lost
// write only means one extra re-drive.
func (s *Schedule) recordRedrive(ctx context.Context, runID, stepKey string, attempts int) {
	if _, err := s.execKV.Put(ctx, redriveKey(runID, stepKey), []byte(strconv.Itoa(attempts))); err != nil {
		s.log.Error(err, "run reconciler: record re-drive failed",
			"runID", runID, "stepKey", stepKey)
	}
}

// failStep writes a terminal failed record for a hop that exhausted its
// re-drives, removing it from the frontier and surfacing the failure in
// run_status instead of looping side effects forever.
func (s *Schedule) failStep(ctx context.Context, runID string, hop runner.EmitRecord, attempts int) {
	rec := runner.StepRecord{
		Node:   hop.To,
		Status: runner.StepStatusFailed,
		Error: fmt.Sprintf(
			"re-drive limit reached after %d attempts — hop never completed (is the target module on an SDK >= v0.11 with durable support?)",
			attempts),
		CompletedAt: time.Now(),
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return
	}
	key := "exec/" + runID + "/step/" + hop.StepKey
	if _, err := s.execKV.Put(ctx, key, b); err != nil {
		s.log.Error(err, "run reconciler: fail-step write failed",
			"runID", runID, "stepKey", hop.StepKey)
		return
	}
	s.log.Info("run reconciler: hop exhausted re-drives, run marked failed",
		"runID", runID, "stepKey", hop.StepKey, "to", hop.To)
}

// readLedger scans the exec KV and groups step records by run.
// Key layout (written by JetStreamState): exec/<runID>/step/<stepKey>.
func (s *Schedule) readLedger(ctx context.Context) (map[string]*runLedger, error) {
	keys, err := s.execKV.Keys(ctx)
	if errors.Is(err, jetstream.ErrNoKeysFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	runs := map[string]*runLedger{}
	for _, k := range keys {
		parts := strings.SplitN(k, "/", 4)
		if len(parts) != 4 || parts[0] != "exec" {
			continue
		}
		runID, stepKey := parts[1], parts[3]

		// Re-drive counters are per-run debris: track them for GC but they
		// carry no ledger record.
		if parts[2] == "redrive" {
			ledger := runs[runID]
			if ledger == nil {
				ledger = &runLedger{records: map[string]runner.StepRecord{}}
				runs[runID] = ledger
			}
			ledger.keys = append(ledger.keys, k)
			continue
		}
		if parts[2] != "step" {
			continue
		}

		entry, err := s.execKV.Get(ctx, k)
		if err != nil {
			continue // deleted between Keys and Get — fine
		}
		var rec runner.StepRecord
		if err := json.Unmarshal(entry.Value(), &rec); err != nil {
			continue // unreadable record: skip rather than poison the pass
		}

		ledger := runs[runID]
		if ledger == nil {
			ledger = &runLedger{records: map[string]runner.StepRecord{}}
			runs[runID] = ledger
		}
		ledger.records[stepKey] = rec
		ledger.keys = append(ledger.keys, k)
		if rec.CompletedAt.After(ledger.latest) {
			ledger.latest = rec.CompletedAt
		}
	}
	return runs, nil
}

// frontierOf returns the hops that were durably emitted by completed steps
// but never produced their own completion record — the run's outstanding
// work. Failed steps count as seen (terminal): the durability layer never
// re-runs business errors.
func frontierOf(l *runLedger) []runner.EmitRecord {
	var frontier []runner.EmitRecord
	for _, rec := range l.records {
		for _, e := range rec.Emits {
			if _, seen := l.records[e.StepKey]; !seen {
				frontier = append(frontier, e)
			}
		}
	}
	return frontier
}
