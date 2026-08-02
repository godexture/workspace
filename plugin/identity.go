package plugin

import (
	"fmt"
	"reflect"
	"strings"
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
	typ := reflect.TypeFor[Marker]()
	if typ == nil || typ.Kind() == reflect.Interface || typ.Name() == "" || typ.PkgPath() == "" {
		return Identity{}, fmt.Errorf("marker must be a named concrete type declared by a package")
	}
	if strings.Contains(typ.Name(), "[") {
		return Identity{}, fmt.Errorf("generic marker instantiations are not stable marker declarations")
	}
	return Identity{canonical: typ.PkgPath() + "." + typ.Name()}, nil
}

// IsZero reports whether i is invalid or absent.
func (i Identity) IsZero() bool { return i.canonical == "" }

// String returns the canonical external representation.
func (i Identity) String() string { return i.canonical }

// PackagePath returns the marker's declaring package path.
func (i Identity) PackagePath() string {
	if index := strings.LastIndexByte(i.canonical, '.'); index >= 0 {
		return i.canonical[:index]
	}
	return ""
}

// Name returns the marker type name.
func (i Identity) Name() string {
	if index := strings.LastIndexByte(i.canonical, '.'); index >= 0 {
		return i.canonical[index+1:]
	}
	return i.canonical
}
