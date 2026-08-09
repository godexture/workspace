package plugin

import "sort"

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

// WithTrait attaches one opaque value to a marker-keyed component slot.
// Packages such as access and endpoint wrap this low-level function with
// typed constructors and accessors.
func WithTrait(key TraitKey, manifest string, value any) ComponentOption {
	return func(options *componentOptions) {
		if !key.Valid() || manifest == "" || value == nil {
			options.problems = append(options.problems, specItem("plugin.trait", "component trait is invalid"))
			return
		}
		if options.traits == nil {
			options.traits = make(map[TraitKey]componentTrait)
		}
		if _, exists := options.traits[key]; exists {
			options.problems = append(options.problems, specItem("plugin.trait-duplicate", "component may declare a trait key only once"))
			return
		}
		options.traits[key] = componentTrait{key: key, manifest: manifest, value: value}
	}
}

// TraitValueOf returns a typed trait value. Runtime and catalog packages use
// the accessor supplied by the trait-owning package instead of this bridge.
func TraitValueOf[T any](component Component, key TraitKey) (T, bool) {
	var zero T
	trait, ok := component.traits[key]
	if !ok {
		return zero, false
	}
	value, ok := trait.value.(T)
	if !ok {
		return zero, false
	}
	return value, true
}

func cloneTraits(values map[TraitKey]componentTrait) map[TraitKey]componentTrait {
	if len(values) == 0 {
		return nil
	}
	result := make(map[TraitKey]componentTrait, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func traitDescriptors(values map[TraitKey]componentTrait) []TraitDescriptor {
	result := make([]TraitDescriptor, 0, len(values))
	for _, value := range values {
		result = append(result, TraitDescriptor{Key: value.key.String(), Manifest: value.manifest})
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Key < result[right].Key })
	return result
}
