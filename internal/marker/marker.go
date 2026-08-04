// Package marker derives canonical identities from Go marker types.
//
// C8 keeps third parties from inventing collision-free string IDs: plugins,
// components, schemas, properties, and metadata keys are identified by the
// package path and name of an empty marker type. Each namespace keeps its own
// public ID type, but they must agree on which markers are valid and how the
// canonical string is formed, so the rule lives here.
package marker

import (
	"errors"
	"reflect"
	"strings"
)

var (
	// ErrUnnamed rejects anonymous types, interfaces, and predeclared types,
	// none of which give a stable package-qualified identity.
	ErrUnnamed = errors.New("marker must be a named concrete type declared by a package")
	// ErrGeneric rejects instantiations such as Marker[int]: the same
	// declaration would produce a different identity per type argument.
	ErrGeneric = errors.New("generic marker instantiations are not stable marker declarations")
)

// Canonical returns "<package path>.<type name>" for a valid marker type.
func Canonical[Marker any]() (string, error) {
	typ := reflect.TypeFor[Marker]()
	if typ == nil || typ.Kind() == reflect.Interface || typ.Name() == "" || typ.PkgPath() == "" {
		return "", ErrUnnamed
	}
	if strings.Contains(typ.Name(), "[") {
		return "", ErrGeneric
	}
	return typ.PkgPath() + "." + typ.Name(), nil
}

// PackagePath returns the declaring package path of a canonical identity. A
// type name never contains a dot, so the last one always separates the two.
func PackagePath(canonical string) string {
	if index := strings.LastIndexByte(canonical, '.'); index >= 0 {
		return canonical[:index]
	}
	return ""
}

// Name returns the marker type name of a canonical identity.
func Name(canonical string) string {
	if index := strings.LastIndexByte(canonical, '.'); index >= 0 {
		return canonical[index+1:]
	}
	return canonical
}
