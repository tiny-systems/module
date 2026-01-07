package utils

import "context"

type contextKey string

const leaderKey contextKey = "leader"
const leadershipKnownKey contextKey = "leadershipKnown"

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

func WithLeadershipKnown(ctx context.Context, known bool) context.Context {
	return context.WithValue(ctx, leadershipKnownKey, known)
}

// IsLeadershipKnown checks if leadership status has been determined
func IsLeadershipKnown(ctx context.Context) bool {
	val, ok := ctx.Value(leadershipKnownKey).(bool)
	if !ok {
		return false // Default to false if not set
	}
	return val
}
