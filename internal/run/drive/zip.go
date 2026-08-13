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
		links[index] = linkOf[I](&bufferDelivery[I]{queue: edge, traits: traits})
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
	once      sync.Once
	err       error
}

func (s *zipState[I, O]) close() {
	for _, edge := range s.edges {
		edge.Close()
	}
}

func (s *zipState[I, O]) discard() {
	for _, edge := range s.edges {
		edge.Drain()
	}
}

func (s *zipState[I, O]) run(ctx context.Context) error {
	defer func() {
		s.close()
		for _, edge := range s.edges {
			edge.Drain()
		}
		close(s.done)
	}()
	defer s.release()
	for {
		s.read = 0
		for index, edge := range s.edges {
			value, err := edge.Pop(ctx)
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
			s.items[index].SetWithTraits(value, s.traits.Fork, s.traits.Drop)
			s.read++
		}
		if !s.withinWatermark() {
			return ErrWatermark
		}
		if err := s.joiner.Process(ctx, flow.NewBatch(s.batch[:s.read]), s.next); err != nil {
			return err
		}
		s.release()
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
func (s *zipState[I, O]) release() {
	for index := 0; index < s.read; index++ {
		s.items[index].Drop()
		s.edges[index].Complete()
	}
	s.read = 0
}

func (s *zipState[I, O]) barrier(ctx context.Context) error {
	s.close()
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *zipState[I, O]) finish(ctx context.Context) error {
	s.once.Do(func() { s.err = errors.Join(s.joiner.Flush(ctx, s.next), s.next.close(ctx)) })
	return s.err
}
