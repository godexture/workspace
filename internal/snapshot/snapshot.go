// Package snapshot decides when a declared clone is required to turn a value
// into a snapshot.
//
// C17 forbids guessing how to copy an arbitrary Go value, so config codecs,
// property keys, and metadata keys all reject a reference-valued type that has
// no declared clone. The rule lives here so those registrations cannot drift
// apart.
package snapshot

import "reflect"

// NeedsClone reports whether a shallow copy of typ can preserve shared mutable
// state, which means the declaring side must supply a clone.
//
// It runs during registration only. Data-plane code never dispatches on
// reflection.
func NeedsClone(typ reflect.Type) bool {
	return needsClone(typ, make(map[reflect.Type]bool))
}

func needsClone(typ reflect.Type, seen map[reflect.Type]bool) bool {
	if typ == nil {
		return false
	}
	switch typ.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return true
	case reflect.Array:
		return needsClone(typ.Elem(), seen)
	case reflect.Struct:
		if seen[typ] {
			return false
		}
		seen[typ] = true
		for index := 0; index < typ.NumField(); index++ {
			if needsClone(typ.Field(index).Type, seen) {
				return true
			}
		}
	}
	return false
}
