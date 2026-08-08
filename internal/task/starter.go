package task

import (
	"context"
	"errors"
	"sync"
)

var ErrWorkerLimit = errors.New("component worker grant is exhausted")

// Starter is a node-local view of a job task group. It enforces the worker
// grant selected in the Program while the Group retains cancel, panic, and
// join ownership.
type Starter struct {
	group *Group
	limit uint32

	mu     sync.Mutex
	active uint32
}

func NewStarter(group *Group, limit uint32) *Starter {
	return &Starter{group: group, limit: limit}
}

func (s *Starter) Start(name string, work func(context.Context) error) error {
	if s == nil || s.group == nil {
		return ErrClosed
	}
	s.mu.Lock()
	if s.active >= s.limit {
		s.mu.Unlock()
		return ErrWorkerLimit
	}
	s.active++
	s.mu.Unlock()

	err := s.group.Start(name, func(ctx context.Context) error {
		defer s.release()
		return work(ctx)
	})
	if err != nil {
		s.release()
	}
	return err
}

func (s *Starter) release() {
	s.mu.Lock()
	if s.active != 0 {
		s.active--
	}
	s.mu.Unlock()
}
