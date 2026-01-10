package utils

import "context"

type contextKey string

const leaderKey contextKey = "leader"

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
