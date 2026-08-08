package plugin

import (
	"reflect"

	"github.com/godexture/godec/internal/gotype"
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
		return "type:" + gotype.Canonical(t.valueType)
	default:
		return "invalid:"
	}
}
