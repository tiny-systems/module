package utils

import "context"

type contextKey string

const (
	leaderKey           contextKey = "leader"
	sourceNodeKey       contextKey = "sourceNode"
	durableDeliveryKey  contextKey = "durableDelivery"
)

// WithDurableDelivery marks the context as a TinySignalReconciler-driven
// delivery of a previously-persisted durable-port signal. Scheduler.Handle
// uses this to skip the durable-port persist step and instead route the
// message through normal MsgHandler dispatch (so edge config evaluation
// runs at delivery time, mapping the source's raw output into the target
// port's expected shape).
func WithDurableDelivery(ctx context.Context) context.Context {
	return context.WithValue(ctx, durableDeliveryKey, true)
}

// IsDurableDelivery reports whether the current call is the reconciler
// delivering an already-persisted durable signal.
func IsDurableDelivery(ctx context.Context) bool {
	v, _ := ctx.Value(durableDeliveryKey).(bool)
	return v
}

func WithLeader(ctx context.Context, isLeader bool) context.Context {
	return context.WithValue(ctx, leaderKey, isLeader)
}

// IsLeader checks if the current context indicates leadership status
func IsLeader(ctx context.Context) bool {
	val, ok := ctx.Value(leaderKey).(bool)
	if !ok {
		return false // Default to false if not set or wrong type
	}
	return val
}

// WithSourceNode adds the source node name to context
func WithSourceNode(ctx context.Context, nodeName string) context.Context {
	return context.WithValue(ctx, sourceNodeKey, nodeName)
}

// GetSourceNode returns the source node name from context
func GetSourceNode(ctx context.Context) string {
	val, ok := ctx.Value(sourceNodeKey).(string)
	if !ok {
		return ""
	}
	return val
}
