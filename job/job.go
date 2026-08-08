// Package job defines immutable user requests and explicit pinned graphs.
package job

import (
	"errors"
	"fmt"
)

// Job is a normalized request boundary. Inputs and outputs are optional only
// when an explicit graph already contains its own source and sink nodes.
type Job struct {
	inputs  []Input
	outputs []Output
	graph   Graph
}

func New(inputs []Input, outputs []Output, graph Graph) (Job, error) {
	if !graph.Valid() && (len(inputs) == 0 || len(outputs) == 0) {
		return Job{}, errors.New("job needs input and output choices or an explicit graph")
	}
	for index, input := range inputs {
		if !input.Valid() {
			return Job{}, fmt.Errorf("job input %d is invalid", index)
		}
	}
	for index, output := range outputs {
		if !output.Valid() {
			return Job{}, fmt.Errorf("job output %d is invalid", index)
		}
	}
	return Job{inputs: append([]Input(nil), inputs...), outputs: append([]Output(nil), outputs...), graph: graph}, nil
}

func (j Job) Valid() bool {
	return j.graph.Valid() || len(j.inputs) != 0 && len(j.outputs) != 0
}
func (j Job) Inputs() []Input   { return append([]Input(nil), j.inputs...) }
func (j Job) Outputs() []Output { return append([]Output(nil), j.outputs...) }
func (j Job) Graph() (Graph, bool) {
	return j.graph, j.graph.Valid()
}
