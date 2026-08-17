// Serial fan-in synchronously serializes callbacks from many inputs without a
// queue or a separate task.
package drive

import (
	"context"
	"sync"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/media/schema"
)

func serialFanIn[I, O any](joiner flow.Joiner[I, O], count int, tolerance int64, typ schema.Type[I], next delivery[O], owner *journal.Domain) ([]Link, Task, error) {
	if count < 1 || tolerance != 0 {
		return nil, Task{}, ErrBinding
	}
	state := &serialState[I, O]{
		joiner:    joiner,
		next:      next,
		typ:       typ,
		site:      owner.At(owner.Home()),
		closed:    make([]bool, count),
		remaining: count,
	}
	links := make([]Link, count)
	for input := range links {
		links[input] = linkOf[I](&serialDelivery[I, O]{state: state, input: input})
	}
	return links, Task{}, nil
}

type serialState[I, O any] struct {
	joiner flow.Joiner[I, O]
	next   delivery[O]
	typ    schema.Type[I]
	site   *journal.Site

	mu        sync.Mutex
	closed    []bool
	remaining int
	stopped   bool
	finished  bool
	closeErr  error
}

type serialDelivery[I, O any] struct {
	state *serialState[I, O]
	input int
}

func (m *serialDelivery[I, O]) Own(into *flow.Item[I], value I) {
	into.Bind(m.state.typ, m.state.site.Reporter())
	into.Set(value)
}

func (m *serialDelivery[I, O]) Emit(ctx context.Context, item *flow.Item[I]) error {
	if item == nil || !item.Valid() {
		return ErrInvalidItem
	}
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	if m.state.closed[m.input] || m.state.stopped || m.state.finished {
		return errAbandoned
	}
	err := performDelivery(ctx, m.state.site, func() (err error) {
		defer func() {
			item.Drop()
			if err == nil {
				err = m.state.site.Domain().Ledger().Stopped()
			}
		}()
		return m.state.joiner.Process(ctx, flow.NewSelectedBatch(m.input, item), m.state.next)
	})
	if err != nil {
		m.state.stopped = true
	}
	return err
}

func (m *serialDelivery[I, O]) close(ctx context.Context) error {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	if m.state.closed[m.input] {
		return m.state.closeErr
	}
	m.state.closed[m.input] = true
	m.state.remaining--
	if ctx == nil {
		ctx = context.Background()
	}
	if cause := context.Cause(ctx); cause != nil {
		m.state.stopped = true
		return cause
	}
	if m.state.stopped {
		return m.state.closeErr
	}
	if m.state.remaining != 0 {
		return nil
	}
	m.state.finished = true
	flushed := m.state.site.Perform(func() error { return m.state.joiner.Flush(ctx, m.state.next) })
	m.state.closeErr = firstFailure(flushed, m.state.next.close(ctx))
	return m.state.closeErr
}

func (m *serialDelivery[I, O]) prepareClose(ctx context.Context) {
	if next, ok := m.state.next.(interface{ prepareClose(context.Context) }); ok {
		next.prepareClose(ctx)
	}
}

// The serial fan-in owns its component site from construction. Its callers remain
// owners of the input cells until Process consumes them, so binding a caller's
// domain here would incorrectly move component failure attribution upstream.
func (*serialDelivery[I, O]) bindDomain(*journal.Domain) {}

func (m *serialDelivery[I, O]) bound() bool {
	if m.state.site == nil {
		return false
	}
	next, ok := m.state.next.(domainBinder)
	return !ok || next.bound()
}
