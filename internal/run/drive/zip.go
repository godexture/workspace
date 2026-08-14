// Fan-in joins one item per input into a batch, bounded by the timestamp
// watermark the zip policy allows.
package drive

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/run/queue"
	"github.com/godexture/godec/internal/run/release"
	"github.com/godexture/godec/media/schema"
)

func zipJoiner[I, O any](joiner flow.Joiner[I, O], count int, limit queue.Limit, traits schema.Traits[I], next delivery[O]) ([]Link, Task, error) {
	if count < 2 {
		return nil, Task{}, ErrBinding
	}
	edges := make([]*queue.Queue[I], count)
	links := make([]Link, count)
	stored := queueTraits(traits)
	for index := range edges {
		edge, err := queue.New(limit, stored)
		if err != nil {
			for previous := 0; previous < index; previous++ {
				edges[previous].Close()
			}
			return nil, Task{}, err
		}
		edges[index] = edge
		links[index] = linkOf[I](&bufferDelivery[I]{queue: edge})
	}
	state := &zipState[I, O]{joiner: joiner, edges: edges, traits: traits, items: make([]flow.Item[I], count), batch: make([]*flow.Item[I], count), next: next, time: traits.Time, watermark: limit.Time, done: make(chan struct{})}
	for index := range state.items {
		state.batch[index] = &state.items[index]
	}
	task := Task{
		close:   state.close,
		discard: state.discard,
		barrier: state.barrier,
		finish:  state.finish,
		run:     state.run,
	}
	return links, task, nil
}

type zipState[I, O any] struct {
	joiner    flow.Joiner[I, O]
	edges     []*queue.Queue[I]
	traits    schema.Traits[I]
	items     []flow.Item[I]
	batch     []*flow.Item[I]
	read      int
	next      delivery[O]
	time      func(I) (int64, bool)
	watermark int64
	done      chan struct{}
	quiesced  bool
	once      sync.Once
	err       error
}

func (s *zipState[I, O]) close() {
	for _, edge := range s.edges {
		edge.Close()
	}
}

func (s *zipState[I, O]) discard() error {
	problems := make([]error, 0, len(s.edges))
	for _, edge := range s.edges {
		_, err := edge.Drain()
		problems = append(problems, err)
	}
	return errors.Join(problems...)
}

func (s *zipState[I, O]) run(ctx context.Context) (err error) {
	defer func() {
		s.close()
		err = errors.Join(err, s.discard())
		close(s.done)
	}()
	defer func() { err = errors.Join(err, s.release()) }()
	for {
		s.read = 0
		for index, edge := range s.edges {
			err := edge.Pop(ctx, &s.items[index])
			if errors.Is(err, io.EOF) {
				// Draining an input to EOF is the only way this join reaches
				// quiescence. The write precedes the deferred close of done, so
				// the barrier reads it with that hand-off.
				s.quiesced = true
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
		if err := s.release(); err != nil {
			return err
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
func (s *zipState[I, O]) release() error {
	read := s.read
	s.read = 0
	for index := 0; index < read; index++ {
		s.edges[index].Complete()
	}
	return release.All(s.items[:read])
}

// barrier closes the inputs and waits for the join to finish them. A fan-in's
// quiescence is the task's own outcome rather than the idle state of its
// edges, because one batch spans every input.
//
// A join that stopped without draining its inputs never reached that outcome:
// the batch it held did not finish downstream. Reporting a barrier anyway
// would let Host run Finalize and Flush over a data path that has already
// died, and would move the failure's phase from run to whatever boundary
// noticed the cancellation next. Only a failing task ends this way, and its
// failure cancels this context, so the wait ends with that failure.
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
