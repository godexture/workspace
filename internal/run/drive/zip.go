// Fan-in joins one item per input into a batch, bounded by the timestamp
// watermark the zip policy allows.
package drive

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/run/queue"
	"github.com/godexture/godec/internal/run/release"
	"github.com/godexture/godec/media/schema"
)

func zipJoiner[I, O any](joiner flow.Joiner[I, O], count int, limit queue.Limit, typ schema.Type[I], next delivery[O]) ([]Link, Task, error) {
	if count < 2 {
		return nil, Task{}, ErrBinding
	}
	traits := typ.Traits()
	edges := make([]*queue.Queue[I], count)
	links := make([]Link, count)
	for index := range edges {
		edge, err := queue.New(limit, typ)
		if err != nil {
			for previous := 0; previous < index; previous++ {
				edges[previous].Close()
			}
			return nil, Task{}, err
		}
		edges[index] = edge
		links[index] = linkOf[I](&bufferDelivery[I]{queue: edge, typ: typ})
	}
	state := &zipState[I, O]{joiner: joiner, edges: edges, typ: typ, items: make([]flow.Item[I], count), batch: make([]*flow.Item[I], count), next: next, time: traits.Time, watermark: limit.Time, done: make(chan struct{})}
	for index := range state.items {
		state.batch[index] = &state.items[index]
	}
	state.bindScope(journal.NewScope(""))
	task := Task{
		close:   state.close,
		discard: state.discard,
		barrier: state.barrier,
		finish:  state.finish,
		bind:    state.bindScope,
		run:     state.run,
	}
	return links, task, nil
}

type zipState[I, O any] struct {
	joiner     flow.Joiner[I, O]
	edges      []*queue.Queue[I]
	typ        schema.Type[I]
	items      []flow.Item[I]
	batch      []*flow.Item[I]
	read       int
	next       delivery[O]
	time       func(I) (int64, bool)
	watermark  int64
	done       chan struct{}
	scope      *journal.Scope
	reachedEOF bool
	quiesced   bool
	once       sync.Once
	err        error
}

// bindScope claims every input for the join: the batch slots and each ring
// belong to this task, so a release none of them can perform is its failure.
func (s *zipState[I, O]) bindScope(scope *journal.Scope) {
	s.scope = scope
	for index := range s.items {
		s.items[index].Bind(s.typ, scope)
	}
	for _, edge := range s.edges {
		edge.Bind(scope)
	}
}

func (s *zipState[I, O]) close() {
	for _, edge := range s.edges {
		edge.Close()
	}
}

func (s *zipState[I, O]) discard(into flow.Reporter) {
	for _, edge := range s.edges {
		edge.Drain(into)
	}
}

func (s *zipState[I, O]) run(ctx context.Context) (err error) {
	defer func() {
		s.close()
		s.discard(s.scope)
		// Reaching EOF is not enough. The batch this join was still holding is
		// released on the way out, and so is anything left in the inputs; a
		// release that fails there is recorded in this task's journal, and a
		// task with anything in its journal did not quiesce. The write precedes
		// the close of done, so the barrier reads it with that hand-off.
		s.quiesced = s.reachedEOF && err == nil && s.scope.Clean()
		close(s.done)
	}()
	defer s.release()
	for {
		s.read = 0
		for index, edge := range s.edges {
			err := edge.Pop(ctx, &s.items[index])
			if errors.Is(err, io.EOF) {
				// An input reaching EOF ends the stream for all of them. It is
				// the only end this join can quiesce from, but the cleanup on
				// the way out still has to succeed.
				s.reachedEOF = true
				return nil
			}
			if err != nil {
				return err
			}
			s.read++
		}
		if !s.withinWatermark() {
			return ErrWatermark
		}
		if err := s.joiner.Process(ctx, flow.NewBatch(s.batch[:s.read]), s.next); err != nil {
			return err
		}
		s.release()
		if !s.scope.Clean() {
			// The batch could not be released. Joining another one would leave
			// that behind and keep going over a broken release trait.
			return nil
		}
	}
}

func (s *zipState[I, O]) withinWatermark() bool {
	if s.watermark == 0 {
		return true
	}
	minimum, maximum := int64(0), int64(0)
	for index := 0; index < s.read; index++ {
		value, ok := s.time(s.items[index].Value())
		if !ok {
			return false
		}
		if index == 0 || value < minimum {
			minimum = value
		}
		if index == 0 || value > maximum {
			maximum = value
		}
	}
	return uint64(maximum)-uint64(minimum) <= uint64(s.watermark)
}

// release drops whatever the joiner left behind and returns the group's slots
// to their queues. A Joiner may consume any subset of the batch.
//
// Returning the slots is queue bookkeeping and happens first, so a declared
// Drop that panics cannot leave an edge permanently short of capacity, and
// every remaining payload is released before the failure is reported.
//
// The slots go back with Complete on every path, including the ones a bounded
// edge would Abandon. A join ends holding an unjoined batch even when it ends
// well, because one input reaching EOF ends the stream for all of them, so an
// input's active count can never express a fan-in's quiescence. That is what
// quiesced is for, and this call is only capacity.
func (s *zipState[I, O]) release() {
	read := s.read
	s.read = 0
	for index := 0; index < read; index++ {
		s.edges[index].Complete()
	}
	release.All(s.items[:read])
}

// barrier closes the inputs and waits for the join to finish them. A fan-in's
// quiescence is the task's own outcome rather than the idle state of its
// edges, because one batch spans every input.
//
// A join that stopped without draining its inputs, or that could not release
// what it still held on the way out, never reached that outcome. Reporting a
// barrier anyway would let Host run Finalize and Flush over a data path that
// has already died, and would move the failure's phase from run to whatever
// boundary noticed the cancellation next. Only a failing task ends this way,
// and its failure cancels this context, so the wait ends with that failure.
func (s *zipState[I, O]) barrier(ctx context.Context) error {
	s.close()
	select {
	case <-s.done:
		if s.quiesced {
			return nil
		}
	case <-ctx.Done():
		return context.Cause(ctx)
	}
	<-ctx.Done()
	return context.Cause(ctx)
}

func (s *zipState[I, O]) finish(ctx context.Context) error {
	s.once.Do(func() { s.err = errors.Join(s.joiner.Flush(ctx, s.next), s.next.close(ctx)) })
	return s.err
}
