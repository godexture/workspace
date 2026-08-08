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
	for index, selected := range path {
		patch, err := selected.result.bridge.component.Schema().Patch(selected.result.config)
		if err != nil {
			return job.Graph{}, err
		}
		id := automaticNodeID(replaced, index, selected.result, used)
		used[id] = struct{}{}
		node := job.NewNode(id, selected.result.bridge.component.Identity(), patch)
		newNodes = append(newNodes, node)
		edge := job.Connect(previous, job.At(id, selected.result.bridge.input.ID()))
		newEdges = append(newEdges, edge)
		previous = job.At(id, selected.result.bridge.output.ID())
		newAnnotations[id] = annotation{origin: plan.Automatic, reason: gap.Need().Code(), summary: selected.result.config.Summary()}
	}
	newEdges = append(newEdges, job.Connect(previous, replaced.To()))
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
	for _, edge := range newEdges {
		p.edges[edgeKey(edge)] = annotation{origin: plan.Automatic, reason: gap.Need().Code()}
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
