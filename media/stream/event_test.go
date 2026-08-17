package stream

import (
	"testing"

	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/timing"
)

type eventSchemaID struct{}
type eventPayload struct{}
type eventPropertyID struct{}

func TestStreamEventsKeepFollowPolicyUndecided(t *testing.T) {
	typ := schema.Define[eventSchemaID, eventPayload](schema.Traits[eventPayload]{Time: func(eventPayload) (int64, bool) { return 0, true }})
	properties, err := property.Define[eventPropertyID](property.Scalar[int]()).Set(property.New(), 2)
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := NewDescriptor("audio-0", typ.Descriptor(), timing.MustBase(1, 1000), properties)
	if err != nil {
		t.Fatal(err)
	}
	added, err := NewAdded(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if !added.Valid() || added.Kind() != StreamAdded || added.Decision() != Undecided {
		t.Fatalf("added event = %#v", added)
	}
	selected, err := added.WithDecision(Follow)
	if err != nil {
		t.Fatal(err)
	}
	if selected.Decision() != Follow || added.Decision() != Undecided {
		t.Fatalf("event decisions = %v, %v", selected.Decision(), added.Decision())
	}
}

func TestStreamEventsRepresentRemovalAndPropertyChange(t *testing.T) {
	removed, err := NewRemoved("audio-1")
	if err != nil {
		t.Fatal(err)
	}
	changed, err := NewChanged("audio-0", property.New())
	if err != nil {
		t.Fatal(err)
	}
	if removed.Kind() != StreamRemoved || removed.ID() != "audio-1" || changed.Kind() != StreamChanged {
		t.Fatalf("events = %#v, %#v", removed, changed)
	}
	if got, ok := changed.Properties(); !ok || got.Len() != 0 {
		t.Fatalf("changed properties = %#v, %v", got, ok)
	}
}

// Two streams that carry the same schema must still be distinguishable, which
// is the reason stream.ID exists separately from schema.ID.
func TestEventsTellApartTwoStreamsOfTheSameSchema(t *testing.T) {
	typ := schema.Define[eventSchemaID, eventPayload](schema.Traits[eventPayload]{Time: func(eventPayload) (int64, bool) { return 0, true }})
	base := timing.MustBase(1, 1000)
	first, err := NewDescriptor("audio-0", typ.Descriptor(), base, property.New())
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewDescriptor("audio-1", typ.Descriptor(), base, property.New())
	if err != nil {
		t.Fatal(err)
	}
	if first.Schema() != second.Schema() || first.ID() == second.ID() {
		t.Fatal("same-schema streams do not have distinct identities")
	}
	addedFirst, err := NewAdded(first)
	if err != nil {
		t.Fatal(err)
	}
	removedSecond, err := NewRemoved(second.ID())
	if err != nil {
		t.Fatal(err)
	}
	if addedFirst.ID() == removedSecond.ID() {
		t.Fatal("events for different streams named the same stream")
	}
}

func TestDescriptorWithoutAnIdentityIsRejected(t *testing.T) {
	typ := schema.Define[eventSchemaID, eventPayload](schema.Traits[eventPayload]{Time: func(eventPayload) (int64, bool) { return 0, true }})
	if _, err := NewDescriptor("", typ.Descriptor(), timing.MustBase(1, 1000), property.New()); err == nil {
		t.Fatal("descriptor without a stream id accepted")
	}
}
