// Package bound owns private Job boundary state shared by binding and runtime.
package bound

import (
	"github.com/godexture/godec/access"
	"github.com/godexture/godec/endpoint"
	"github.com/godexture/godec/plan"
)

type Entry struct {
	projection plan.Boundary
	reference  access.Reference
	provider   access.Provider
	trait      endpoint.Trait
}

func Provider(projection plan.Boundary, reference access.Reference, provider access.Provider) Entry {
	return Entry{projection: cloneProjection(projection), reference: reference, provider: provider}
}

func Endpoint(projection plan.Boundary, trait endpoint.Trait) Entry {
	return Entry{projection: cloneProjection(projection), trait: trait}
}

func (e Entry) Valid() bool {
	if !e.projection.Valid() {
		return false
	}
	switch e.projection.Kind {
	case plan.ProviderBoundary:
		return e.reference.Valid() && e.provider.Valid() && !e.trait.Valid()
	case plan.EndpointBoundary:
		return !e.reference.Valid() && !e.provider.Valid() && e.trait.Valid()
	default:
		return false
	}
}

func (e Entry) Projection() plan.Boundary     { return cloneProjection(e.projection) }
func (e Entry) Reference() access.Reference   { return e.reference }
func (e Entry) Provider() access.Provider     { return e.provider }
func (e Entry) EndpointTrait() endpoint.Trait { return e.trait }

type State struct{ entries []Entry }

func New(entries ...Entry) State {
	return State{entries: append([]Entry(nil), entries...)}
}

func (s State) Valid() bool {
	seen := make(map[[2]int]struct{}, len(s.entries))
	for _, entry := range s.entries {
		if !entry.Valid() {
			return false
		}
		projection := entry.projection
		key := [2]int{int(projection.Direction), projection.Choice}
		if _, exists := seen[key]; exists {
			return false
		}
		seen[key] = struct{}{}
	}
	return true
}

func (s State) Entries() []Entry { return append([]Entry(nil), s.entries...) }

func (s State) Projections() []plan.Boundary {
	result := make([]plan.Boundary, len(s.entries))
	for index, entry := range s.entries {
		result[index] = entry.Projection()
	}
	return result
}

func cloneProjection(value plan.Boundary) plan.Boundary {
	value.Available = append([]access.Capability(nil), value.Available...)
	value.Selected = append([]access.Capability(nil), value.Selected...)
	return value
}
