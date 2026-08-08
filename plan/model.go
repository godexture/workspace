package plan

import (
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

type Origin uint8

const (
	Requested Origin = iota + 1
	Automatic
)

func (o Origin) Valid() bool { return o == Requested || o == Automatic }

// Node describes one selected component implementation without retaining its
// compiled plan or resolved typed config.
type Node struct {
	ID           string
	Origin       Origin
	Component    string
	DisplayName  string
	Variant      string
	Version      string
	Config       config.Summary
	Inputs       []PortDescriptor
	Outputs      []PortDescriptor
	Reason       string
	Effects      []plugin.Effect
	Contract     plugin.Contract
	Resources    resource.Request
	Estimate     resource.Estimate
	Finalization plugin.Finalization
}

type Edge struct {
	FromNode string
	FromPort string
	ToNode   string
	ToPort   string
	Origin   Origin
	Reason   string
}

func (e Edge) Valid() bool {
	return e.FromNode != "" && e.FromPort != "" && e.ToNode != "" && e.ToPort != "" && e.Origin.Valid() && (e.Origin != Automatic || e.Reason != "")
}

// Usage records deterministic planner work. Duration is intentionally absent
// because wall-clock timing cannot participate in reproducible selection.
type Usage struct {
	States             int
	Compiles           int
	Suggestions        int
	FixpointIterations int
	CacheHits          int
}

func (u Usage) valid(budget job.Budget) bool {
	return u.States >= 0 && u.Compiles >= 0 && u.Suggestions >= 0 && u.FixpointIterations >= 0 && u.CacheHits >= 0 &&
		u.States <= budget.States && u.Compiles <= budget.Compiles && u.FixpointIterations <= budget.FixpointIterations
}

type Platform struct {
	OS        string
	Arch      string
	Toolchain string
	Features  []string
}

func (p Platform) Valid() bool { return p.OS != "" && p.Arch != "" && p.Toolchain != "" }

// Description is the DTO-ready, inert input used to build a Plan.
type Description struct {
	RequestedPolicy    job.Policy
	EffectivePolicy    job.Policy
	Budget             job.Budget
	Usage              Usage
	CatalogFingerprint string
	Platform           Platform
	Nodes              []Node
	Edges              []Edge
	Boundaries         []Boundary
	Warnings           []string
}

func cloneNode(node Node) Node {
	node.Inputs = append([]PortDescriptor(nil), node.Inputs...)
	node.Outputs = append([]PortDescriptor(nil), node.Outputs...)
	node.Effects = append([]plugin.Effect(nil), node.Effects...)
	features := node.Contract.Platform.Features
	node.Contract.Platform.Features = append([]string(nil), features...)
	return node
}

func cloneDescription(description Description) Description {
	description.Nodes = append([]Node(nil), description.Nodes...)
	for index := range description.Nodes {
		description.Nodes[index] = cloneNode(description.Nodes[index])
	}
	description.Edges = append([]Edge(nil), description.Edges...)
	description.Boundaries = append([]Boundary(nil), description.Boundaries...)
	for index := range description.Boundaries {
		description.Boundaries[index] = cloneBoundary(description.Boundaries[index])
	}
	description.Warnings = append([]string(nil), description.Warnings...)
	description.Platform.Features = append([]string(nil), description.Platform.Features...)
	return description
}
