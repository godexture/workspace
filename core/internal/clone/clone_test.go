package clone

import "testing"

type inner struct {
	Tags []string
}

type outer struct {
	Extra    map[string]int
	Nested   *inner
	private  string
	Fixed    [2]int
	Anything any
}

func TestAnyDeepCopiesSliceMapArrayAndNestedPointer(t *testing.T) {
	t.Parallel()
	original := &outer{
		Extra:    map[string]int{"x": 1},
		Nested:   &inner{Tags: []string{"a"}},
		private:  "secret",
		Fixed:    [2]int{1, 2},
		Anything: []string{"boxed"},
	}

	cloned := Any(original).(*outer)
	if cloned == original {
		t.Fatal("Any returned the same pointer")
	}

	cloned.Extra["x"] = 99
	cloned.Nested.Tags[0] = "mutated"
	cloned.Fixed[0] = 99
	cloned.Anything.([]string)[0] = "mutated"

	if original.Extra["x"] == 99 {
		t.Fatal("mutating the clone's map mutated the original")
	}
	if original.Nested.Tags[0] == "mutated" {
		t.Fatal("mutating the clone's nested pointer mutated the original")
	}
	if original.Fixed[0] == 99 {
		t.Fatal("mutating the clone's array mutated the original")
	}
	if original.Anything.([]string)[0] == "mutated" {
		t.Fatal("mutating the clone's interface-boxed slice mutated the original")
	}
}

func TestAnyLeavesUnexportedFieldsZero(t *testing.T) {
	t.Parallel()
	original := &outer{private: "secret"}
	cloned := Any(original).(*outer)
	if cloned.private != "" {
		t.Fatalf("private = %q, want zero value", cloned.private)
	}
}

func TestAnyHandlesNilAndInvalid(t *testing.T) {
	t.Parallel()
	if got := Any(nil); got != nil {
		t.Fatalf("Any(nil) = %v, want nil", got)
	}
	var nilPtr *inner
	if got := Any(nilPtr); got != nilPtr {
		t.Fatalf("Any(nilPtr) = %v, want nil pointer preserved", got)
	}
}
