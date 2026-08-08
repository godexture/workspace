package flow

import (
	"testing"
)

func TestDescriptorsPreserveManyPortOrderAndCopies(t *testing.T) {
	values := NewDescriptors(Describe("input", 1), Describe("input", 2))
	if err := values.Validate(func(value int) bool { return value > 0 }); err != nil {
		t.Fatal(err)
	}
	got := values.At("input")
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("many descriptors = %#v", got)
	}
	bindings := values.Bindings()
	bindings[0] = PortDescriptor[int]{}
	if values.Bindings()[0].Port() != "input" {
		t.Fatal("Descriptors exposed mutable slice storage")
	}
	if _, ok := values.One("input"); ok {
		t.Fatal("One accepted a many-valued port")
	}
}
