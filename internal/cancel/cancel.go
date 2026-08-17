// Package cancel links a caller's cancellation boundary to runtime work.
package cancel

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/godexture/godec/internal/errorx"
)

// Normalize returns the trusted cancellation cause when err is only a pure
// propagation of the stopped context. The callback error must reach the
// context cause, Canceled, or DeadlineExceeded through one finite single-error
// unwrap chain. A live context, joined error, malformed chain, or independent
// failure returns nil so callers keep the original failure private.
func Normalize(ctx context.Context, err error) error {
	if ctx == nil || err == nil || ctx.Err() == nil {
		return nil
	}
	cause := context.Cause(ctx)
	if cause == nil {
		cause = ctx.Err()
	}
	if cause == nil {
		return nil
	}
	if errorx.Only(err, cause) ||
		errorx.Only(err, context.Canceled) ||
		errorx.Only(err, context.DeadlineExceeded) {
		return cause
	}
	return nil
}

// Link gives one run its own cancellation boundary while preserving the
// caller's values and deadline. Every cause that crosses the boundary is one
// comparable pointer, so a callback returning context.Cause verbatim remains
// an identity-preserving echo even when the caller supplied a non-comparable
// concrete error.
func Link(source context.Context) (context.Context, context.CancelCauseFunc, func()) {
	if source == nil {
		source = context.Background()
	}
	state, stateCancel := context.WithCancelCause(context.Background())
	linked := &contextLink{source: source, state: state, cancel: stateCancel}
	stopSource := context.AfterFunc(source, linked.fromSource)
	detach := func() { stopSource() }
	if source.Err() != nil {
		linked.fromSource()
	}
	return linked, linked.stop, detach
}

type contextLink struct {
	source    context.Context
	state     context.Context
	cancel    context.CancelCauseFunc
	once      sync.Once
	sourceWon atomic.Bool
}

func (c *contextLink) Deadline() (time.Time, bool) { return c.source.Deadline() }
func (c *contextLink) Done() <-chan struct{}       { return c.state.Done() }

func (c *contextLink) Err() error {
	if err := c.state.Err(); err == nil {
		return nil
	}
	if c.sourceWon.Load() {
		if err := c.source.Err(); err != nil {
			return err
		}
	}
	return c.state.Err()
}

func (c *contextLink) Value(key any) any {
	if value := c.state.Value(key); value != nil {
		return value
	}
	return c.source.Value(key)
}

func (c *contextLink) fromSource() {
	c.end(true, func() error { return context.Cause(c.source) })
}

func (c *contextLink) stop(err error) {
	c.end(false, func() error { return err })
}

func (c *contextLink) end(fromSource bool, cause func() error) {
	c.once.Do(func() {
		value := cause()
		if value == nil {
			value = context.Canceled
		}
		reason := context.Canceled
		if fromSource {
			c.sourceWon.Store(true)
			if err := c.source.Err(); err != nil {
				reason = err
			}
		}
		c.cancel(&carrier{cause: value, reason: reason})
	})
}

// carrier is intentionally private. Its pointer identity is the boundary's
// cancellation identity; its unwrap keeps the caller's trusted cause visible
// to the journal and ordinary error inspection.
type carrier struct {
	cause  error
	reason error
}

func (c *carrier) Error() string {
	if c == nil || c.cause == nil {
		return context.Canceled.Error()
	}
	return c.cause.Error()
}

func (c *carrier) Unwrap() error {
	if c == nil {
		return nil
	}
	return c.cause
}

func (c *carrier) Is(target error) bool {
	if c == nil {
		return false
	}
	return (target == context.Canceled && c.reason == context.Canceled) ||
		(target == context.DeadlineExceeded && c.reason == context.DeadlineExceeded)
}
