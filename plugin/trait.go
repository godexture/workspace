package plugin

import (
	"errors"
	"sort"
)

var (
	ErrInvalidTrait   = errors.New("plugin trait is invalid")
	ErrDuplicateTrait = errors.New("plugin trait key is already set")
)

// TraitKey identifies one component trait namespace by a named marker type.
// Trait meanings and typed accessors live in the package that owns the key.
type TraitKey struct {
	identity Identity
}

// TraitKeyOf derives a trait key from a named marker type.
func TraitKeyOf[Marker any]() TraitKey {
	return TraitKey{identity: IdentityOf[Marker]()}
}

func (k TraitKey) Valid() bool    { return !k.identity.IsZero() }
func (k TraitKey) String() string { return k.identity.String() }

// TraitDescriptor is the inert catalog projection of a component trait.
// Manifest is supplied by the trait-owning package and must not contain live
// values, functions, references, or credentials.
type TraitDescriptor struct {
	Key      string
	Manifest string
}

type componentTrait struct {
	key      TraitKey
	manifest string
	value    any
}

type traitStore map[TraitKey]componentTrait

type traitSource interface {
	traitSlots() traitStore
}

// WithTrait attaches one opaque value to a marker-keyed component slot.
// Packages such as access and endpoint wrap this low-level function with
// typed constructors and accessors.
func WithTrait(key TraitKey, manifest string, value any) ComponentOption {
	return func(options *componentOptions) {
		if manifest == "" {
			options.problems = append(options.problems, specItem("plugin.trait", "component trait is invalid"))
			return
		}
		traits, err := options.traits.with(key, manifest, value)
		switch {
		case errors.Is(err, ErrInvalidTrait):
			options.problems = append(options.problems, specItem("plugin.trait", "component trait is invalid"))
		case errors.Is(err, ErrDuplicateTrait):
			options.problems = append(options.problems, specItem("plugin.trait-duplicate", "component may declare a trait key only once"))
		case err == nil:
			options.traits = traits
		}
	}
}

// TraitValueOf returns a typed trait value. Runtime and catalog packages use
// the accessor supplied by the trait-owning package instead of this bridge.
func TraitValueOf[T any](source traitSource, key TraitKey) (T, bool) {
	var zero T
	if source == nil {
		return zero, false
	}
	trait, ok := source.traitSlots()[key]
	if !ok {
		return zero, false
	}
	value, ok := trait.value.(T)
	if !ok {
		return zero, false
	}
	return value, true
}

// CompileContextWithTrait returns an immutable CompileContext with one
// marker-keyed prepared value attached. Semantic packages wrap this bridge
// with typed constructors and accessors.
func CompileContextWithTrait(ctx CompileContext, key TraitKey, value any) (CompileContext, error) {
	traits, err := ctx.traits.with(key, "", value)
	if err != nil {
		return ctx, err
	}
	ctx.traits = traits
	return ctx, nil
}

func (s traitStore) with(key TraitKey, manifest string, value any) (traitStore, error) {
	if !key.Valid() || value == nil {
		return nil, ErrInvalidTrait
	}
	if _, exists := s[key]; exists {
		return nil, ErrDuplicateTrait
	}
	result := cloneTraits(s)
	if result == nil {
		result = make(traitStore)
	}
	result[key] = componentTrait{key: key, manifest: manifest, value: value}
	return result, nil
}

func cloneTraits(values traitStore) traitStore {
	if len(values) == 0 {
		return nil
	}
	result := make(traitStore, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func traitDescriptors(values traitStore) []TraitDescriptor {
	result := make([]TraitDescriptor, 0, len(values))
	for _, value := range values {
		result = append(result, TraitDescriptor{Key: value.key.String(), Manifest: value.manifest})
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Key < result[right].Key })
	return result
}
