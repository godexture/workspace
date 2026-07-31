package node

import "context"

type Lifecycle interface {
	Start(ctx context.Context) error
	Close() error
}

type Node interface {
	Lifecycle
}

type InputPhase uint8

const (
	InputPhaseRun InputPhase = iota
	InputPhasePreload
)

// StagedInput is implemented by nodes that consume some inputs before the
// regular pipeline run begins. Preload must consume every InputPhasePreload
// port to EOF without producing downstream output.
type StagedInput interface {
	InputPhases() map[string]InputPhase
	Preload(context.Context) error
}

type InputNode[T any] interface {
	InputPorts() map[string]*InPort[T]
}

type OutputNode[T any] interface {
	OutputPorts() map[string]*OutPort[T]
}
