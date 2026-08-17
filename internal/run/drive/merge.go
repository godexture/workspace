package drive

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/errorx"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/run/queue"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/timing"
)

func mergeJoiner[I, O any](joiner flow.Joiner[I, O], inputs []JoinInput, tolerance int64, typ schema.Type[I], next delivery[O], owner *journal.Domain) ([]Link, Task, error) {
	traits := typ.Traits()
	if len(inputs) == 0 || tolerance != 0 || traits.Time == nil || traits.Order == nil {
		return nil, Task{}, ErrBinding
	}
	for _, input := range inputs {
		if !input.Base.Valid() {
			return nil, Task{}, ErrBinding
		}
	}

	site := owner.At(owner.Home())
	edges := make([]*queue.Queue[I], len(inputs))
	links := make([]Link, len(inputs))
	bases := make([]timing.Base, len(inputs))
	for index := range edges {
		edge, err := queue.New(inputs[index].Limit, typ, site.Reporter())
		if err != nil {
			for previous := 0; previous < index; previous++ {
				edges[previous].Abort()
			}
			return nil, Task{}, err
		}
		edges[index] = edge
		links[index] = linkOf[I](&bufferDelivery[I]{queue: edge, typ: typ, node: owner.Home()})
		bases[index] = inputs[index].Base
	}
	state := &mergeState[I, O]{
		joiner:  joiner,
		edges:   edges,
		heads:   make([]flow.Item[I], len(inputs)),
		ready:   make([]bool, len(inputs)),
		eof:     make([]bool, len(inputs)),
		orders:  make([]int64, len(inputs)),
		last:    make([]int64, len(inputs)),
		hasLast: make([]bool, len(inputs)),
		bases:   bases,
		order:   traits.Order,
		next:    next,
		done:    make(chan struct{}),
		site:    site,
	}
	for index := range state.heads {
		state.heads[index].Bind(typ, site.Reporter())
	}
	return links, Task{
		domain:  owner,
		abort:   state.abort,
		discard: state.discard,
		barrier: state.barrier,
		finish:  state.finish,
		run:     state.run,
		sealed:  state.sealed,
	}, nil
}

type mergeState[I, O any] struct {
	joiner  flow.Joiner[I, O]
	edges   []*queue.Queue[I]
	heads   []flow.Item[I]
	ready   []bool
	eof     []bool
	orders  []int64
	last    []int64
	hasLast []bool
	bases   []timing.Base
	order   func(I) (int64, bool)
	next    delivery[O]
	done    chan struct{}
	site    *journal.Site

	reachedEOF bool
	stopped    bool
	quiesced   bool
	once       sync.Once
	err        error
}

func (s *mergeState[I, O]) abort() {
	for _, edge := range s.edges {
		edge.Abort()
	}
}

func (s *mergeState[I, O]) seal() {
	for _, edge := range s.edges {
		edge.Seal()
	}
}

func (s *mergeState[I, O]) discard() {
	for _, edge := range s.edges {
		edge.Drain()
	}
}

func (s *mergeState[I, O]) run(ctx context.Context, span *journal.Span) (err error) {
	defer func() {
		s.abort()
		s.discard()
		s.quiesced = (s.reachedEOF || s.stopped) && err == nil && span.Clean()
	}()
	defer s.releaseHeads()
	for {
		for index, edge := range s.edges {
			if s.eof[index] || s.ready[index] {
				continue
			}
			err := edge.Pop(ctx, &s.heads[index])
			if errorx.Is(err, io.EOF) {
				s.eof[index] = true
				continue
			}
			if errorx.Is(err, queue.ErrAbandoned) {
				s.stopped = true
				return nil
			}
			if err != nil {
				return err
			}
			s.ready[index] = true
			order, ok := s.order(s.heads[index].Value())
			if !ok {
				return ErrOrderMissing
			}
			if s.hasLast[index] && order < s.last[index] {
				return ErrOrderBackward
			}
			s.orders[index] = order
		}

		selected, selectErr := s.selectHead()
		if selectErr != nil {
			return selectErr
		}
		if selected < 0 {
			s.reachedEOF = true
			return nil
		}

		if err := s.joiner.Process(ctx, flow.NewSelectedBatch(selected, &s.heads[selected]), s.next); err != nil {
			if isAbandoned(err) {
				s.stopped = true
				return nil
			}
			return err
		}
		s.last[selected] = s.orders[selected]
		s.hasLast[selected] = true
		s.release(selected)
		if !span.Clean() {
			return nil
		}
	}
}

func (s *mergeState[I, O]) selectHead() (int, error) {
	selected := -1
	for index := range s.edges {
		if !s.ready[index] {
			continue
		}
		if selected < 0 {
			selected = index
			continue
		}
		comparison, err := timing.Compare(s.orders[index], s.bases[index], s.orders[selected], s.bases[selected])
		if err != nil {
			return -1, errors.Join(ErrOrderCompare, err)
		}
		if comparison < 0 {
			selected = index
		}
	}
	return selected, nil
}

func (s *mergeState[I, O]) release(index int) {
	if !s.ready[index] {
		return
	}
	s.ready[index] = false
	s.edges[index].Complete()
	s.heads[index].Drop()
}

func (s *mergeState[I, O]) releaseHeads() {
	for index := range s.heads {
		s.release(index)
	}
}

func (s *mergeState[I, O]) sealed(error) { close(s.done) }

func (s *mergeState[I, O]) barrier(ctx context.Context) error {
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

func (s *mergeState[I, O]) finish(ctx context.Context) error {
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
