package config

import (
	"reflect"
)

// ApplyDefaults merges an optional configuration struct with a default resolved configuration struct.
// For any field in cfg that has an Exists() bool method, if it exists, its unwrapped value is used.
// Otherwise, the value from def is used.
// Fields without an Exists() method are copied directly if assignable.
func ApplyDefaults[T any, U any](cfg T, def U) U {
	cfgVal := reflect.ValueOf(cfg)
	defVal := reflect.ValueOf(def)

	// If cfg is a pointer, dereference it (handle nil gracefully if necessary, though T is usually value)
	if cfgVal.Kind() == reflect.Ptr {
		if cfgVal.IsNil() {
			return def
		}
		cfgVal = cfgVal.Elem()
	}
	if defVal.Kind() == reflect.Ptr {
		defVal = defVal.Elem()
	}

	if cfgVal.Kind() != reflect.Struct || defVal.Kind() != reflect.Struct {
		return def
	}

	out := reflect.New(defVal.Type()).Elem()
	out.Set(defVal) // start with defaults

	for i := 0; i < cfgVal.NumField(); i++ {
		cfgField := cfgVal.Field(i)
		fieldName := cfgVal.Type().Field(i).Name

		outField := out.FieldByName(fieldName)
		if !outField.IsValid() || !outField.CanSet() {
			continue
		}

		existsMethod := cfgField.MethodByName("Exists")
		if existsMethod.IsValid() {
			res := existsMethod.Call(nil)
			if len(res) > 0 && res[0].Bool() {
				// Field exists, get the underlying value
				unwrapMethod := cfgField.MethodByName("Unwrap")
				if unwrapMethod.IsValid() {
					unwrapped := unwrapMethod.Call(nil)[0]
					if unwrapped.Type().AssignableTo(outField.Type()) {
						outField.Set(unwrapped)
					}
				}
			}
		} else {
			// Not optional, just assign if assignable
			if cfgField.Type().AssignableTo(outField.Type()) {
				outField.Set(cfgField)
			}
		}
	}

	return out.Interface().(U)
}
