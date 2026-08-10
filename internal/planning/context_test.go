package planning

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStartSharesOneDurationBoundary(t *testing.T) {
	ctx, cancel := Start(context.Background(), time.Millisecond)
	defer cancel()
	nested, nestedCancel := Start(ctx, time.Hour)
	defer nestedCancel()
	if nested != ctx {
		t.Fatal("nested planning started a second context")
	}
	<-nested.Done()
	if !DurationExhausted(nested) || !errors.Is(context.Cause(nested), ErrDuration) {
		t.Fatalf("cause = %v", context.Cause(nested))
	}
}

func TestStartPreservesExternalCancellation(t *testing.T) {
	parent, stop := context.WithCancel(context.Background())
	ctx, cancel := Start(parent, time.Hour)
	defer cancel()
	stop()
	<-ctx.Done()
	if DurationExhausted(ctx) || !errors.Is(context.Cause(ctx), context.Canceled) {
		t.Fatalf("cause = %v", context.Cause(ctx))
	}
}
