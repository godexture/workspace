// Package loss defines the format-independent evidence that semantic metadata
// could not be carried exactly.
package loss

import (
	"strings"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/key"
)

// Kind says what happened to a value an encoding could not carry whole.
type Kind uint8

const (
	Dropped Kind = iota + 1
	Folded
	Truncated
	Converted
	Substituted
)

func (k Kind) Valid() bool { return k >= Dropped && k <= Substituted }

func (k Kind) String() string {
	switch k {
	case Dropped:
		return "dropped"
	case Folded:
		return "folded"
	case Truncated:
		return "truncated"
	case Converted:
		return "converted"
	case Substituted:
		return "substituted"
	}
	return "unknown"
}

// Lossiness says how much source meaning survives a declared mapping.
type Lossiness uint8

const (
	Lossless Lossiness = iota + 1
	Approximate
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

// Origin identifies the source block that supplied a value, when that fact
// is known. It deliberately mirrors metadata provenance without importing the
// metadata package, so reports can travel through plugin and plan contracts.
type Origin struct {
	Carrier  carrier.ID
	Encoding string
	Block    string
	Native   string
}

func (o Origin) IsZero() bool {
	return !o.Carrier.Valid() && o.Encoding == "" && o.Block == "" && o.Native == ""
}

func (o Origin) Valid() bool {
	return o.Carrier.Valid() && strings.TrimSpace(o.Encoding) != "" && strings.TrimSpace(o.Block) != ""
}

// Loss is one fact an encoding could not carry as the document stated it.
// Detail is a stable code for surfaces, not a rendered sentence.
type Loss struct {
	Key     key.ID
	Kind    Kind
	Native  string
	Mapping Lossiness
	Target  key.ID
	Detail  string
	Source  Origin
}

func (l Loss) Valid() bool {
	if l.Key.IsZero() || !l.Kind.Valid() || strings.TrimSpace(l.Detail) == "" {
		return false
	}
	if !l.Source.IsZero() && !l.Source.Valid() {
		return false
	}
	if l.Kind == Converted {
		return !l.Target.IsZero() && l.Mapping.Valid()
	}
	return l.Target.IsZero() && l.Mapping == 0
}

// Lossy reports whether the result loses semantics. Declared lossless mapping
// remains visible as conversion evidence but is not a loss-policy violation.
func (l Loss) Lossy() bool {
	return l.Kind != Converted || l.Mapping != Lossless
}

// Report binds a loss fact to the target metadata block which will carry the
// output bytes. Encoding is a component identity rendered as a stable string.
type Report struct {
	Carrier  carrier.ID
	Encoding string
	Block    string
	Loss     Loss
}

func (r Report) Valid() bool {
	return r.Carrier.Valid() && strings.TrimSpace(r.Encoding) != "" && strings.TrimSpace(r.Block) != "" && r.Loss.Valid()
}

func (r Report) Lossy() bool { return r.Loss.Lossy() }
