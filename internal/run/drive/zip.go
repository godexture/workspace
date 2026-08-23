// Fan-in joins one item per input into a batch, bounded by the timestamp
// tolerance the zip policy allows.
package drive

import (
	"context"
	"io"
	"sync"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/errorx"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/run/queue"
	"github.com/godexture/godec/internal/run/release"
	"github.com/godexture/godec/media/schema"
)

func zipJoiner[I, O any](joiner flow.Joiner[I, O], count int, prior []bool, limit queue.Limit, tolerance int64, typ schema.Type[I], next delivery[O], owner *journal.Domain) ([]Link, Task, error) {
	if count < 2 || tolerance < 0 {
		return nil, Task{}, ErrBinding
	}
	traits := typ.Traits()
	site := owner.At(owner.Home())
	edges := make([]*queue.Queue[I], count)
	links := make([]Link, count)
	for index := range edges {
		edge, err := queue.New(limit, typ, site.Reporter())
		if err != nil {
			for previous := 0; previous < index; previous++ {
				edges[previous].Abort()
			}
			return nil, Task{}, err
		}
		edges[index] = edge
		links[index] = linkOf[I](&bufferDelivery[I]{queue: edge, typ: typ, node: owner.Home()})
	}
	// A fan-in over one many port declares no order between its branches, so
	// the flags arrive empty and every input is read from the start.
	ordering := make([]bool, count)
	waiting := 0
	if len(prior) == count {
		copy(ordering, prior)
		for _, value := range ordering {
			if value {
				waiting++
			}
		}
	}
	state := &zipState[I, O]{joiner: joiner, edges: edges, typ: typ, items: make([]flow.Item[I], count), batch: make([]*flow.Item[I], count), popped: make([]bool, count), ended: make([]bool, count), prior: ordering, waiting: waiting, live: count, next: next, time: traits.Time, tolerance: tolerance, done: make(chan struct{}), site: site}
	for index := range state.items {
		state.batch[index] = &state.items[index]
		state.items[index].Bind(typ, site.Reporter())
	}
	task := Task{
		domain:  owner,
		abort:   state.abort,
		discard: state.discard,
		barrier: state.barrier,
		finish:  state.finish,
		run:     state.run,
		sealed:  state.sealed,
	}
	return links, task, nil
}

type zipState[I, O any] struct {
	joiner flow.Joiner[I, O]
	edges  []*queue.Queue[I]
	typ    schema.Type[I]
	items  []flow.Item[I]
	batch  []*flow.Item[I]
	// popped marks the inputs this round took a slot from, so the slots go
	// back to exactly those queues however many cells the joiner consumed.
	popped []bool
	// ended marks the inputs that have finished. They stop contributing a cell
	// and stop being read; the join ends when none is left.
	ended []bool
	// prior marks the inputs read to completion before the rest, and waiting
	// counts how many of them have not finished yet. While any has not, the
	// others are left alone: their queues fill and their producers wait, which
	// is what keeps a component that needs one input whole from holding the
	// others in memory to get it.
	prior      []bool
	waiting    int
	live       int
	next       delivery[O]
	time       func(I) (int64, bool)
	tolerance  int64
	done       chan struct{}
	site       *journal.Site
	reachedEOF bool
	stopped    bool
	quiesced   bool
	once       sync.Once
	err        error
}

func (s *zipState[I, O]) abort() {
	for _, edge := range s.edges {
		edge.Abort()
	}
}

func (s *zipState[I, O]) seal() {
	for _, edge := range s.edges {
		edge.Seal()
	}
}

func (s *zipState[I, O]) discard() {
	for _, edge := range s.edges {
		edge.Drain()
	}
}

