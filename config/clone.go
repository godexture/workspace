package config

import "reflect"

type cloneVisit struct {
	typ  reflect.Type
	ptr  uintptr
	kind reflect.Kind
}

func cloneTyped[T any](value T) T {
	cloned := cloneValue(reflect.ValueOf(value), make(map[cloneVisit]reflect.Value))
	if !cloned.IsValid() {
		var zero T
		return zero
	}
	return cloned.Interface().(T)
}

func cloneValue(value reflect.Value, visited map[cloneVisit]reflect.Value) reflect.Value {
	if !value.IsValid() {
		return reflect.Value{}
	}

	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := cloneValue(value.Elem(), visited)
		result := reflect.New(value.Type()).Elem()
		if cloned.IsValid() && cloned.Type().AssignableTo(value.Type()) {
			result.Set(cloned)
		} else if cloned.IsValid() && cloned.Type().Implements(value.Type()) {
			result.Set(cloned)
		} else if cloned.IsValid() {
			result.Set(value)
		}
		return result

	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		key := cloneVisit{typ: value.Type(), ptr: value.Pointer(), kind: value.Kind()}
		if cloned, ok := visited[key]; ok {
			return cloned
		}
		result := reflect.New(value.Type().Elem())
		visited[key] = result
		result.Elem().Set(cloneAssignable(value.Elem(), visited))
		return result

	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		result := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for index := 0; index < value.Len(); index++ {
			result.Index(index).Set(cloneAssignable(value.Index(index), visited))
		}
		return result

	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		result := reflect.MakeMapWithSize(value.Type(), value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			key := cloneAssignable(iterator.Key(), visited)
			entry := cloneAssignable(iterator.Value(), visited)
			result.SetMapIndex(key, entry)
		}
		return result

	case reflect.Struct:
		result := reflect.New(value.Type()).Elem()
		result.Set(value)
		for index := 0; index < value.NumField(); index++ {
			if !result.Field(index).CanSet() || !value.Field(index).CanInterface() {
				continue
			}
			result.Field(index).Set(cloneAssignable(value.Field(index), visited))
		}
		return result

	case reflect.Array:
		result := reflect.New(value.Type()).Elem()
		for index := 0; index < value.Len(); index++ {
			result.Index(index).Set(cloneAssignable(value.Index(index), visited))
		}
		return result

	default:
		// Scalars and functions are copied by value. A custom codec must provide
		// the canonical meaning for handles or function values.
		return value
	}
}

func cloneAssignable(value reflect.Value, visited map[cloneVisit]reflect.Value) reflect.Value {
	cloned := cloneValue(value, visited)
	if cloned.IsValid() && cloned.Type().AssignableTo(value.Type()) {
		return cloned
	}
	return value
}
