package node

import (
	"context"

	"github.com/godexture/core/domain/media"
)

type ConstraintFunc func(info media.StreamInfo) error

type InPort[T any] struct {
	id             string
	constraintFunc ConstraintFunc

	edge Edge[T]
}

func NewInPort[T any](id string, c ConstraintFunc) *InPort[T] {
	return &InPort[T]{id: id, constraintFunc: c}
}

func (p *InPort[T]) ID() string        { return p.id }
func (p *InPort[T]) Connect(e Edge[T]) { p.edge = e }

func (p *InPort[T]) Accept(info media.StreamInfo) error {
	if p.constraintFunc != nil {
		return p.constraintFunc(info)
	}

	return nil
}

func (p *InPort[T]) Pull(ctx context.Context) (T, error) {
	return p.edge.Pull(ctx)
}

func (p *InPort[T]) Edge() Edge[T] { return p.edge }

type OutPort[T any] struct {
	id   string
	info media.StreamInfo

	edge Edge[T]
}

func NewOutPort[T any](id string, info media.StreamInfo) *OutPort[T] {
	return &OutPort[T]{id: id, info: info}
}

func (p *OutPort[T]) SetStreamInfo(info media.StreamInfo) {
	p.info = info
}

func (p *OutPort[T]) ID() string                   { return p.id }
func (p *OutPort[T]) StreamInfo() media.StreamInfo { return p.info }

func (p *OutPort[T]) Connect(e Edge[T]) { p.edge = e }

func (p *OutPort[T]) Push(ctx context.Context, data T) error {
	return p.edge.Push(ctx, data)
}

func (p *OutPort[T]) Edge() Edge[T] { return p.edge }
