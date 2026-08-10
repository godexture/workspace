// Package planning owns the cancellation boundary shared by every planning
// phase from Access acquisition through graph solving.
package planning

import (
	"context"
	"errors"
	"time"
)

var ErrDuration = errors.New("planning duration budget exhausted")

type contextKey struct{}

// Start creates the one duration boundary for a planning pipeline. A caller
// already inside that boundary reuses it so direct solver use and Host use do
// not start independent clocks.
func Start(parent context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	if active, _ := parent.Value(contextKey{}).(bool); active {
		return parent, func() {}
	}
	if duration <= 0 {
		return context.WithValue(parent, contextKey{}, true), func() {}
	}
	ctx, cancel := context.WithTimeoutCause(parent, duration, ErrDuration)
	return context.WithValue(ctx, contextKey{}, true), cancel
}

func DurationExhausted(ctx context.Context) bool {
	return ctx != nil && errors.Is(context.Cause(ctx), ErrDuration)
}
