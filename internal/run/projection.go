package run

import (
	"strconv"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/run/drive"
	"github.com/godexture/godec/plan"
)

func (t Template) project() plan.Runtime {
	result := plan.Runtime{Executable: true}
	assigned := make([]bool, len(t.nodes))
	for index := range t.nodes {
		if assigned[index] {
			continue
		}
		island := plan.Island{ID: "island-" + strconv.Itoa(len(result.Islands))}
		current := index
		for {
			assigned[current] = true
			island.Nodes = append(island.Nodes, t.nodes[current].id.String())
			if t.nodes[current].kind != drive.Processor || len(t.outgoing[current]) != 1 {
				break
			}
			connection := t.connections[t.outgoing[current][0]]
			if connection.reason != 0 || assigned[connection.to] || t.nodes[connection.to].kind != drive.Processor || len(t.incoming[connection.to]) != 1 {
				break
			}
			current = connection.to
		}
		result.Islands = append(result.Islands, island)
	}
	for _, edge := range t.edges {
		if edge.reason == 0 {
			continue
		}
		result.Buffers = append(result.Buffers, plan.Buffer{
			ID:       edgeKey(edge.value),
			FromNode: edge.value.From().Node().String(),
			FromPort: edge.value.From().ID(),
			ToNode:   edge.value.To().Node().String(),
			ToPort:   edge.value.To().ID(),
			Limit:    edge.limit,
			Reason:   edge.reason,
		})
	}
	for _, value := range t.nodes {
		if value.kind != drive.Joiner {
			continue
		}
		result.FanIns = append(result.FanIns, plan.FanIn{
			Node:      value.id.String(),
			Port:      value.binding.Input(),
			Policy:    value.binding.FanIn(),
			Tolerance: value.tolerance,
			Direct:    directInput(value.shape, value.binding.Input()),
		})
	}
	return result
}

func directInput(shape flow.Shape, port string) bool {
	for _, value := range shape.Inputs {
		if value.ID() == port {
			return value.Direct()
		}
	}
	return false
}

func cloneProjection(value plan.Runtime) plan.Runtime {
	result := value
	result.Islands = append([]plan.Island(nil), value.Islands...)
	for index := range result.Islands {
		result.Islands[index].Nodes = append([]string(nil), value.Islands[index].Nodes...)
	}
	result.Buffers = append([]plan.Buffer(nil), value.Buffers...)
	result.FanIns = append([]plan.FanIn(nil), value.FanIns...)
	return result
}
