package solve

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"

	"github.com/godexture/godec/internal/graph"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plan"
)

func (p *planner) insert(current job.Graph, gap graph.Gap, path []step) (job.Graph, error) {
	replaced, ok := gap.Edge()
	if !ok {
		return job.Graph{}, errors.New("cannot insert a bridge into an ambiguous gap")
	}
	nodes := current.Nodes()
	edges := current.Edges()
	kept := make([]job.Edge, 0, len(edges)-1+len(path)+1)
	found := false
	for _, edge := range edges {
		if edge == replaced && !found {
			found = true
			continue
		}
		kept = append(kept, edge)
	}
	if !found {
		return job.Graph{}, errors.New("gap edge is absent from the requested graph")
	}

	used := make(map[job.NodeID]struct{}, len(nodes)+len(path))
	for _, node := range nodes {
		used[node.ID()] = struct{}{}
	}
	previous := replaced.From()
	newNodes := make([]job.Node, 0, len(path))
	newEdges := make([]job.Edge, 0, len(path)+1)
	newAnnotations := make(map[job.NodeID]annotation, len(path))
	newEdgeAnnotations := make(map[string]annotation, len(path)+1)
	terminal, constrained := p.terminals[jobPortKey(gap.Node(), gap.Port())]
	for index, selected := range path {
		patch, err := selected.result.bridge.component.Schema().Patch(selected.result.config)
		if err != nil {
			return job.Graph{}, err
		}
		patch = patch.Planned()
		id := automaticNodeID(replaced, index, selected.result, used)
		used[id] = struct{}{}
		node := job.NewNode(id, selected.result.bridge.component.Identity(), patch)
		newNodes = append(newNodes, node)
		edge := job.Connect(previous, job.At(id, selected.result.bridge.input.ID()))
		newEdges = append(newEdges, edge)
		previous = job.At(id, selected.result.bridge.output.ID())
		reason := gap.Need().Code()
		if constrained && selected.result.bridge.component.Identity() == terminal.component && index == len(path)-1 {
			reason = terminal.reason
		}
		newAnnotations[id] = annotation{origin: plan.Automatic, reason: reason, summary: selected.result.config.Summary()}
		newEdgeAnnotations[edgeKey(edge)] = annotation{origin: plan.Automatic, reason: reason}
		if constrained && selected.result.bridge.component.Identity() == terminal.component && index == len(path)-1 {
			p.contexts = p.contexts.WithPrepared(id, terminal.context)
		}
	}
	last := job.Connect(previous, replaced.To())
	newEdges = append(newEdges, last)
	lastReason := gap.Need().Code()
	if constrained && len(path) != 0 && path[len(path)-1].result.bridge.component.Identity() == terminal.component {
		lastReason = terminal.reason
	}
	newEdgeAnnotations[edgeKey(last)] = annotation{origin: plan.Automatic, reason: lastReason}
	nodes = append(nodes, newNodes...)
	kept = append(kept, newEdges...)
	updated, err := job.NewGraph(nodes, kept, current.Mappings()...)
	if err != nil {
		return job.Graph{}, err
	}
	delete(p.edges, edgeKey(replaced))
	for id, value := range newAnnotations {
		p.nodes[id] = value
	}
	for key, value := range newEdgeAnnotations {
		p.edges[key] = value
	}
	return updated, nil
}

func automaticNodeID(edge job.Edge, index int, result candidateResult, used map[job.NodeID]struct{}) job.NodeID {
	base := edge.From().String() + "->" + edge.To().String() + "\x00" + strconv.Itoa(index) + "\x00" + result.bridge.component.Identity().String() + "\x00" + result.config.Fingerprint.String()
	for nonce := 0; ; nonce++ {
		digest := sha256.Sum256([]byte(base + "\x00" + strconv.Itoa(nonce)))
		id := job.NodeID("auto-" + hex.EncodeToString(digest[:]))
		if _, exists := used[id]; !exists {
			return id
		}
	}
}

func edgeKey(edge job.Edge) string {
	return edge.From().String() + "->" + edge.To().String()
}