func (s *zipState[I, O]) run(ctx context.Context, span *journal.Span) (err error) {
	defer func() {
		s.abort()
		s.discard()
		// Reaching EOF is not enough. The batch this join was still holding is
		// released on the way out, and so is anything left in the inputs; a
		// release that fails there is recorded against this task's domain, and
		// a task with anything recorded did not quiesce. The sealed hook, not
		// this defer, closes done: Run returning is not the same moment as
		// this task's span ending, and the barrier must not act on this domain
		// until it has.
		s.quiesced = (s.reachedEOF || s.stopped) && err == nil && span.Clean()
	}()
	defer s.release()
	for {
		for index, edge := range s.edges {
			if s.ended[index] || s.waiting != 0 && !s.prior[index] {
				continue
			}
			err := edge.Pop(ctx, &s.items[index])
			if errorx.Is(err, io.EOF) {
				// An input that has finished stops contributing a cell and
				// stops being read. The others carry on: what a joiner makes
				// of an absent input is its own business, and a mixer that
				// took it for silence has a different answer from a stage that
				// has been waiting for it.
				s.ended[index] = true
				s.live--
				if s.prior[index] {
					s.waiting--
				}
				continue
			}
			if errorx.Is(err, queue.ErrAbandoned) {
				s.stopped = true
				return nil
			}
			if err != nil {
				return err
			}
			s.popped[index] = true
		}
		if s.live == 0 && s.waiting == 0 {
			// Every input has finished, which is the only end this join can
			// quiesce from. The cleanup on the way out still has to succeed.
			s.reachedEOF = true
			return nil
		}
		if !s.withinTolerance() {
			return ErrTolerance
		}
		if err := s.joiner.Process(ctx, flow.NewBatch(s.batch), s.next); err != nil {
			if isAbandoned(err) {
				s.stopped = true
				return nil
			}
			return err
		}
		s.release()
		if !span.Clean() {
			// The batch could not be released. Joining another one would leave
			// that behind and keep going over a broken release trait.
			return nil
		}
	}
}

// withinTolerance measures only the inputs that contributed a cell. An input
// that has finished has no instant to be far from, and holding the join to one
// would end every stream at the first of them to run out.
func (s *zipState[I, O]) withinTolerance() bool {
	if s.tolerance == 0 {
		return true
	}
	minimum, maximum := int64(0), int64(0)
	seen := false
	for index := range s.items {
		if !s.popped[index] {
			continue
		}
		value, ok := s.time(s.items[index].Value())
		if !ok {
			return false
		}
		if !seen || value < minimum {
			minimum = value
		}
		if !seen || value > maximum {
			maximum = value
		}
		seen = true
	}
	return uint64(maximum)-uint64(minimum) <= uint64(s.tolerance)
}

// release drops whatever the joiner left behind and returns the group's slots
// to their queues. A Joiner may consume any subset of the batch.
//
// Returning the slots is queue bookkeeping and happens first, so a declared
// Drop that panics cannot leave an edge permanently short of capacity, and
// every remaining payload is released before the failure is reported.
//
// The slots go back with Complete on every path, including the ones a bounded
// edge would Abandon. A join can end holding an unjoined batch, so an input's
// active count can never express a fan-in's quiescence. That is what quiesced
// is for, and this call is only capacity.
func (s *zipState[I, O]) release() {
	for index := range s.edges {
		if !s.popped[index] {
			continue
		}
		s.popped[index] = false
		s.edges[index].Complete()
	}
	release.All(s.items)
}

// sealed is task.Group's sealed hook: it runs after this task's Run span has
// ended, so closing done here -- rather than in run's own defer, before the
// span has recorded what work returned -- is what lets the barrier's waiter,
// and the run's own Finish afterwards, act on this domain safely.
func (s *zipState[I, O]) sealed(error) { close(s.done) }

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
	if ctx == nil {
		ctx = context.Background()
	}
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	s.seal()
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

// finish flushes the join and then closes what it feeds. As with a linear
// stage, the two are independent failures and each is recorded where it
// happens rather than joined into one.
func (s *zipState[I, O]) finish(ctx context.Context) error {
	if !s.reachedEOF {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	s.once.Do(func() {
		flushed := s.site.Perform(func() error { return s.joiner.Flush(ctx, s.next) })
		s.err = firstFailure(flushed, s.next.close(ctx))
	})
	return s.err
}
