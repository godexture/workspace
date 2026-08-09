package plan

import (
	"github.com/godexture/godec/access"
	"github.com/godexture/godec/endpoint"
)

type BoundaryDirection uint8

const (
	InputBoundary BoundaryDirection = iota + 1
	OutputBoundary
)

func (d BoundaryDirection) Valid() bool { return d == InputBoundary || d == OutputBoundary }

type BoundaryKind uint8

const (
	ProviderBoundary BoundaryKind = iota + 1
	EndpointBoundary
	DirectBoundary
)

func (k BoundaryKind) Valid() bool { return k >= ProviderBoundary && k <= DirectBoundary }

// Boundary is an inert projection of one Job input/output binding. Reference
// carries only a redacted display and a hash of its private canonical form.
type Boundary struct {
	Direction            BoundaryDirection
	Kind                 BoundaryKind
	Choice               int
	Node                 string
	Port                 string
	Component            string
	Scheme               string
	Reference            string
	ReferenceFingerprint string
	Available            []access.Capability
	Effective            []access.Capability
	Selected             []access.Capability
	Spool                access.SpoolSpec
	Topology             endpoint.Topology
	Mode                 endpoint.Mode
	Ownership            access.Ownership
}

func (b Boundary) Valid() bool {
	if !b.Direction.Valid() || !b.Kind.Valid() || b.Choice < 0 || b.Node == "" || b.Port == "" || b.Component == "" {
		return false
	}
	switch b.Kind {
	case ProviderBoundary:
		if b.Scheme == "" || b.Reference == "" || b.ReferenceFingerprint == "" || b.Topology != 0 || b.Mode != 0 || b.Ownership != 0 {
			return false
		}
		if len(b.Available) == 0 || len(b.Effective) == 0 || len(b.Selected) == 0 || !canonicalCapabilities(b.Available) || !canonicalCapabilities(b.Effective) || !canonicalCapabilities(b.Selected) || !subsetCapabilities(b.Selected, b.Effective) {
			return false
		}
		if b.Spool.IsZero() {
			if !equalCapabilities(b.Effective, b.Available) {
				return false
			}
		} else if !b.Spool.Valid() || b.Direction != OutputBoundary || !b.Spool.FinalCopy() || !subsetCapabilities(b.Available, b.Effective) || equalCapabilities(b.Effective, b.Available) {
			return false
		}
	case EndpointBoundary:
		if b.Scheme != "" || b.Reference != "" || b.ReferenceFingerprint != "" || len(b.Available) != 0 || len(b.Effective) != 0 || len(b.Selected) != 0 || !b.Spool.IsZero() || !b.Topology.Valid() || !b.Mode.Valid() || b.Ownership != 0 {
			return false
		}
	case DirectBoundary:
		if b.Scheme != "" || b.Reference != "" || b.ReferenceFingerprint != "" || len(b.Available) != 0 || len(b.Effective) != 0 || len(b.Selected) != 0 || !b.Spool.IsZero() || b.Topology != 0 || b.Mode != 0 || !b.Ownership.Valid() {
			return false
		}
	}
	return true
}

func cloneBoundary(value Boundary) Boundary {
	value.Available = append([]access.Capability(nil), value.Available...)
	value.Effective = append([]access.Capability(nil), value.Effective...)
	value.Selected = append([]access.Capability(nil), value.Selected...)
	return value
}

func equalCapabilities(left, right []access.Capability) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func canonicalCapabilities(values []access.Capability) bool {
	for index, value := range values {
		if !value.Valid() || index != 0 && value <= values[index-1] {
			return false
		}
	}
	return true
}

func subsetCapabilities(values, available []access.Capability) bool {
	for _, value := range values {
		found := false
		for _, candidate := range available {
			if value == candidate {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
