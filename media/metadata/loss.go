package metadata

import (
	"github.com/godexture/godec/media/key"
)

// LossKind says what happened to a value an encoding could not carry whole.
type LossKind uint8

const (
	// Dropped means the encoding has no way to say this at all.
	Dropped LossKind = iota + 1
	// Folded means the encoding says it once where the document said it
	// several times, so the values after the first are gone.
	Folded
	// Truncated means the encoding kept the value but not all of it, because
	// its field is a fixed size.
	Truncated
	// Converted means a declared Mapping carried the value onto another key.
	// What it cost is the mapping's own Lossiness.
	Converted
)

func (k LossKind) Valid() bool { return k >= Dropped && k <= Converted }

func (k LossKind) String() string {
	switch k {
	case Dropped:
		return "dropped"
	case Folded:
		return "folded"
	case Truncated:
		return "truncated"
	case Converted:
		return "converted"
	}
	return "unknown"
}

// Loss is one thing an encoding could not carry as the document stated it.
//
// An encoding reports these rather than failing, because what a container can
// say is a fact about the container and not a mistake by whoever asked for it.
// A job that would rather fail than lose anything says so in its policy; the
// encoding's answer is the same either way.
type Loss struct {
	// Key is the document key the value was stored under.
	Key key.ID
	// Kind is what became of it.
	Kind LossKind
	// Native is the encoding's own name for the field, where it has one. A
	// dropped value usually has none, which is why it was dropped.
	Native string
	// Mapping is what a conversion cost, and is zero for every other kind.
	Mapping Lossiness
	// Detail is a stable code, not a sentence: surfaces phrase it.
	Detail string
}

func (l Loss) Valid() bool {
	if !l.Kind.Valid() || l.Key.IsZero() {
		return false
	}
	if l.Kind == Converted {
		return l.Mapping.Valid()
	}
	return l.Mapping == 0
}

// Lost reports whether any of these describe something that did not survive.
// An empty report is the ordinary case and says the carrier held everything.
func Lost(values []Loss) bool { return len(values) != 0 }
