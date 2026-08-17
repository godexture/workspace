package bind

import (
	"sort"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/job"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/plugin"
)

type openInput struct {
	port   job.Port
	direct bool
}

func (r Registry) openPorts(nodes []job.Node, edges []job.Edge) ([]openInput, []job.Port, error) {
	incoming := make(map[string]struct{}, len(edges))
	outgoing := make(map[string]struct{}, len(edges))
	for _, edge := range edges {
		incoming[edge.To().String()] = struct{}{}
		outgoing[edge.From().String()] = struct{}{}
	}
	var inputs []openInput
	var outputs []job.Port
	for _, node := range nodes {
		component, ok := r.index.Lookup(node.Component())
		if !ok {
			continue
		}
		if _, err := component.Resolve(node.Config()); err != nil {
			return nil, nil, err
		}
		shape := component.Ports()
		if port, ok := directReaderPort(component); ok {
			inputs = append(inputs, openInput{port: job.At(node.ID(), port.ID()), direct: true})
		}
		for _, port := range shape.Inputs {
			value := job.At(node.ID(), port.ID())
			if port.Required() {
				if _, connected := incoming[value.String()]; !connected {
					inputs = append(inputs, openInput{port: value})
				}
			}
		}
		for _, port := range shape.Outputs {
			value := job.At(node.ID(), port.ID())
			if port.Required() {
				if _, connected := outgoing[value.String()]; !connected {
					outputs = append(outputs, value)
				}
			}
		}
	}
	sort.Slice(inputs, func(left, right int) bool { return inputs[left].port.String() < inputs[right].port.String() })
	sort.Slice(outputs, func(left, right int) bool { return outputs[left].String() < outputs[right].String() })
	return inputs, outputs, nil
}

func directReaderPort(component plugin.Component) (flow.Port, bool) {
	trait, ok := mediaformat.ReadOf(component)
	if !ok || !trait.Valid() {
		return flow.Port{}, false
	}
	shape := component.Ports()
	if len(shape.Inputs) != 0 || len(shape.Outputs) != 1 || shape.Outputs[0].Multiplicity() != flow.ManyMultiplicity {
		return flow.Port{}, false
	}
	return shape.Outputs[0], true
}
