package graph

import (
	"context"

	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plugin"
)

// CompileContexts is the immutable node-local prepared input to component
// compilation. Solver-inserted nodes receive values only when Host selected
// them before solving, such as an output Format terminal.
type CompileContexts struct {
	byNode   map[job.NodeID]plugin.CompileContext
	planning context.Context
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
	value := c.byNode[id]
	if c.planning == nil {
		return value
	}
	return plugin.CompileContextWithContext(value, c.planning)
}

// WithContext returns a view that applies one planning cancellation context
// to every requested or inserted node without changing prepared trait slots.
func (c CompileContexts) WithContext(ctx context.Context) CompileContexts {
	c.planning = ctx
	return c
}

// WithPrepared returns a copy carrying node-local values for a component
// selected before solver insertion.
func (c CompileContexts) WithPrepared(id job.NodeID, value plugin.CompileContext) CompileContexts {
	result := make(map[job.NodeID]plugin.CompileContext, len(c.byNode)+1)
	for key, prepared := range c.byNode {
		result[key] = prepared
	}
	if id.Valid() {
		result[id] = value
	}
	return CompileContexts{byNode: result, planning: c.planning}
}
