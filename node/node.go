package node

import "context"

type Lifecycle interface {
	Start(ctx context.Context) error
}

type Node interface {
	Lifecycle
}

type InputNode[T any] interface {
	InputPorts() map[string]*InPort[T]
}

type OutputNode[T any] interface {
	OutputPorts() map[string]*OutPort[T]
}
