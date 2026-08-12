package host

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/internal/bind"
	"github.com/godexture/godec/internal/bound"
	"github.com/godexture/godec/internal/solve"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

type inputSelection struct {
	request      job.Job
	entries      []bound.Entry
	sessions     []acquiredSession
	stores       []*probeStore
	preselection solve.Preselection
}

func (h *Host) selectInputFormats(ctx context.Context, request job.Job, entries []bound.Entry, sessions []acquiredSession) (inputSelection, error) {
	result := inputSelection{
		request:  request,
		entries:  append([]bound.Entry(nil), entries...),
		sessions: append([]acquiredSession(nil), sessions...),
	}
	var nodes []solve.SelectedNode
	var edges []solve.SelectedEdge
	var warnings []string
	usage := plan.Usage{}
	fail := func(err error) (inputSelection, error) {
		return inputSelection{}, errors.Join(err, closeProbeStores(result.stores))
	}

	for entryIndex, entry := range result.entries {
		if !entry.Pending() {
			continue
		}
		projection := entry.Projection()
		sessionIndex := sessionIndex(result.sessions, projection.Node)
		if sessionIndex < 0 {
			return fail(probeDiagnostic("prepare.probe-session", projection, plugin.Identity{}, "automatic Format selection has no acquired input session", nil))
		}
		var hint job.FormatSelector
		inputs := result.request.Inputs()
		if projection.Choice >= 0 && projection.Choice < len(inputs) {
			hint, _ = inputs[projection.Choice].FormatHint()
		}
		choice, store, probeUsage, err := h.probeInput(ctx, projection, result.sessions[sessionIndex], hint, request.Budget())
		if err != nil {
			return fail(err)
		}
		result.stores = append(result.stores, store)
		usage.ProbeBytes += probeUsage.ProbeBytes
		usage.ProbeRounds += probeUsage.ProbeRounds
		if usage.ProbeBytes > request.Budget().ProbeBytes || usage.ProbeRounds > request.Budget().ProbeRounds {
			return fail(probeBudgetDiagnostic(projection, "job", usage, request.Budget(), nil))
		}

		updated, node, insertedEdges, err := insertFormatNode(result.request, projection, choice.component, choice.config)
		if err != nil {
			return fail(err)
		}
		resolvedEntry, selection, err := bind.FinalizeInput(entry, node, choice.component, result.sessions[sessionIndex].actual)
		if err != nil {
			return fail(err)
		}
		sessionValue := result.sessions[sessionIndex].value
		if store.sequential != nil && store.random == nil {
			sessionValue, err = store.ReplaySession(sessionValue)
			if err != nil {
				return fail(err)
			}
			result.sessions[sessionIndex].value = sessionValue
		}
		opening, err := access.NewOpening(access.SourceDirection, sessionValue, selection, 0)
		if err != nil {
			return fail(sessionDiagnostic("prepare.format-view", resolvedEntry.Projection(), "Access session cannot provide the selected Format view", map[string]string{"error": err.Error()}))
		}
		result.sessions[sessionIndex].selected = selection
		result.sessions[sessionIndex].opening = opening
		result.entries[entryIndex] = resolvedEntry
		result.request = updated

		reason := "format.probe"
		if choice.fallback {
			reason = "format.fallback"
			basis := "explicit " + formatSelectorLabel(hint)
			if choice.configured {
				basis += " and media configuration"
			}
			warnings = append(warnings, "input "+strconv.Itoa(projection.Choice)+" selected "+choice.trait.Format().Identity().String()+" without content evidence through "+basis)
		}
		nodes = append(nodes, solve.SelectedNode{ID: node.ID(), Reason: reason})
		for _, edge := range insertedEdges {
			edges = append(edges, solve.SelectedEdge{Edge: edge, Reason: reason})
		}
	}

	selected, err := solve.NewPreselection(nodes, edges, warnings, usage)
	if err != nil {
		return fail(err)
	}
	result.preselection = selected
	return result, nil
}

func insertFormatNode(request job.Job, boundary plan.Boundary, component plugin.Component, patch config.Patch) (job.Job, job.Node, []job.Edge, error) {
	graph, ok := request.Graph()
	if !ok {
		return job.Job{}, job.Node{}, nil, errors.New("automatic Format selection requires a normalized graph")
	}
	resolved, err := component.Resolve(patch)
	if err != nil {
		return job.Job{}, job.Node{}, nil, err
	}
	shape, err := component.Shape(plugin.ShapeContext{}, resolved)
	if err != nil {
		return job.Job{}, job.Node{}, nil, err
	}
	if len(shape.Inputs) != 1 || len(shape.Outputs) != 1 {
		return job.Job{}, job.Node{}, nil, probeDiagnostic("prepare.probe-shape", boundary, component.Identity(), "automatically selected Format must expose one input and one output in M6", map[string]string{
			"inputs": strconv.Itoa(len(shape.Inputs)), "outputs": strconv.Itoa(len(shape.Outputs)),
		})
	}

	nodes := graph.Nodes()
	edges := graph.Edges()
	used := make(map[job.NodeID]struct{}, len(nodes)+1)
	for _, node := range nodes {
		used[node.ID()] = struct{}{}
	}
	var replaced job.Edge
	replacedIndex := -1
	for index, edge := range edges {
		if edge.From().Node().String() == boundary.Node && edge.From().ID() == boundary.Port {
			if replacedIndex >= 0 {
				return job.Job{}, job.Node{}, nil, probeDiagnostic("prepare.probe-edge", boundary, component.Identity(), "automatic Format boundary has multiple outgoing edges", nil)
			}
			replaced = edge
			replacedIndex = index
		}
	}
	if replacedIndex < 0 {
		return job.Job{}, job.Node{}, nil, probeDiagnostic("prepare.probe-edge", boundary, component.Identity(), "automatic Format boundary edge is missing", nil)
	}
	id := selectedFormatNodeID(replaced, component.Identity(), used)
	node := job.NewNode(id, component.Identity(), patch)
	first := job.Connect(replaced.From(), job.At(id, shape.Inputs[0].ID()))
	second := job.Connect(job.At(id, shape.Outputs[0].ID()), replaced.To())
	nodes = append(nodes, node)
	edges[replacedIndex] = first
	edges = append(edges, second)
	updatedGraph, err := job.NewGraph(nodes, edges, graph.Mappings()...)
	if err != nil {
		return job.Job{}, job.Node{}, nil, err
	}
	updated, err := job.New(request.Inputs(), request.Outputs(), updatedGraph, job.WithPolicy(request.Policy()), job.WithBudget(request.Budget()))
	if err != nil {
		return job.Job{}, job.Node{}, nil, err
	}
	return updated, node, []job.Edge{first, second}, nil
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

func sessionIndex(values []acquiredSession, node string) int {
	for index := range values {
		if values[index].node == node {
			return index
		}
	}
	return -1
}

func closeProbeStores(values []*probeStore) error {
	var failures []error
	for index := len(values) - 1; index >= 0; index-- {
		failures = append(failures, values[index].Close())
	}
	return errors.Join(failures...)
}

func finishProbeStores(values []*probeStore) ([]*probeStore, error) {
	retained := make([]*probeStore, 0, len(values))
	var failures []error
	for _, store := range values {
		if store != nil && store.sequential != nil && store.random == nil {
			retained = append(retained, store)
			continue
		}
		failures = append(failures, store.Close())
	}
	return retained, errors.Join(failures...)
}
