package schema

import "testing"

type thirdPartyUnitID struct{}
type alternatePayload struct{ Text string }

func TestMarkerIdentityIsIndependentFromPayload(t *testing.T) {
	type firstPayload struct{ Value int }
	first := Define[thirdPartyUnitID](Traits[firstPayload]{})
	second := Define[thirdPartyUnitID](Traits[alternatePayload]{})
	if first.Identity() == (ID{}) || first.Identity() != second.Identity() {
		t.Fatalf("marker identities = %q and %q", first.Identity(), second.Identity())
	}
	if first.Descriptor().Payload() == second.Descriptor().Payload() {
		t.Fatal("payload types were not kept distinct")
	}
}

func TestErasedFactoryRetainsTypedPipeAndTee(t *testing.T) {
	type unit struct{ Value int }
	typ := Define[thirdPartyUnitID](Traits[unit]{Fork: func(value unit) unit { value.Value++; return value }})
	pipeValue, err := typ.Descriptor().NewPipe(2)
	if err != nil {
		t.Fatal(err)
	}
	pipe, ok := pipeValue.(*Pipe[unit])
	if !ok {
		t.Fatalf("pipe type = %T", pipeValue)
	}
	if !pipe.Push(unit{Value: 1}) {
		t.Fatal("pipe rejected first value")
	}
	teeValue, err := typ.Descriptor().NewTee(2)
	if err != nil {
		t.Fatal(err)
	}
	tee, ok := teeValue.(*Tee[unit])
	if !ok || tee.Outputs() != 2 {
		t.Fatalf("tee = %#v (%T)", teeValue, teeValue)
	}
	if got := tee.Split(unit{Value: 4}); got[1].Value != 5 {
		t.Fatalf("forked values = %#v", got)
	}
}

func TestCatalogRejectsUnknownAndDuplicateSchema(t *testing.T) {
	typ := Define[thirdPartyUnitID](Traits[alternatePayload]{})
	catalog, err := NewCatalog(typ.Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	if !catalog.Has(typ.Identity()) {
		t.Fatal("registered schema is missing")
	}
	if catalog.Has(IdentityOf[struct{}]()) {
		t.Fatal("unknown schema unexpectedly resolved")
	}
	if _, err := NewCatalog(typ.Descriptor(), typ.Descriptor()); err == nil {
		t.Fatal("duplicate schema unexpectedly accepted")
	}
}
