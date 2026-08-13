package observe

import (
	"context"
	"errors"
	"runtime/debug"

	"github.com/godexture/godec/diagnostic"
)

type SinkPanicError struct {
	Value any
	Stack []byte
}

func (e *SinkPanicError) Error() string {
	return "observation sink panicked: " + diagnostic.Recovered(e.Value)
}
func (e *SinkPanicError) StackTrace() []byte {
	return append([]byte(nil), e.Stack...)
}

func (c *Collector) dispatch(sink Sink) {
	defer close(c.done)
	for {
		select {
		case <-c.ctx.Done():
			c.dropPending()
			return
		case event, ok := <-c.delivery:
			if !ok {
				return
			}
			if err := invokeSink(c.ctx, sink, event); err != nil {
				if c.ctx.Err() == nil || !cancellationEcho(err, c.ctx) {
					c.setFailure(err)
				}
				c.dropPending()
				return
			}
		}
	}
}

func (c *Collector) setFailure(err error) {
	if err == nil {
		return
	}
	c.failureOnce.Do(func() {
		c.mu.Lock()
		c.err = err
		c.deliveryFailed = true
		c.mu.Unlock()
		if c.fail != nil {
			c.fail(err)
		}
	})
}

func (c *Collector) dropPending() {
	c.mu.Lock()
	c.deliveryFailed = true
	for {
		select {
		case _, ok := <-c.delivery:
			if !ok {
				c.mu.Unlock()
				return
			}
			c.deliveryDropped++
		default:
			c.mu.Unlock()
			return
		}
	}
}

func invokeSink(ctx context.Context, sink Sink, event Event) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = &SinkPanicError{Value: recovered, Stack: append([]byte(nil), debug.Stack()...)}
		}
	}()
	return sink(ctx, event.clone())
}

func cancellationEcho(err error, ctx context.Context) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Cause(ctx))
}
