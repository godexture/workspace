package host

import (
	"context"
	"errors"
	"strconv"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/internal/bind"
	"github.com/godexture/godec/internal/bound"
	"github.com/godexture/godec/internal/graph"
	"github.com/godexture/godec/internal/solve"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

type inputSelection struct {
	request   job.Job
	entries   []bound.Entry
	sessions  []acquiredSession
	stores    []*probeStore
	inspected []inspectedFormat
	contexts  graph.CompileContexts
	nodes     []solve.SelectedNode
	edges     []solve.SelectedEdge
	formats   []solve.SelectedFormat
	warnings  []string
	usage     plan.Usage
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
		if !entry.Pending() || entry.Projection().Direction != plan.InputBoundary {
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

		insertion, err := insertInputFormat(result.request, projection, choice.component, choice.config)
		if err != nil {
			return fail(err)
		}
		resolvedEntry, selection, err := bind.FinalizeInput(entry, insertion.node, choice.component, result.sessions[sessionIndex].actual)
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
		if anchor := resolvedEntry.Anchor(); anchor.Valid() {
			result.sessions[sessionIndex].node = anchor.String()
		}
		result.entries[entryIndex] = resolvedEntry
		result.request = insertion.request

		reason := "format.probe"
		if choice.fallback {
			reason = "format.fallback"
			basis := "explicit " + hint.String()
			if choice.configured {
				basis += " and media configuration"
			}
			warnings = append(warnings, "input "+strconv.Itoa(projection.Choice)+" selected "+choice.trait.Format().Identity().String()+" without content evidence through "+basis)
		}
		nodes = append(nodes, solve.SelectedNode{ID: insertion.node.ID(), Reason: reason, InferConfig: !choice.configured})
		for _, edge := range insertion.inserted {
			edges = append(edges, solve.SelectedEdge{Edge: edge, Reason: reason})
		}
	}
	result.nodes = nodes
	result.edges = edges
	result.warnings = warnings
	result.usage = usage
	return result, nil
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
