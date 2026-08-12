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
	source     access.SourceTrait
	sink       access.SinkTrait
	trait      endpoint.Trait
	direct     any
	close      func() error
	automatic  bool
	resolved   bool
}

func Source(projection plan.Boundary, reference access.Reference, trait access.SourceTrait) Entry {
	return Entry{projection: cloneProjection(projection), reference: reference, source: trait, resolved: true}
}

// AutomaticSource is an input boundary narrowed only for Probe acquisition.
// It must be resolved with the selected Format before entering the solver.
func AutomaticSource(projection plan.Boundary, reference access.Reference, trait access.SourceTrait) Entry {
	return Entry{projection: cloneProjection(projection), reference: reference, source: trait, automatic: true}
}

func Sink(projection plan.Boundary, reference access.Reference, trait access.SinkTrait) Entry {
	return Entry{projection: cloneProjection(projection), reference: reference, sink: trait, resolved: true}
}

// AutomaticSink is an output boundary whose Format requirements will be
// selected by Host before the solver runs.
func AutomaticSink(projection plan.Boundary, reference access.Reference, trait access.SinkTrait) Entry {
	return Entry{projection: cloneProjection(projection), reference: reference, sink: trait, automatic: true}
}

func Endpoint(projection plan.Boundary, trait endpoint.Trait) Entry {
	return Entry{projection: cloneProjection(projection), trait: trait, resolved: true}
}

func Direct(projection plan.Boundary, opening any, close func() error) Entry {
	return Entry{projection: cloneProjection(projection), direct: opening, close: close, resolved: true}
}

func ResolveSource(entry Entry, projection plan.Boundary) Entry {
	entry.projection = cloneProjection(projection)
	entry.resolved = true
	return entry
}

func (e Entry) Valid() bool {
	if !e.projection.Valid() {
		return false
	}
	switch e.projection.Kind {
	case plan.ProviderBoundary:
		if !e.reference.Valid() || e.trait.Valid() || e.direct != nil || e.close != nil {
			return false
		}
		if e.projection.Direction == plan.InputBoundary {
			return e.source.Valid() && !e.sink.Valid()
		}
		return e.sink.Valid() && !e.source.Valid()
	case plan.EndpointBoundary:
		return !e.reference.Valid() && !e.source.Valid() && !e.sink.Valid() && e.trait.Valid() && e.direct == nil && e.close == nil
	case plan.DirectBoundary:
		return !e.reference.Valid() && !e.source.Valid() && !e.sink.Valid() && !e.trait.Valid() && e.direct != nil && e.close != nil
	default:
		return false
	}
}

func (e Entry) Projection() plan.Boundary       { return cloneProjection(e.projection) }
func (e Entry) Reference() access.Reference     { return e.reference }
func (e Entry) SourceTrait() access.SourceTrait { return e.source }
func (e Entry) SinkTrait() access.SinkTrait     { return e.sink }
func (e Entry) EndpointTrait() endpoint.Trait   { return e.trait }
func (e Entry) DirectOpening() any              { return e.direct }
func (e Entry) Automatic() bool                 { return e.automatic }
func (e Entry) Pending() bool                   { return e.automatic && !e.resolved }
func (e Entry) Close() error {
	if e.close == nil {
		return nil
	}
	return e.close()
}

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

func (s State) Ready() bool {
	if !s.Valid() {
		return false
	}
	for _, entry := range s.entries {
		if entry.Pending() {
			return false
		}
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
	value.Effective = append([]access.Capability(nil), value.Effective...)
	value.Selected = append([]access.Capability(nil), value.Selected...)
	return value
}
