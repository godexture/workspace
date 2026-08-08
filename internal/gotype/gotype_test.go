package gotype

import (
	"reflect"
	"testing"
)

type named struct{}

func TestCanonicalQualifiesNamedAndCompositeTypes(t *testing.T) {
	if got := Canonical(reflect.TypeFor[named]()); got != "github.com/godexture/godec/internal/gotype.named" {
		t.Fatalf("named type = %q", got)
	}
	if got := Canonical(reflect.TypeFor[map[string][]*named]()); got != "map[string][]*github.com/godexture/godec/internal/gotype.named" {
		t.Fatalf("composite type = %q", got)
	}
}
