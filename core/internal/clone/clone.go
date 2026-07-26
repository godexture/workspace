// Package clone deep-copies values of unknown, reflection-only types —
// notably registry.Configuration, whose concrete type is defined by
// whichever plugin registered it.
package clone

import "reflect"

// Any returns a deep copy of value: pointers, interfaces, slices, maps, and
// arrays are copied recursively so the result shares no mutable state with
// value. Unexported struct fields are left at their zero value, since
// reflect cannot set them without unsafe tricks. Channels, funcs, and other
// kinds with no meaningful copy are returned as-is.
func Any(value any) any {
	v := reflect.ValueOf(value)
	if !v.IsValid() {
		return value
	}
	return deepClone(v).Interface()
}

func deepClone(value reflect.Value) reflect.Value {
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return value
		}
		clone := reflect.New(value.Type().Elem())
		clone.Elem().Set(deepClone(value.Elem()))
		return clone
	case reflect.Interface:
		if value.IsNil() {
			return value
		}
		clone := reflect.New(value.Type()).Elem()
		clone.Set(deepClone(value.Elem()))
		return clone
	case reflect.Slice:
		if value.IsNil() {
			return value
		}
		clone := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for i := range value.Len() {
			clone.Index(i).Set(deepClone(value.Index(i)))
		}
		return clone
	case reflect.Array:
		clone := reflect.New(value.Type()).Elem()
		for i := range value.Len() {
			clone.Index(i).Set(deepClone(value.Index(i)))
		}
		return clone
	case reflect.Map:
		if value.IsNil() {
			return value
		}
		clone := reflect.MakeMapWithSize(value.Type(), value.Len())
		iter := value.MapRange()
		for iter.Next() {
			clone.SetMapIndex(deepClone(iter.Key()), deepClone(iter.Value()))
		}
		return clone
	case reflect.Struct:
		clone := reflect.New(value.Type()).Elem()
		for i := range value.NumField() {
			field := clone.Field(i)
			if !field.CanSet() {
				// Unexported field: leave the zero value rather than
				// panicking on reflect.Value.Set.
				continue
			}
			field.Set(deepClone(value.Field(i)))
		}
		return clone
	default:
		return value
	}
}
