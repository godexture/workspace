package metadata

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/plugin"
)

// Mapping declares that one key's value can express another key's value.
//
// Mappings exist so encodings do not need a converter for every other encoding:
// each one maps to and from the shared vocabulary, and the vocabulary is the
// hub. A mapping is always declared, never inferred, because whether two
// third-party keys mean the same thing is not decidable from their types.
type Mapping struct {
	sourceDeclaration key.Erased
	source            key.ID
	target            key.ID
	targetDeclaration key.Erased
	lossiness         loss.Lossiness
	priority          int
	convert           func(any) (any, bool)
	problem           string
}

// Map declares a typed conversion from source to target. Priority orders
// mappings considered for one source entry; higher wins, then lossiness and
// target key identity keep selection deterministic.
func Map[S, T any](source key.Key[S], target key.Key[T], lossiness loss.Lossiness, priority int, convert func(S) (T, bool)) Mapping {
	mapping := Mapping{sourceDeclaration: source.Erased(), source: source.ID(), target: target.ID(), targetDeclaration: target.Erased(), lossiness: lossiness, priority: priority}
	switch {
	case source.Problem() != nil:
		mapping.problem = source.Problem().Error()
	case target.Problem() != nil:
		mapping.problem = target.Problem().Error()
	case !source.Valid():
		mapping.problem = "metadata mapping source key is invalid"
	case !target.Valid():
		mapping.problem = "metadata mapping target key is invalid"
	case !lossiness.Valid():
		mapping.problem = fmt.Sprintf("metadata mapping %s -> %s must declare its lossiness", source.ID(), target.ID())
	case convert == nil:
		mapping.problem = fmt.Sprintf("metadata mapping %s -> %s must supply a conversion", source.ID(), target.ID())
	case source.ID() == target.ID():
		mapping.problem = fmt.Sprintf("metadata mapping source and target are both %s", source.ID())
	default:
		mapping.convert = func(value any) (any, bool) {
			typed, ok := value.(S)
			if !ok {
				return nil, false
			}
			converted, ok := convert(typed)
			if !ok {
				return nil, false
			}
			return target.Erased().Clone(converted)
		}
	}
	return mapping
}

func (m Mapping) Source() key.ID            { return m.source }
func (m Mapping) Target() key.ID            { return m.target }
func (m Mapping) Lossiness() loss.Lossiness { return m.lossiness }
func (m Mapping) Priority() int             { return m.priority }
func (m Mapping) Valid() bool {
	return m.sourceDeclaration.Valid() && m.targetDeclaration.Valid() && m.convert != nil && m.problem == ""
}

// Problem returns the mapping declaration problem, if any.
func (m Mapping) Problem() error {
	if m.problem == "" {
		return nil
	}
	return errors.New(m.problem)
}

// Convert applies the mapping to a source snapshot. It reports false when the
// value does not belong to the source key or the conversion declines it, which
// the caller turns into a loss report rather than a silent drop.
func (m Mapping) Convert(value any) (any, bool) {
	if m.convert == nil || !m.sourceDeclaration.Valid() {
		return nil, false
	}
	snapshot, ok := m.sourceDeclaration.Clone(value)
	if !ok {
		return nil, false
	}
	return m.convert(snapshot)
}

// Better reports whether m should be preferred for one source entry. Ordering
// is total and independent of declaration order.
func (m Mapping) Better(other Mapping) bool {
	if m.priority != other.priority {
		return m.priority > other.priority
	}
	if m.lossiness != other.lossiness {
		return m.lossiness < other.lossiness
	}
	if m.target != other.target {
		return m.target.String() < other.target.String()
	}
	return m.source.String() < other.source.String()
}

type mappingTraitKey struct{}

var mappingsKey = plugin.TraitKeyOf[mappingTraitKey]()

// Mappings is an immutable component declaration of semantic conversions.
type Mappings struct {
	values  []Mapping
	problem string
}

func (m Mappings) Valid() bool { return len(m.values) != 0 && m.problem == "" }

func (m Mappings) Problem() error {
	if m.problem == "" {
		return nil
	}
	return errors.New(m.problem)
}

// Values returns a snapshot of declared mappings.
func (m Mappings) Values() []Mapping { return append([]Mapping(nil), m.values...) }

// WithMappings attaches explicitly declared semantic conversions without
// coupling them to a component's data-plane shape.
func WithMappings(mappings ...Mapping) plugin.ComponentOption {
	value := newMappings(mappings)
	return plugin.WithTrait(mappingsKey, value.manifest(), plugin.PortShapeOptional, value)
}

// MappingsOf returns a snapshot of mappings declared by component.
func MappingsOf(component plugin.Component) (Mappings, bool) {
	value, ok := plugin.TraitValueOf[Mappings](component, mappingsKey)
	value.values = append([]Mapping(nil), value.values...)
	return value, ok
}

func newMappings(values []Mapping) Mappings {
	result := Mappings{values: append([]Mapping(nil), values...)}
	if err := validateMappingTrait(result.values); err != nil {
		result.problem = err.Error()
	}
	return result
}

func (m Mappings) manifest() string {
	if m.problem != "" {
		return "invalid=" + m.problem
	}
	ordered := m.Values()
	sort.Slice(ordered, func(left, right int) bool {
		if ordered[left].Source() != ordered[right].Source() {
			return ordered[left].Source().String() < ordered[right].Source().String()
		}
		if ordered[left].Target() != ordered[right].Target() {
			return ordered[left].Target().String() < ordered[right].Target().String()
		}
		if ordered[left].Lossiness() != ordered[right].Lossiness() {
			return ordered[left].Lossiness() < ordered[right].Lossiness()
		}
		return ordered[left].Priority() < ordered[right].Priority()
	})
	parts := make([]string, len(ordered))
	for index, mapping := range ordered {
		parts[index] = mapping.Source().String() + ">" + mapping.Target().String() + ":" + mapping.Lossiness().String() + ":" + strconv.Itoa(mapping.Priority())
	}
	return "mappings=" + strings.Join(parts, ",")
}

func validateMappingTrait(values []Mapping) error {
	if len(values) == 0 {
		return errors.New("metadata mapping trait must declare at least one mapping")
	}
	seen := make(map[mappingContract]struct{}, len(values))
	for _, mapping := range values {
		if !mapping.Valid() {
			if problem := mapping.Problem(); problem != nil {
				return problem
			}
			return errors.New("metadata mapping trait has an invalid mapping")
		}
		contract := mapping.contract()
		if _, exists := seen[contract]; exists {
			return fmt.Errorf("metadata mapping trait repeats %s -> %s", mapping.Source(), mapping.Target())
		}
		seen[contract] = struct{}{}
	}
	return nil
}

type mappingContract struct {
	source    key.ID
	target    key.ID
	lossiness loss.Lossiness
	priority  int
}

func (m Mapping) contract() mappingContract {
	return mappingContract{source: m.source, target: m.target, lossiness: m.lossiness, priority: m.priority}
}
