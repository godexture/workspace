package solve

import (
	"errors"
	"sort"
	"strings"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

type SelectedNode struct {
	ID     job.NodeID
	Reason string
}

type SelectedEdge struct {
	Edge   job.Edge
	Reason string
}

// TerminalSelection constrains only the component that completes one output
// boundary gap. Intermediate bridge candidates remain unrestricted.
type TerminalSelection struct {
	Boundary   job.Port
	Component  plugin.Identity
	Config     config.Patch
	Configured bool
	Context    plugin.CompileContext
	Reason     string
}

type terminalSelection struct {
	boundary   job.Port
	component  plugin.Identity
	config     config.Patch
	configured bool
	context    plugin.CompileContext
	reason     string
}

// Preselection carries only Format choices made before solver gap filling.
// It is internal so caller-owned Job nodes remain explicitly requested.
type Preselection struct {
	nodes     map[job.NodeID]string
	edges     map[string]string
	terminals map[string]terminalSelection
	warnings  []string
	usage     plan.Usage
}

func NewPreselection(nodes []SelectedNode, edges []SelectedEdge, warnings []string, usage plan.Usage) (Preselection, error) {
	result := Preselection{
		nodes:     make(map[job.NodeID]string, len(nodes)),
		edges:     make(map[string]string, len(edges)),
		terminals: make(map[string]terminalSelection),
		warnings:  append([]string(nil), warnings...),
		usage:     usage,
	}
	for _, value := range nodes {
		value.Reason = strings.TrimSpace(value.Reason)
		if !value.ID.Valid() || value.Reason == "" {
			return Preselection{}, errors.New("preselected node requires an identity and reason")
		}
		if _, exists := result.nodes[value.ID]; exists {
			return Preselection{}, errors.New("preselected node is repeated")
		}
		result.nodes[value.ID] = value.Reason
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
	for _, warning := range result.warnings {
		if strings.TrimSpace(warning) == "" {
			return Preselection{}, errors.New("preselection warning must not be empty")
		}
	}
	sort.Strings(result.warnings)
	return result, nil
}

func (s Preselection) WithTerminals(values ...TerminalSelection) (Preselection, error) {
	result := s.clone()
	if result.terminals == nil {
		result.terminals = make(map[string]terminalSelection, len(values))
	}
	for _, value := range values {
		value.Reason = strings.TrimSpace(value.Reason)
		if !value.Boundary.Valid() || value.Component.IsZero() || value.Reason == "" {
			return Preselection{}, errors.New("terminal selection requires a boundary, component, and reason")
		}
		if !value.Configured && (value.Config.PresetName() != "" || len(value.Config.FieldIDs()) != 0) {
			return Preselection{}, errors.New("unconfigured terminal selection carries config")
		}
		key := value.Boundary.String()
		if _, exists := result.terminals[key]; exists {
			return Preselection{}, errors.New("terminal selection is repeated")
		}
		result.terminals[key] = terminalSelection{
			boundary: value.Boundary, component: value.Component, config: value.Config.Planned(), configured: value.Configured, context: value.Context, reason: value.Reason,
		}
	}
	return result, nil
}

func (s Preselection) clone() Preselection {
	result := Preselection{
		nodes:     make(map[job.NodeID]string, len(s.nodes)),
		edges:     make(map[string]string, len(s.edges)),
		terminals: make(map[string]terminalSelection, len(s.terminals)),
		warnings:  append([]string(nil), s.warnings...),
		usage:     s.usage,
	}
	for key, value := range s.nodes {
		result.nodes[key] = value
	}
	for key, value := range s.edges {
		result.edges[key] = value
	}
	for key, value := range s.terminals {
		value.config = value.config.Clone()
		result.terminals[key] = value
	}
	return result
}

func (s Preselection) validFor(graph job.Graph) bool {
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
	for key, terminal := range s.terminals {
		if terminal.boundary.String() != key {
			return false
		}
		if _, ok := nodes[terminal.boundary.Node()]; !ok {
			return false
		}
		connected := false
		for _, edge := range graph.Edges() {
			if edge.To() == terminal.boundary {
				connected = true
				break
			}
		}
		if !connected || terminal.component.IsZero() || strings.TrimSpace(terminal.reason) == "" {
			return false
		}
	}
	return true
}
