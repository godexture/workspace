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

func TestTypedTraitsRemainOnTypedSchema(t *testing.T) {
	type unit struct{ Value int }
	typ := Define[thirdPartyUnitID](Traits[unit]{
		Fork: func(value unit) unit { value.Value++; return value },
		Size: func(unit) int { return 1 },
		Time: func(unit) (int64, bool) { return 7, true },
	})
	if got := typ.Fork(unit{Value: 4}); got.Value != 5 {
		t.Fatalf("forked value = %#v", got)
	}
	if got, ok := typ.Size(unit{}); !ok || got != 1 {
		t.Fatalf("size = %d, %v", got, ok)
	}
	if got, ok := typ.Time(unit{}); !ok || got != 7 {
		t.Fatalf("time = %d, %v", got, ok)
	}
}

func TestDescriptorRetainsIdentityAndPayloadWithoutRuntimeProducts(t *testing.T) {
	typ := Define[thirdPartyUnitID](Traits[alternatePayload]{
		Fork: func(value alternatePayload) alternatePayload { return value },
	})
	descriptor := typ.Descriptor()
	if !descriptor.Valid() || descriptor.Identity() != typ.Identity() {
		t.Fatalf("descriptor = %#v", descriptor)
	}
	// A second Define for the same marker has the same semantic identity.
	again := Define[thirdPartyUnitID](Traits[alternatePayload]{})
	if again.Identity() != descriptor.Identity() {
		t.Fatalf("repeated Define identity = %v, want %v", again.Identity(), descriptor.Identity())
	}
	if descriptor.Payload() == nil || descriptor.Payload().Name() != "alternatePayload" {
		t.Fatalf("payload = %v", descriptor.Payload())
	}
}

func TestInvalidMarkerRetainsProblem(t *testing.T) {
	typ := Define[struct{}](Traits[int]{})
	if typ.Valid() || !typ.Identity().IsZero() {
		t.Fatal("invalid marker unexpectedly produced a valid schema")
	}
	if typ.Problem() == nil {
		t.Fatal("invalid marker problem was discarded")
	}
}
