// Package carrier defines open identities for physical payload slots.
package carrier

import "github.com/godexture/godec/internal/marker"

// ID identifies a format- or bitstream-owned payload slot.
type ID struct{ canonical string }

func (id ID) IsZero() bool        { return id.canonical == "" }
func (id ID) Valid() bool         { return !id.IsZero() }
func (id ID) String() string      { return id.canonical }
func (id ID) PackagePath() string { return marker.PackagePath(id.canonical) }
func (id ID) Name() string        { return marker.Name(id.canonical) }

// Define derives a carrier identity from Marker.
func Define[Marker any]() ID {
	canonical, err := marker.Canonical[Marker]()
	if err != nil {
		return ID{}
	}
	return ID{canonical: canonical}
}
