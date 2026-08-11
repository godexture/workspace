package testkit

import (
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/plugin"
)

type typedPort[T any] struct {
	id     string
	schema schema.Type[T]
}

// Subject identifies one typed executable component inside its complete
// composition. The Set form permits definitions whose owned declarations
// legitimately target components supplied by another definition.
type Subject[I, O any] struct {
	set      plugin.Set
	identity plugin.Identity
	input    typedPort[I]
	output   typedPort[O]
	coverage *Coverage
}

// SubjectOf describes a component whose definition is structurally complete
// on its own.
func SubjectOf[I, O any](definition plugin.Definition, identity plugin.Identity, input string, in schema.Type[I], output string, out schema.Type[O]) Subject[I, O] {
	return SubjectIn(plugin.NewSet(definition), identity, input, in, output, out)
}

// SubjectIn describes a component in the complete Set needed to compose it.
func SubjectIn[I, O any](set plugin.Set, identity plugin.Identity, input string, in schema.Type[I], output string, out schema.Type[O]) Subject[I, O] {
	return Subject[I, O]{
		set:      set,
		identity: identity,
		input:    typedPort[I]{id: input, schema: in},
		output:   typedPort[O]{id: output, schema: out},
	}
}

// Identity returns the marker-derived component identity under test.
func (s Subject[I, O]) Identity() plugin.Identity { return s.identity }

func (s Subject[I, O]) valid() bool {
	return !s.set.Empty() && !s.identity.IsZero() && s.input.id != "" && s.output.id != "" && s.input.schema.Valid() && s.output.schema.Valid()
}
