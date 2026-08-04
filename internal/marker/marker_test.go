package marker

import (
	"errors"
	"testing"
)

type validMarker struct{}

type genericMarker[T any] struct{}

type markerInterface interface{ marker() }

func TestCanonicalDerivesPackageQualifiedIdentity(t *testing.T) {
	canonical, err := Canonical[validMarker]()
	if err != nil {
		t.Fatal(err)
	}
	if PackagePath(canonical) != "github.com/godexture/godec/internal/marker" || Name(canonical) != "validMarker" {
		t.Fatalf("canonical = %q", canonical)
	}
}

func TestCanonicalRejectsMarkersWithoutStableIdentity(t *testing.T) {
	if _, err := Canonical[struct{}](); !errors.Is(err, ErrUnnamed) {
		t.Fatalf("anonymous struct = %v", err)
	}
	if _, err := Canonical[markerInterface](); !errors.Is(err, ErrUnnamed) {
		t.Fatalf("interface = %v", err)
	}
	if _, err := Canonical[int](); !errors.Is(err, ErrUnnamed) {
		t.Fatalf("predeclared type = %v", err)
	}
	// Instantiations of one declaration would otherwise produce a different
	// identity per type argument.
	if _, err := Canonical[genericMarker[int]](); !errors.Is(err, ErrGeneric) {
		t.Fatalf("generic instantiation = %v", err)
	}
}

func TestSplitHandlesPackagePathsContainingDots(t *testing.T) {
	const canonical = "gopkg.in/yaml.v2.someMarker"
	if PackagePath(canonical) != "gopkg.in/yaml.v2" || Name(canonical) != "someMarker" {
		t.Fatalf("split = %q, %q", PackagePath(canonical), Name(canonical))
	}
}
