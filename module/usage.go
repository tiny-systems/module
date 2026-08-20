package module

import (
	"context"
	"sync"
)

// Metered work.
//
// Some hops cost money — an LLM call, a paid API, egress. The amounts were
// always visible in the message payload and nowhere else, so answering "what
// did this run cost" meant a reader parsing one module's response shape, which
// is exactly the coupling the SDK exists to prevent.
//
// A component reports amounts in units it names itself:
//
//	module.Meter(ctx, "input_tokens", float64(resp.Usage.Input))
//	module.Meter(ctx, "output_tokens", float64(resp.Usage.Output))
//
// The runtime records them on the hop's span and nothing between here and the
// dashboard interprets them: the units are opaque strings, summed per unit. A
// component that meters "usd_micros", "credits" or "rows_scanned" is served by
// the same path, and a reader that has never heard of an LLM can still total
// the run.
//
// Report what the provider actually charged for, not an estimate. A number
// that looks authoritative and was guessed is worse than no number.

// UsageAttrPrefix namespaces a metered unit where the runtime records it — a
// span attribute today. It is part of the contract rather than an internal
// detail because the reader on the other side has to strip it, and that reader
// lives in a different repository.
const UsageAttrPrefix = "tiny.usage."

// UsageUnit names what is being counted. It is the component's choice and is
// never interpreted by the runtime.
type UsageUnit = string

// usageSink accumulates one hop's metered amounts. The runner installs it
// before calling a handler and reads it back afterwards.
type usageSink struct {
	mu     sync.Mutex
	counts map[UsageUnit]float64
}

func (s *usageSink) add(unit UsageUnit, amount float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.counts == nil {
		s.counts = make(map[UsageUnit]float64, 4)
	}
	s.counts[unit] += amount
}

// total returns a copy, so a caller reading a hop's usage cannot be tripped by
// a background goroutine still metering against the same context.
func (s *usageSink) total() map[UsageUnit]float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.counts) == 0 {
		return nil
	}
	out := make(map[UsageUnit]float64, len(s.counts))
	for unit, amount := range s.counts {
		out[unit] = amount
	}
	return out
}

type usageKey struct{}

// WithUsage returns a context that accumulates metered amounts, and the sink to
// read them from. The runtime calls this; a component only calls Meter.
func WithUsage(ctx context.Context) (context.Context, UsageReader) {
	sink := &usageSink{}
	return context.WithValue(ctx, usageKey{}, sink), sink
}

// UsageReader hands back what was metered against a context.
type UsageReader interface {
	Total() map[UsageUnit]float64
}

func (s *usageSink) Total() map[UsageUnit]float64 { return s.total() }

// Meter records that this hop consumed amount of unit.
//
// Calling it outside a metered context — a unit test, a direct Handle call — is
// a no-op, so a component never has to ask whether metering is available.
// A zero or negative amount is ignored: a provider that reported nothing must
// not look like a hop that cost nothing measured.
func Meter(ctx context.Context, unit UsageUnit, amount float64) {
	if unit == "" || amount <= 0 {
		return
	}
	sink, ok := ctx.Value(usageKey{}).(*usageSink)
	if !ok {
		return
	}
	sink.add(unit, amount)
}
