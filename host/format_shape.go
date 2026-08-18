package host

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/internal/solve"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

type formatInsertion struct {
	request  job.Job
	node     job.Node
	replaced job.Edge
	inserted []job.Edge
}

func insertInputFormat(request job.Job, boundary plan.Boundary, component plugin.Component, patch config.Patch) (formatInsertion, error) {
	shape := component.Ports()
	if len(shape.Outputs) != 1 || len(shape.Inputs) > 1 {
		return formatInsertion{}, formatShapeError("prepare.probe-shape", boundary, component, "automatically selected input Format must expose one output and at most one input")
	}
	return insertFormat(request, boundary, component, patch, plan.InputBoundary)
}

func insertOutputFormat(request job.Job, boundary plan.Boundary, component plugin.Component, patch config.Patch) (formatInsertion, error) {
	shape := component.Ports()
	if len(shape.Inputs) != 1 || len(shape.Outputs) != 1 {
		return formatInsertion{}, formatShapeError("prepare.format-shape", boundary, component, "automatically selected output Format must expose one input and one output")
	}
	return insertFormat(request, boundary, component, patch, plan.OutputBoundary)
}

func insertFormat(request job.Job, boundary plan.Boundary, component plugin.Component, patch config.Patch, direction plan.BoundaryDirection) (formatInsertion, error) {
	requested, ok := request.Graph()
	if !ok {
		return formatInsertion{}, errors.New("automatic Format selection requires a normalized graph")
	}
	if _, err := component.Resolve(patch); err != nil {
		return formatInsertion{}, err
	}
	if boundary.Direction != direction {
		return formatInsertion{}, errors.New("automatic Format insertion received the wrong boundary direction")
	}

	nodes := requested.Nodes()
	edges := requested.Edges()
	used := make(map[job.NodeID]struct{}, len(nodes)+1)
	for _, node := range nodes {
		used[node.ID()] = struct{}{}
	}
	replaced, index, err := formatBoundaryEdge(edges, boundary, component.Identity())
	if err != nil {
		return formatInsertion{}, err
	}
	id := selectedFormatNodeID(replaced, component.Identity(), used)
	node := job.NewNode(id, component.Identity(), patch)
	shape := component.Ports()

	var inserted []job.Edge
	if direction == plan.InputBoundary && len(shape.Inputs) == 0 {
		filtered, err := removeCarrierNode(nodes, edges, job.NodeID(boundary.Node), component.Identity(), boundary)
		if err != nil {
			return formatInsertion{}, err
		}
		nodes = append(filtered, node)
		inserted = []job.Edge{job.Connect(job.At(id, shape.Outputs[0].ID()), replaced.To())}
		edges[index] = inserted[0]
	} else {
		first := job.Connect(replaced.From(), job.At(id, shape.Inputs[0].ID()))
		second := job.Connect(job.At(id, shape.Outputs[0].ID()), replaced.To())
		nodes = append(nodes, node)
		edges[index] = first
		edges = append(edges, second)
		inserted = []job.Edge{first, second}
	}

	updatedGraph, err := job.NewGraph(nodes, edges, requested.Mappings()...)
	if err != nil {
		return formatInsertion{}, err
	}
	updated, err := job.New(request.Inputs(), request.Outputs(), updatedGraph, job.WithPolicy(request.Policy()), job.WithBudget(request.Budget()))
	if err != nil {
		return formatInsertion{}, err
	}
	return formatInsertion{request: updated, node: node, replaced: replaced, inserted: inserted}, nil
}

func formatBoundaryEdge(edges []job.Edge, boundary plan.Boundary, component plugin.Identity) (job.Edge, int, error) {
	index := -1
	var selected job.Edge
	for candidate, edge := range edges {
		matches := boundary.Direction == plan.InputBoundary && edge.From() == job.At(job.NodeID(boundary.Node), boundary.Port) ||
			boundary.Direction == plan.OutputBoundary && edge.To() == job.At(job.NodeID(boundary.Node), boundary.Port)
		if !matches {
			continue
		}
		if index >= 0 {
			return job.Edge{}, -1, probeDiagnostic("prepare.probe-edge", boundary, component, "automatic Format boundary has multiple adjacent edges", nil)
		}
		selected = edge
		index = candidate
	}
	if index < 0 {
		return job.Edge{}, -1, probeDiagnostic("prepare.probe-edge", boundary, component, "automatic Format boundary edge is missing", nil)
	}
	return selected, index, nil
}

func removeCarrierNode(nodes []job.Node, edges []job.Edge, carrier job.NodeID, component plugin.Identity, boundary plan.Boundary) ([]job.Node, error) {
	result := make([]job.Node, 0, len(nodes)-1)
	found := false
	for _, node := range nodes {
		if node.ID() != carrier {
			result = append(result, node)
			continue
		}
		found = true
		if node.Component().String() != boundary.Component {
			return nil, probeDiagnostic("prepare.format-direct-boundary", boundary, component, "direct Format reader cannot replace a non-provider boundary node", nil)
		}
	}
	if !found {
		return nil, probeDiagnostic("prepare.format-direct-boundary", boundary, component, "direct Format reader boundary node is missing", nil)
	}
	for _, edge := range edges {
		if edge.To().Node() == carrier {
			return nil, probeDiagnostic("prepare.format-direct-boundary", boundary, component, "direct Format reader boundary carrier has an incoming edge", nil)
		}
	}
	return result, nil
}

func formatShapeError(code string, boundary plan.Boundary, component plugin.Component, message string) error {
	shape := component.Ports()
	return probeDiagnostic(code, boundary, component.Identity(), message, map[string]string{
		"inputs": strconv.Itoa(len(shape.Inputs)), "outputs": strconv.Itoa(len(shape.Outputs)),
	})
}

func selectedFormatNodeID(edge job.Edge, identity plugin.Identity, used map[job.NodeID]struct{}) job.NodeID {
	base := edge.From().String() + "->" + edge.To().String() + "\x00" + identity.String()
	for nonce := 0; ; nonce++ {
		digest := sha256.Sum256([]byte("godec/format-selection/v1\x00" + base + "\x00" + strconv.Itoa(nonce)))
		id := job.NodeID("format-" + hex.EncodeToString(digest[:8]))
		if _, exists := used[id]; !exists {
			return id
		}
	}
}

func replaceSelectedEdge(values []solve.SelectedEdge, replaced job.Edge, inserted []job.Edge, reason string) []solve.SelectedEdge {
	result := make([]solve.SelectedEdge, 0, len(values)+len(inserted))
	for _, value := range values {
		if value.Edge != replaced {
			result = append(result, value)
		}
	}
	for _, edge := range inserted {
		result = append(result, solve.SelectedEdge{Edge: edge, Reason: reason})
	}
	return result
}
