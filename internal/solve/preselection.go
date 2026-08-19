package solve

import (
	"errors"
	"sort"
	"strings"

	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plan"
)

type SelectedNode struct {
	ID          job.NodeID
	Reason      string
	InferConfig bool
}

type SelectedEdge struct {
	Edge   job.Edge
	Reason string
}

// SelectedFormat identifies the Format component Host resolved at one Job
// boundary. Keeping this fact explicit avoids rediscovering boundary topology
// after solver insertion.
type SelectedFormat struct {
	Direction plan.BoundaryDirection
	Choice    int
	Node      job.NodeID
}

// Preselection carries only Format choices made before solver gap filling.
// It is internal so caller-owned Job nodes remain explicitly requested.
type Preselection struct {
	nodes    map[job.NodeID]selectedNode
	edges    map[string]string
	formats  map[formatBoundary]job.NodeID
	warnings []string
	usage    plan.Usage
}

type formatBoundary struct {
	direction plan.BoundaryDirection
	choice    int
}

type selectedNode struct {
	reason      string
	inferConfig bool
}

func NewPreselection(nodes []SelectedNode, edges []SelectedEdge, formats []SelectedFormat, warnings []string, usage plan.Usage) (Preselection, error) {
	result := Preselection{
		nodes:    make(map[job.NodeID]selectedNode, len(nodes)),
		edges:    make(map[string]string, len(edges)),
		formats:  make(map[formatBoundary]job.NodeID, len(formats)),
		warnings: append([]string(nil), warnings...),
		usage:    usage,
	}
	for _, value := range nodes {
		value.Reason = strings.TrimSpace(value.Reason)
		if !value.ID.Valid() || value.Reason == "" {
			return Preselection{}, errors.New("preselected node requires an identity and reason")
		}
		if _, exists := result.nodes[value.ID]; exists {
			return Preselection{}, errors.New("preselected node is repeated")
		}
		result.nodes[value.ID] = selectedNode{reason: value.Reason, inferConfig: value.InferConfig}
	}
	for _, value := range edges {
		value.Reason = strings.TrimSpace(value.Reason)
		if !value.Edge.Valid() || value.Reason == "" {
			return Preselection{}, errors.New("preselected edge requires an edge and reason")
		}
		key := edgeKey(value.Edge)
		if _, exists := result.edges[key]; exists {
			return Preselection{}, errors.New("preselected edge is repeated")
		}
		result.edges[key] = value.Reason
	}
	for _, value := range formats {
		if !value.Direction.Valid() || value.Choice < 0 || !value.Node.Valid() {
			return Preselection{}, errors.New("preselected Format requires a boundary and node identity")
		}
		key := formatBoundary{direction: value.Direction, choice: value.Choice}
		if _, exists := result.formats[key]; exists {
			return Preselection{}, errors.New("preselected Format boundary is repeated")
		}
		result.formats[key] = value.Node
	}
	for _, warning := range result.warnings {
		if strings.TrimSpace(warning) == "" {
			return Preselection{}, errors.New("preselection warning must not be empty")
		}
	}
	sort.Strings(result.warnings)
	return result, nil
}

func (s Preselection) validFor(graph job.Graph, boundaries []plan.Boundary) bool {
	nodes := make(map[job.NodeID]struct{}, len(graph.Nodes()))
	for _, node := range graph.Nodes() {
		nodes[node.ID()] = struct{}{}
	}
	for id := range s.nodes {
		if _, ok := nodes[id]; !ok {
			return false
		}
	}
	edges := make(map[string]struct{}, len(graph.Edges()))
	for _, edge := range graph.Edges() {
		edges[edgeKey(edge)] = struct{}{}
	}
	for key := range s.edges {
		if _, ok := edges[key]; !ok {
			return false
		}
	}
	boundaryKeys := make(map[formatBoundary]struct{}, len(boundaries))
	for _, boundary := range boundaries {
		boundaryKeys[formatBoundary{direction: boundary.Direction, choice: boundary.Choice}] = struct{}{}
	}
	for key, id := range s.formats {
		if _, ok := nodes[id]; !ok {
			return false
		}
		if _, ok := boundaryKeys[key]; !ok {
			return false
		}
	}
	return true
}
