package solve

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/internal/graph"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plan"
)

func (p *planner) insert(current job.Graph, gap graph.Gap, result searchResult) (job.Graph, error) {
	nodes := current.Nodes()
	if result.hasConfig {
		var err error
		nodes, err = p.patchConfig(nodes, gap, result.config)
		if err != nil {
			return job.Graph{}, err
		}
	}
	if len(result.path) == 0 {
		updated, err := job.NewGraph(nodes, current.Edges(), current.Mappings()...)
		if err != nil {
			return job.Graph{}, err
		}
		p.markInferred(gap.Node(), result.config)
		return updated, nil
	}

	replaced, ok := gap.Edge()
	if !ok {
		return job.Graph{}, errors.New("cannot insert a bridge into an ambiguous gap")
	}
	edges := current.Edges()
	kept := make([]job.Edge, 0, len(edges)-1+len(result.path)+1)
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

	used := make(map[job.NodeID]struct{}, len(nodes)+len(result.path))
	for _, node := range nodes {
		used[node.ID()] = struct{}{}
	}
	previous := replaced.From()
	newNodes := make([]job.Node, 0, len(result.path))
	newEdges := make([]job.Edge, 0, len(result.path)+1)
	newAnnotations := make(map[job.NodeID]annotation, len(result.path))
	newEdgeAnnotations := make(map[string]annotation, len(result.path)+1)
	for index, selected := range result.path {
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
		newAnnotations[id] = annotation{origin: plan.Automatic, reason: reason, summary: selected.result.config.Summary()}
		newEdgeAnnotations[edgeKey(edge)] = annotation{origin: plan.Automatic, reason: reason}
	}
	last := job.Connect(previous, replaced.To())
	newEdges = append(newEdges, last)
	newEdgeAnnotations[edgeKey(last)] = annotation{origin: plan.Automatic, reason: gap.Need().Code()}
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
	p.markInferred(gap.Node(), result.config)
	return updated, nil
}

func (p *planner) patchConfig(nodes []job.Node, gap graph.Gap, resolved config.ResolvedView) ([]job.Node, error) {
	metadata, ok := p.nodes[gap.Node()]
	if !ok || !metadata.inferConfig {
		return nil, errors.New("attempted to infer config for a fixed node")
	}
	patch, err := gap.Component().Schema().Patch(resolved)
	if err != nil {
		return nil, err
	}
	result := append([]job.Node(nil), nodes...)
	for index, node := range result {
		if node.ID() != gap.Node() {
			continue
		}
		if node.Component() != gap.Component().Identity() {
			return nil, errors.New("gap component differs from the node being configured")
		}
		result[index] = job.NewNode(node.ID(), node.Component(), patch.Planned())
		return result, nil
	}
	return nil, errors.New("gap node is absent from the requested graph")
}

func (p *planner) markInferred(id job.NodeID, resolved config.ResolvedView) {
	if resolved.Fingerprint().IsZero() {
		return
	}
	metadata, ok := p.nodes[id]
	if !ok || !metadata.inferConfig {
		return
	}
	metadata.inferConfig = false
	metadata.summary = resolved.Summary()
	p.nodes[id] = metadata
}

func automaticNodeID(edge job.Edge, index int, result candidateResult, used map[job.NodeID]struct{}) job.NodeID {
	base := edge.From().String() + "->" + edge.To().String() + "\x00" + strconv.Itoa(index) + "\x00" + result.bridge.component.Identity().String() + "\x00" + result.config.Fingerprint().String()
	for nonce := 0; ; nonce++ {
		digest := sha256.Sum256([]byte(base + "\x00" + strconv.Itoa(nonce)))
		id := job.NodeID("auto-" + hex.EncodeToString(digest[:8]))
		if _, exists := used[id]; !exists {
			return id
		}
	}
}

func edgeKey(edge job.Edge) string {
	return edge.From().String() + "->" + edge.To().String()
}
