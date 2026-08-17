// Package plan describes a selected execution without granting the ability to
// run it. Executable typed state remains in an internal Program.
package plan

import (
	"encoding/json"
	"errors"
	"sort"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/endpoint"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

type Plan struct {
	description Description
	execution   Fingerprint
	fingerprint Fingerprint
}

// New validates and snapshots an inert execution description.
func New(description Description) (Plan, error) {
	description = cloneDescription(description)
	description.Runtime = normalizeRuntime(description.Runtime)
	sort.Strings(description.Platform.Features)
	sort.Slice(description.Edges, func(left, right int) bool {
		return edgeKey(description.Edges[left]) < edgeKey(description.Edges[right])
	})
	sort.Slice(description.Boundaries, func(left, right int) bool {
		if description.Boundaries[left].Direction != description.Boundaries[right].Direction {
			return description.Boundaries[left].Direction < description.Boundaries[right].Direction
		}
		return description.Boundaries[left].Choice < description.Boundaries[right].Choice
	})
	if err := validate(description); err != nil {
		return Plan{}, err
	}
	executionCanonical, err := json.Marshal(canonicalExecutionOf(description))
	if err != nil {
		return Plan{}, err
	}
	execution := fingerprint("godec/plan/execution/v1", executionCanonical)
	planCanonical, err := json.Marshal(canonicalPlanOf(description, execution))
	if err != nil {
		return Plan{}, err
	}
	return Plan{
		description: description,
		execution:   execution,
		fingerprint: fingerprint("godec/plan/fingerprint/v1", planCanonical),
	}, nil
}

func (p Plan) Valid() bool                     { return !p.fingerprint.IsZero() && !p.execution.IsZero() }
func (p Plan) Fingerprint() Fingerprint        { return p.fingerprint }
func (p Plan) ExecutionSignature() Fingerprint { return p.execution }
func (p Plan) Description() Description        { return cloneDescription(p.description) }
func (p Plan) Nodes() []Node                   { return p.Description().Nodes }
func (p Plan) Edges() []Edge                   { return p.Description().Edges }
func (p Plan) RequestedPolicy() job.Policy     { return p.description.RequestedPolicy }
func (p Plan) EffectivePolicy() job.Policy     { return p.description.EffectivePolicy }
func (p Plan) Budget() job.Budget              { return p.description.Budget }
func (p Plan) Usage() Usage                    { return p.description.Usage }
func (p Plan) CatalogFingerprint() string      { return p.description.CatalogFingerprint }
func (p Plan) Platform() Platform              { return p.Description().Platform }
func (p Plan) Boundaries() []Boundary          { return p.Description().Boundaries }
func (p Plan) Runtime() Runtime                { return cloneRuntime(p.description.Runtime) }
func (p Plan) Warnings() []string              { return append([]string(nil), p.description.Warnings...) }

func validate(description Description) error {
	if !description.RequestedPolicy.Valid() || !description.EffectivePolicy.Valid() {
		return errors.New("plan policy is invalid")
	}
	if !description.Budget.Valid() {
		return errors.New("plan budget is invalid")
	}
	if !description.Usage.valid(description.Budget) {
		return errors.New("plan budget usage is invalid")
	}
	if description.CatalogFingerprint == "" || !description.Platform.Valid() {
		return errors.New("plan catalog or platform snapshot is invalid")
	}
	if len(description.Nodes) == 0 {
		return errors.New("plan has no nodes")
	}
	seen := make(map[string]struct{}, len(description.Nodes))
	for _, node := range description.Nodes {
		if node.ID == "" || !node.Origin.Valid() || node.Component == "" || node.Variant == "" || node.Version == "" || !node.Config.Valid() || !node.Contract.Valid() || !node.Estimate.Valid() || !node.Finalization.Valid() || node.Origin == Automatic && node.Reason == "" {
			return errors.New("plan contains an invalid node")
		}
		if _, exists := seen[node.ID]; exists {
			return errors.New("plan contains duplicate node IDs")
		}
		seen[node.ID] = struct{}{}
		for _, descriptor := range append(append([]PortDescriptor(nil), node.Inputs...), node.Outputs...) {
			if !descriptor.Valid() {
				return errors.New("plan contains an invalid descriptor projection")
			}
		}
		for _, effect := range node.Effects {
			if !effect.Valid() {
				return errors.New("plan contains an invalid effect")
			}
		}
	}
	for _, edge := range description.Edges {
		if !edge.Valid() {
			return errors.New("plan contains an invalid edge")
		}
		if _, ok := seen[edge.FromNode]; !ok {
			return errors.New("plan edge source is absent")
		}
		if _, ok := seen[edge.ToNode]; !ok {
			return errors.New("plan edge destination is absent")
		}
	}
	seenBoundaries := make(map[[2]int]struct{}, len(description.Boundaries))
	for _, boundary := range description.Boundaries {
		if !boundary.Valid() {
			return errors.New("plan contains an invalid boundary binding")
		}
		key := [2]int{int(boundary.Direction), boundary.Choice}
		if _, exists := seenBoundaries[key]; exists {
			return errors.New("plan contains duplicate boundary choices")
		}
		seenBoundaries[key] = struct{}{}
		if _, ok := seen[boundary.Node]; !ok {
			return errors.New("plan boundary node is absent")
		}
	}
	if err := validateRuntime(description.Runtime, seen, description.Edges); err != nil {
		return err
	}
	previous := ""
	for _, feature := range description.Platform.Features {
		if feature == "" || feature == previous {
			return errors.New("plan platform features are invalid")
		}
		previous = feature
	}
	return nil
}

func edgeKey(edge Edge) string {
	return edge.FromNode + ":" + edge.FromPort + "->" + edge.ToNode + ":" + edge.ToPort
}

type canonicalConfig struct {
	Schema      string
	Version     string
	Fingerprint string
}

type canonicalNode struct {
	ID           string
	Component    string
	Variant      string
	Version      string
	Config       canonicalConfig
	Inputs       []PortDescriptor
	Outputs      []PortDescriptor
	Effects      []plugin.Effect
	Contract     plugin.Contract
	Resources    resource.Request
	Estimate     resource.Estimate
	Finalization plugin.Finalization
}

type canonicalExecution struct {
	Catalog    string
	Policy     job.Policy
	Platform   Platform
	Nodes      []canonicalNode
	Edges      []Edge
	Boundaries []canonicalBoundary
	Runtime    Runtime
}

type canonicalBoundary struct {
	Direction            BoundaryDirection
	Kind                 BoundaryKind
	Choice               int
	Node                 string
	Port                 string
	Component            string
	Scheme               string
	ReferenceFingerprint string
	Available            []access.Capability
	Effective            []access.Capability
	Selected             []access.Capability
	Spool                canonicalSpool
	Topology             endpoint.Topology
	Mode                 endpoint.Mode
	Ownership            access.Ownership
}

type canonicalSpool struct {
	Present        bool
	MaximumBytes   int64
	PredictedBytes int64
	Storage        access.SpoolStorage
	StartupLatency int64
	FinalCopy      bool
	Rollback       access.TransactionClass
}

type canonicalPlan struct {
	Execution       string
	RequestedPolicy job.Policy
	Budget          job.Budget
	Usage           Usage
	Origins         []Origin
	Reasons         []string
	EdgeOrigins     []Origin
	EdgeReasons     []string
}

func canonicalExecutionOf(description Description) canonicalExecution {
	nodes := make([]canonicalNode, len(description.Nodes))
	for index, node := range description.Nodes {
		nodes[index] = canonicalNode{
			ID:           node.ID,
			Component:    node.Component,
			Variant:      node.Variant,
			Version:      node.Version,
			Config:       canonicalConfig{Schema: node.Config.Schema(), Version: node.Config.Version(), Fingerprint: node.Config.Fingerprint().String()},
			Inputs:       append([]PortDescriptor(nil), node.Inputs...),
			Outputs:      append([]PortDescriptor(nil), node.Outputs...),
			Effects:      append([]plugin.Effect(nil), node.Effects...),
			Contract:     node.Contract,
			Resources:    node.Resources,
			Estimate:     node.Estimate,
			Finalization: node.Finalization,
		}
	}
	edges := append([]Edge(nil), description.Edges...)
	for index := range edges {
		edges[index].Origin = 0
		edges[index].Reason = ""
	}
	boundaries := make([]canonicalBoundary, len(description.Boundaries))
	for index, boundary := range description.Boundaries {
		boundaries[index] = canonicalBoundary{
			Direction:            boundary.Direction,
			Kind:                 boundary.Kind,
			Choice:               boundary.Choice,
			Node:                 boundary.Node,
			Port:                 boundary.Port,
			Component:            boundary.Component,
			Scheme:               boundary.Scheme,
			ReferenceFingerprint: boundary.ReferenceFingerprint,
			Available:            append([]access.Capability(nil), boundary.Available...),
			Effective:            append([]access.Capability(nil), boundary.Effective...),
			Selected:             append([]access.Capability(nil), boundary.Selected...),
			Spool:                canonicalSpoolOf(boundary.Spool),
			Topology:             boundary.Topology,
			Mode:                 boundary.Mode,
			Ownership:            boundary.Ownership,
		}
	}
	return canonicalExecution{Catalog: description.CatalogFingerprint, Policy: description.EffectivePolicy, Platform: description.Platform, Nodes: nodes, Edges: edges, Boundaries: boundaries, Runtime: cloneRuntime(description.Runtime)}
}

func canonicalSpoolOf(value access.SpoolSpec) canonicalSpool {
	if value.IsZero() {
		return canonicalSpool{}
	}
	return canonicalSpool{
		Present:        true,
		MaximumBytes:   value.MaximumBytes(),
		PredictedBytes: value.PredictedBytes(),
		Storage:        value.Storage(),
		StartupLatency: int64(value.StartupLatency()),
		FinalCopy:      value.FinalCopy(),
		Rollback:       value.RollbackClass(),
	}
}

func validateRuntime(runtime Runtime, nodes map[string]struct{}, edges []Edge) error {
	if !runtime.Executable {
		if len(runtime.Islands) != 0 || len(runtime.Buffers) != 0 || len(runtime.FanIns) != 0 {
			return errors.New("non-executable plan contains runtime topology")
		}
		return nil
	}
	if len(runtime.Islands) == 0 {
		return errors.New("executable plan has no execution islands")
	}
	covered := make(map[string]struct{}, len(nodes))
	islandIDs := make(map[string]struct{}, len(runtime.Islands))
	for _, island := range runtime.Islands {
		if island.ID == "" || len(island.Nodes) == 0 {
			return errors.New("plan contains an invalid execution island")
		}
		if _, exists := islandIDs[island.ID]; exists {
			return errors.New("plan contains duplicate execution island IDs")
		}
		islandIDs[island.ID] = struct{}{}
		for _, node := range island.Nodes {
			if _, exists := nodes[node]; !exists {
				return errors.New("execution island names an absent node")
			}
			if _, exists := covered[node]; exists {
				return errors.New("execution islands overlap")
			}
			covered[node] = struct{}{}
		}
	}
	if len(covered) != len(nodes) {
		return errors.New("execution islands do not cover every node")
	}
	edgeSet := make(map[string]struct{}, len(edges))
	for _, edge := range edges {
		edgeSet[edgeKey(edge)] = struct{}{}
	}
	bufferIDs := make(map[string]struct{}, len(runtime.Buffers))
	for _, buffer := range runtime.Buffers {
		if buffer.ID == "" || !buffer.Limit.Valid() || !buffer.Reason.Valid() {
			return errors.New("plan contains an invalid runtime buffer")
		}
		if _, exists := bufferIDs[buffer.ID]; exists {
			return errors.New("plan contains duplicate runtime buffer IDs")
		}
		bufferIDs[buffer.ID] = struct{}{}
		key := buffer.FromNode + ":" + buffer.FromPort + "->" + buffer.ToNode + ":" + buffer.ToPort
		if _, exists := edgeSet[key]; !exists {
			return errors.New("runtime buffer does not correspond to a plan edge")
		}
	}
	fanInPorts := make(map[string]struct{}, len(runtime.FanIns))
	for _, fanIn := range runtime.FanIns {
		if fanIn.Node == "" || fanIn.Port == "" || !fanIn.Policy.Valid() || len(fanIn.Connections) < 2 || !fanIn.Limit.Valid() || fanIn.Tolerance < 0 {
			return errors.New("plan contains an invalid fan-in projection")
		}
		if _, exists := nodes[fanIn.Node]; !exists {
			return errors.New("fan-in names an absent node")
		}
		key := fanIn.Node + ":" + fanIn.Port
		if _, exists := fanInPorts[key]; exists {
			return errors.New("plan contains duplicate fan-in ports")
		}
		fanInPorts[key] = struct{}{}
		previous := ""
		for _, connection := range fanIn.Connections {
			connectionKey := connection.FromNode + ":" + connection.FromPort
			if connection.FromNode == "" || connection.FromPort == "" || connectionKey <= previous {
				return errors.New("fan-in connection order is not canonical")
			}
			previous = connectionKey
			if _, exists := edgeSet[connectionKey+"->"+key]; !exists {
				return errors.New("fan-in connection does not correspond to a plan edge")
			}
		}
	}
	return nil
}

func canonicalPlanOf(description Description, execution Fingerprint) canonicalPlan {
	result := canonicalPlan{
		Execution:       execution.String(),
		RequestedPolicy: description.RequestedPolicy,
		Budget:          description.Budget,
		Usage:           description.Usage,
		Origins:         make([]Origin, len(description.Nodes)),
		Reasons:         make([]string, len(description.Nodes)),
		EdgeOrigins:     make([]Origin, len(description.Edges)),
		EdgeReasons:     make([]string, len(description.Edges)),
	}
	for index, node := range description.Nodes {
		result.Origins[index] = node.Origin
		result.Reasons[index] = node.Reason
	}
	for index, edge := range description.Edges {
		result.EdgeOrigins[index] = edge.Origin
		result.EdgeReasons[index] = edge.Reason
	}
	return result
}
