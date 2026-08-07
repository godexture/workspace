package metadata

import (
	"errors"
	"fmt"

	"github.com/godexture/godec/media/key"
)

// Lossiness states how much of the source value survives a mapping. The host
// never guesses this: the declaring side asserts it, and a lossy mapping is
// reported as an effect rather than applied silently.
type Lossiness uint8

const (
	// Lossless keeps the full meaning of the source value.
	Lossless Lossiness = iota + 1
	// Approximate keeps the meaning but not every detail, such as a date
	// narrowed to its year.
	Approximate
	// Ambiguous asserts a correspondence that is a judgement call, such as one
	// vocabulary's mood mapped onto another's genre.
	Ambiguous
)

func (l Lossiness) Valid() bool { return l >= Lossless && l <= Ambiguous }

func (l Lossiness) String() string {
	switch l {
	case Lossless:
		return "lossless"
	case Approximate:
		return "approximate"
	case Ambiguous:
		return "ambiguous"
	}
	return "unknown"
}

// Mapping declares that one key's value can express another key's value.
//
// Mappings exist so encodings do not need a converter for every other encoding:
// each one maps to and from the shared vocabulary, and the vocabulary is the
// hub. A mapping is always declared, never inferred, because whether two
// third-party keys mean the same thing is not decidable from their types.
type Mapping struct {
	source    key.ID
	target    key.ID
	lossiness Lossiness
	priority  int
	convert   func(any) (any, bool)
	problem   string
}

// Map declares a typed conversion from source to target. Priority orders
// competing mappings for the same target; higher wins, and ties are resolved by
// the source key identity so selection stays deterministic.
func Map[S, T any](source key.Key[S], target key.Key[T], lossiness Lossiness, priority int, convert func(S) (T, bool)) Mapping {
	mapping := Mapping{source: source.ID(), target: target.ID(), lossiness: lossiness, priority: priority}
	switch {
	case source.Problem() != nil:
		mapping.problem = source.Problem().Error()
	case target.Problem() != nil:
		mapping.problem = target.Problem().Error()
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

func (m Mapping) Source() key.ID       { return m.source }
func (m Mapping) Target() key.ID       { return m.target }
func (m Mapping) Lossiness() Lossiness { return m.lossiness }
func (m Mapping) Priority() int        { return m.priority }
func (m Mapping) Valid() bool          { return m.convert != nil && m.problem == "" }

// Problem returns the mapping declaration problem, if any.
func (m Mapping) Problem() error {
	if m.problem == "" {
		return nil
	}
	return errors.New(m.problem)
}

// Convert applies the mapping to one entry value. It reports false when the
// value does not belong to the source key or the conversion declines it, which
// the caller turns into a loss report rather than a silent drop.
func (m Mapping) Convert(value any) (any, bool) {
	if m.convert == nil {
		return nil, false
	}
	return m.convert(value)
}

// Better reports whether m should be preferred over other for the same target.
// Ordering is total and independent of declaration order.
func (m Mapping) Better(other Mapping) bool {
	if m.priority != other.priority {
		return m.priority > other.priority
	}
	if m.lossiness != other.lossiness {
		return m.lossiness < other.lossiness
	}
	return m.source.String() < other.source.String()
}
