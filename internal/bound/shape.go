package bound

import (
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/plan"
)

// Port returns the sole port exposed by a directional boundary shape.
func Port(shape flow.Shape, direction plan.BoundaryDirection) (flow.Port, bool) {
	switch direction {
	case plan.InputBoundary:
		if len(shape.Inputs) == 0 && len(shape.Outputs) == 1 && shape.Outputs[0].Multiplicity() == flow.One {
			return shape.Outputs[0], true
		}
	case plan.OutputBoundary:
		if len(shape.Inputs) == 1 && len(shape.Outputs) == 0 && shape.Inputs[0].Multiplicity() == flow.One {
			return shape.Inputs[0], true
		}
	}
	return flow.Port{}, false
}
