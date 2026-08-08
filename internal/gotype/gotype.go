// Package gotype produces deterministic identities for Go payload types used
// at erased control-plane boundaries.
package gotype

import (
	"reflect"
	"strconv"
	"strings"
)

// Canonical returns a package-qualified structural representation of typ.
func Canonical(typ reflect.Type) string {
	if typ == nil {
		return ""
	}
	if name := typ.Name(); name != "" {
		if packagePath := typ.PkgPath(); packagePath != "" {
			return packagePath + "." + name
		}
		return name
	}

	switch typ.Kind() {
	case reflect.Pointer:
		return "*" + Canonical(typ.Elem())
	case reflect.Slice:
		return "[]" + Canonical(typ.Elem())
	case reflect.Array:
		return "[" + strconv.Itoa(typ.Len()) + "]" + Canonical(typ.Elem())
	case reflect.Map:
		return "map[" + Canonical(typ.Key()) + "]" + Canonical(typ.Elem())
	case reflect.Chan:
		prefix := "chan "
		switch typ.ChanDir() {
		case reflect.RecvDir:
			prefix = "<-chan "
		case reflect.SendDir:
			prefix = "chan<- "
		}
		return prefix + Canonical(typ.Elem())
	case reflect.Func:
		return canonicalFunc(typ)
	case reflect.Struct:
		fields := make([]string, typ.NumField())
		for index := range fields {
			field := typ.Field(index)
			name := field.Name
			if field.PkgPath != "" {
				name = field.PkgPath + "." + name
			}
			if field.Anonymous {
				name = "embedded " + name
			}
			fields[index] = name + " " + Canonical(field.Type) + " " + strconv.Quote(string(field.Tag))
		}
		return "struct{" + strings.Join(fields, ";") + "}"
	case reflect.Interface:
		methods := make([]string, typ.NumMethod())
		for index := range methods {
			method := typ.Method(index)
			name := method.Name
			if method.PkgPath != "" {
				name = method.PkgPath + "." + name
			}
			methods[index] = name + canonicalFunc(method.Type)
		}
		return "interface{" + strings.Join(methods, ";") + "}"
	default:
		return typ.String()
	}
}

func canonicalFunc(typ reflect.Type) string {
	inputs := make([]string, typ.NumIn())
	for index := range inputs {
		input := Canonical(typ.In(index))
		if typ.IsVariadic() && index == len(inputs)-1 {
			input = "..." + Canonical(typ.In(index).Elem())
		}
		inputs[index] = input
	}
	outputs := make([]string, typ.NumOut())
	for index := range outputs {
		outputs[index] = Canonical(typ.Out(index))
	}
	result := "func(" + strings.Join(inputs, ",") + ")"
	if len(outputs) == 1 {
		return result + outputs[0]
	}
	if len(outputs) > 1 {
		return result + "(" + strings.Join(outputs, ",") + ")"
	}
	return result
}
