package utils

import "context"

type contextKey string

const (
	leaderKey     contextKey = "leader"
	sourceNodeKey contextKey = "sourceNode"
)

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
