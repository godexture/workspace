package plugin

import "strings"

// EffectKind classifies what a component changes. Automatic insertion policy
// is derived by Host from this fact and the requested job; plugins do not
// grant themselves permission to alter content.
type EffectKind uint8

const (
	StructuralEffect EffectKind = iota + 1
	RepresentationEffect
	CompressionEffect
	MetadataEffect
	ContentEffect
	TimelineEffect
	TopologyEffect
)

func (k EffectKind) Valid() bool { return k >= StructuralEffect && k <= TopologyEffect }

// Loss states the semantic loss asserted by a component transformation.
type Loss uint8

const (
	NoLoss Loss = iota
	BoundedLoss
	Lossy
	UnknownLoss
)

func (l Loss) Valid() bool { return l <= UnknownLoss }

// Effect is one structured transformation fact. Detail is a stable code, not
// user-formatted diagnostic text.
type Effect struct {
	Kind   EffectKind
	Loss   Loss
	Detail string
	// Item names what the effect is about where the effect is about one thing:
	// the metadata key a carrier could not carry, for instance. Like Detail it
	// is a stable identifier rather than a message, so a surface can group and
	// count effects without parsing them.
	Item string
}

func (e Effect) Valid() bool {
	return e.Kind.Valid() && e.Loss.Valid() && strings.TrimSpace(e.Detail) != ""
}
