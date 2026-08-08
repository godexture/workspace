package plugin

import (
	"reflect"
	"strconv"
	"strings"
)

// DeclarationTargetKind distinguishes catalog component references from Go
// payload types. Only component targets are resolved through the catalog.
type DeclarationTargetKind uint8

const (
	ComponentTargetKind DeclarationTargetKind = iota + 1
	TypeTargetKind
)

func (k DeclarationTargetKind) String() string {
	switch k {
	case ComponentTargetKind:
		return "component"
	case TypeTargetKind:
		return "type"
	default:
		return "unknown"
	}
}

// DeclarationTarget is one ordered value attached to a composition
// declaration. Its zero value is invalid.
type DeclarationTarget struct {
	kind      DeclarationTargetKind
	component Identity
	valueType reflect.Type
}

func componentTarget(identity Identity) DeclarationTarget {
	return DeclarationTarget{kind: ComponentTargetKind, component: identity}
}

func typeTarget(valueType reflect.Type) DeclarationTarget {
	return DeclarationTarget{kind: TypeTargetKind, valueType: valueType}
}

func (t DeclarationTarget) Kind() DeclarationTargetKind { return t.kind }

// Component returns the referenced component identity when this is a
// component target.
func (t DeclarationTarget) Component() (Identity, bool) {
	return t.component, t.kind == ComponentTargetKind && !t.component.IsZero()
}

// Type returns the payload type when this is a type target.
func (t DeclarationTarget) Type() (reflect.Type, bool) {
	return t.valueType, t.kind == TypeTargetKind && t.valueType != nil
}

func (t DeclarationTarget) Valid() bool {
	switch t.kind {
	case ComponentTargetKind:
		return !t.component.IsZero() && t.valueType == nil
	case TypeTargetKind:
		return t.component.IsZero() && t.valueType != nil
	default:
		return false
	}
}

// String returns a deterministic representation suitable for sorting,
// diagnostics, and catalog fingerprints.
func (t DeclarationTarget) String() string {
	switch t.kind {
	case ComponentTargetKind:
		return "component:" + t.component.String()
	case TypeTargetKind:
		return "type:" + canonicalType(t.valueType)
	default:
		return "invalid:"
	}
}

func canonicalType(valueType reflect.Type) string {
	if valueType == nil {
		return ""
	}
	if name := valueType.Name(); name != "" {
		if packagePath := valueType.PkgPath(); packagePath != "" {
			return packagePath + "." + name
		}
		return name
	}

	switch valueType.Kind() {
	case reflect.Pointer:
		return "*" + canonicalType(valueType.Elem())
	case reflect.Slice:
		return "[]" + canonicalType(valueType.Elem())
	case reflect.Array:
		return "[" + strconv.Itoa(valueType.Len()) + "]" + canonicalType(valueType.Elem())
	case reflect.Map:
		return "map[" + canonicalType(valueType.Key()) + "]" + canonicalType(valueType.Elem())
	case reflect.Chan:
		prefix := "chan "
		switch valueType.ChanDir() {
		case reflect.RecvDir:
			prefix = "<-chan "
		case reflect.SendDir:
			prefix = "chan<- "
		}
		return prefix + canonicalType(valueType.Elem())
	case reflect.Func:
		return canonicalFunc(valueType)
	case reflect.Struct:
		fields := make([]string, valueType.NumField())
		for index := range fields {
			field := valueType.Field(index)
			name := field.Name
			if field.PkgPath != "" {
				name = field.PkgPath + "." + name
			}
			if field.Anonymous {
				name = "embedded " + name
			}
			fields[index] = name + " " + canonicalType(field.Type) + " " + strconv.Quote(string(field.Tag))
		}
		return "struct{" + strings.Join(fields, ";") + "}"
	case reflect.Interface:
		methods := make([]string, valueType.NumMethod())
		for index := range methods {
			method := valueType.Method(index)
			name := method.Name
			if method.PkgPath != "" {
				name = method.PkgPath + "." + name
			}
			methods[index] = name + canonicalFunc(method.Type)
		}
		return "interface{" + strings.Join(methods, ";") + "}"
	default:
		return valueType.String()
	}
}

func canonicalFunc(valueType reflect.Type) string {
	inputs := make([]string, valueType.NumIn())
	for index := range inputs {
		input := canonicalType(valueType.In(index))
		if valueType.IsVariadic() && index == len(inputs)-1 {
			input = "..." + canonicalType(valueType.In(index).Elem())
		}
		inputs[index] = input
	}
	outputs := make([]string, valueType.NumOut())
	for index := range outputs {
		outputs[index] = canonicalType(valueType.Out(index))
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
