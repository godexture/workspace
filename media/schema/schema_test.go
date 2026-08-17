package schema

import (
	"strings"
	"testing"
)

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
	if first.Descriptor().Equal(second.Descriptor()) {
		t.Fatal("same marker with different payloads produced equal descriptors")
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

func TestOrderTraitIsRuntimeOnly(t *testing.T) {
	type unit struct{ Value int }
	untimed := Define[thirdPartyUnitID](Traits[unit]{})
	ordered := Define[thirdPartyUnitID](Traits[unit]{
		Order: func(value unit) (int64, bool) { return int64(value.Value), true },
	})
	if !ordered.Valid() || !ordered.Descriptor().Equal(untimed.Descriptor()) {
		t.Fatalf("order presence changed descriptor equality: ordered=%#v untimed=%#v", ordered.Descriptor(), untimed.Descriptor())
	}
	if ordered.Descriptor().HasTime() || ordered.Descriptor().Identity() != untimed.Descriptor().Identity() {
		t.Fatal("order presence changed descriptor state")
	}
	if got, ok := ordered.Order(unit{Value: 7}); !ok || got != 7 {
		t.Fatalf("order = %d/%v", got, ok)
	}
	if got, ok := untimed.Order(unit{}); ok || got != 0 {
		t.Fatalf("missing order = %d/%v", got, ok)
	}
	patched := untimed.WithTraits(Traits[unit]{Order: func(unit) (int64, bool) { return 3, true }})
	if !patched.Valid() {
		t.Fatalf("WithTraits rejected an order-only runtime change: %v", patched.Problem())
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

func TestDescriptorTracksTimeTraitPresence(t *testing.T) {
	type unit struct{ Value int }
	typ := Define[thirdPartyUnitID](Traits[unit]{})
	if typ.Descriptor().HasTime() {
		t.Fatal("descriptor reports a time trait that was not declared")
	}
	patched := typ.WithTraits(Traits[unit]{Fork: func(value unit) unit { return value }, Drop: func(unit) {}, Size: func(unit) int { return 1 }})
	if !patched.Valid() || patched.Descriptor().HasTime() || patched.Traits().Fork == nil || patched.Traits().Drop == nil || patched.Traits().Size == nil {
		t.Fatalf("same-presence WithTraits changed descriptor = %#v", patched.Descriptor())
	}
	timed := Define[thirdPartyUnitID](Traits[unit]{Time: func(unit) (int64, bool) { return 0, true }})
	if !timed.Descriptor().HasTime() || timed.Traits().Time == nil {
		t.Fatal("Define did not publish time-trait presence")
	}
}

func TestWithTraitsRejectsTimeTraitPresenceChange(t *testing.T) {
	type unit struct{}
	untimed := Define[thirdPartyUnitID](Traits[unit]{})
	timedAttempt := untimed.WithTraits(Traits[unit]{Time: func(unit) (int64, bool) { return 0, true }})
	if timedAttempt.Valid() || timedAttempt.Descriptor().HasTime() || timedAttempt.Problem() == nil {
		t.Fatalf("untimed schema accepted a timed replacement: valid=%v hasTime=%v problem=%v", timedAttempt.Valid(), timedAttempt.Descriptor().HasTime(), timedAttempt.Problem())
	}
	if !strings.Contains(timedAttempt.Problem().Error(), "time-trait presence") {
		t.Fatalf("time replacement problem = %v", timedAttempt.Problem())
	}
	if !untimed.Valid() || untimed.Descriptor().HasTime() {
		t.Fatal("WithTraits mutated the original untimed schema")
	}

	timed := Define[thirdPartyUnitID](Traits[unit]{Time: func(unit) (int64, bool) { return 1, true }})
	untimedAttempt := timed.WithTraits(Traits[unit]{Fork: func(value unit) unit { return value }})
	if untimedAttempt.Valid() || !untimedAttempt.Descriptor().HasTime() || untimedAttempt.Problem() == nil {
		t.Fatalf("timed schema accepted an untimed replacement: valid=%v hasTime=%v problem=%v", untimedAttempt.Valid(), untimedAttempt.Descriptor().HasTime(), untimedAttempt.Problem())
	}
}

func TestDescriptorEqualIncludesTimeTraitPresence(t *testing.T) {
	type unit struct{}
	untimed := Define[thirdPartyUnitID](Traits[unit]{})
	timed := Define[thirdPartyUnitID](Traits[unit]{Time: func(unit) (int64, bool) { return 0, true }})
	if untimed.Descriptor().Equal(timed.Descriptor()) {
		t.Fatal("time-trait presence was omitted from descriptor equality")
	}
	if !timed.Descriptor().Equal(timed.Descriptor()) {
		t.Fatal("descriptor was not equal to itself")
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
