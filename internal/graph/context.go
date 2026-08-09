package graph

import (
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plugin"
)

// CompileContexts is the immutable node-local prepared input to component
// compilation. Nodes inserted by the solver intentionally receive the zero
// context unless an earlier Prepare phase selected and inspected them.
type CompileContexts struct {
	byNode map[job.NodeID]plugin.CompileContext
}

func NewCompileContexts(values map[job.NodeID]plugin.CompileContext) CompileContexts {
	if len(values) == 0 {
		return CompileContexts{}
	}
	result := make(map[job.NodeID]plugin.CompileContext, len(values))
	for id, value := range values {
		if id != "" {
			result[id] = value
		}
	}
	return CompileContexts{byNode: result}
}

func (c CompileContexts) For(id job.NodeID) plugin.CompileContext {
	return c.byNode[id]
}
