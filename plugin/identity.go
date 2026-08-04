package plugin

import (
	"github.com/godexture/godec/internal/marker"
)

// Identity is the canonical identity derived from a named Go marker type.
// Version, display name, config type, and aliases are intentionally absent.
type Identity struct {
	canonical string
}

// IdentityOf derives an identity from marker. Invalid marker types return the
// zero identity; constructors retain the detailed diagnostic instead of
// panicking during package initialization.
func IdentityOf[Marker any]() Identity {
	identity, _ := identityOf[Marker]()
	return identity
}

func identityOf[Marker any]() (Identity, error) {
	canonical, err := marker.Canonical[Marker]()
	if err != nil {
		return Identity{}, err
	}
	return Identity{canonical: canonical}, nil
}

// IsZero reports whether i is invalid or absent.
func (i Identity) IsZero() bool { return i.canonical == "" }

// String returns the canonical external representation.
func (i Identity) String() string { return i.canonical }

// PackagePath returns the marker's declaring package path.
func (i Identity) PackagePath() string { return marker.PackagePath(i.canonical) }

// Name returns the marker type name.
func (i Identity) Name() string { return marker.Name(i.canonical) }
