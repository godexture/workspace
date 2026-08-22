package stream

import (
	"testing"

	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/timing"
)

type streamSchemaID struct{}
type streamPayload struct{}
type streamPropertyID struct{}
type streamMetadataID struct{}

func TestDescriptorKeepsStreamLocalPropertiesOutOfItems(t *testing.T) {
	typ := schema.Define[streamSchemaID, streamPayload](schema.Traits[streamPayload]{Time: func(streamPayload) (int64, bool) { return 0, true }})
	rate := property.Define[streamPropertyID](property.Scalar[int]())
	properties, err := rate.Set(property.New(), 48000)
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := NewDescriptor("audio-0", typ.Descriptor(), timing.MustBase(1, 48000), properties)
	if err != nil {
		t.Fatal(err)
	}
	if !descriptor.Valid() || descriptor.Schema() != typ.Identity() || descriptor.TimeBase().Denominator != 48000 {
		t.Fatalf("descriptor = %#v", descriptor)
	}
	if value, ok := rate.Get(descriptor.Properties()); !ok || value != 48000 {
		t.Fatalf("descriptor property = %d, %v", value, ok)
	}
}

func TestUntimedDescriptorRequiresZeroTimeBase(t *testing.T) {
	type untimedID struct{}
	type value struct{}
	typ := schema.Define[untimedID, value](schema.Traits[value]{})
	if _, err := NewDescriptor("bytes", typ.Descriptor(), timing.MustBase(1, 1), property.New()); err != ErrInvalidDescriptor {
		t.Fatalf("nonzero untimed base error = %v", err)
	}
	descriptor, err := NewDescriptor("bytes", typ.Descriptor(), timing.Base{}, property.New())
	if err != nil {
		t.Fatal(err)
	}
	if !descriptor.Valid() || descriptor.HasTimeline() || descriptor.TimeBase() != (timing.Base{}) {
		t.Fatalf("untimed descriptor = %#v", descriptor)
	}
}

func TestDescriptorPresenceChecksDoNotAllocate(t *testing.T) {
	typ := schema.Define[streamSchemaID, streamPayload](schema.Traits[streamPayload]{Time: func(streamPayload) (int64, bool) { return 0, true }})
	descriptor := MustDescriptor("audio", typ.Descriptor(), timing.MustBase(1, 48_000), property.New())
	if allocations := testing.AllocsPerRun(1_000, func() {
		if !descriptor.HasTimeline() || !descriptor.TimeBase().Valid() {
			t.Fatal("timed descriptor changed during allocation check")
		}
	}); allocations != 0 {
		t.Fatalf("descriptor presence check allocations = %v", allocations)
	}
}

func TestDescriptorCarriesImmutableStaticMetadata(t *testing.T) {
	typ := schema.Define[streamSchemaID, streamPayload](schema.Traits[streamPayload]{Time: func(streamPayload) (int64, bool) { return 0, true }})
	descriptor, err := NewDescriptor("audio-0", typ.Descriptor(), timing.MustBase(1, 48000), property.New())
	if err != nil {
		t.Fatal(err)
	}
	title := key.Define[streamMetadataID, string]()
	document, err := metadata.Add(metadata.NewBuilder(metadata.StreamScope), title, "stream title", metadata.Origin{}).Build()
	if err != nil {
		t.Fatal(err)
	}
	withMetadata := descriptor.WithMetadata(document)
	if descriptor.Metadata().Len() != 0 || withMetadata.Metadata().Len() != 1 {
		t.Fatalf("metadata lengths = %d, %d", descriptor.Metadata().Len(), withMetadata.Metadata().Len())
	}
	if value, ok := metadata.First(withMetadata.Metadata(), title); !ok || value != "stream title" {
		t.Fatalf("metadata title = %q, %v", value, ok)
	}
}

func TestDescriptorFingerprintIncludesCanonicalPropertyState(t *testing.T) {
	rate := property.Define[streamPropertyID](property.Scalar[int]())
	leftProperties, err := rate.Set(property.New(), 48000)
	if err != nil {
		t.Fatal(err)
	}
	rightProperties, _ := rate.Set(property.New(), 44100)
	typ := schema.Define[streamSchemaID, streamPayload](schema.Traits[streamPayload]{Time: func(streamPayload) (int64, bool) { return 0, true }})
	left := MustDescriptor("audio", typ.Descriptor(), timing.MustBase(1, 48000), leftProperties)
	same := MustDescriptor("audio", typ.Descriptor(), timing.MustBase(1, 48000), leftProperties)
	right := MustDescriptor("audio", typ.Descriptor(), timing.MustBase(1, 44100), rightProperties)
	leftFingerprint, err := left.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	sameFingerprint, _ := same.Fingerprint()
	rightFingerprint, _ := right.Fingerprint()
	if leftFingerprint != sameFingerprint || !left.SameState(same) {
		t.Fatal("equivalent descriptor state changed fingerprint")
	}
	if leftFingerprint == rightFingerprint || left.SameState(right) {
		t.Fatal("time base/property change did not change descriptor state")
	}
	if _, err := (Descriptor{}).Fingerprint(); err != ErrInvalidDescriptor {
		t.Fatalf("invalid descriptor fingerprint error = %v", err)
	}
}

func TestDescriptorFingerprintIncludesTimelinePresence(t *testing.T) {
	type timelineID struct{}
	type value struct{}
	untimed := schema.Define[timelineID, value](schema.Traits[value]{})
	timed := schema.Define[timelineID, value](schema.Traits[value]{Time: func(value) (int64, bool) { return 0, true }})
	left := MustDescriptor("stream", untimed.Descriptor(), timing.Base{}, property.New())
	right := MustDescriptor("stream", timed.Descriptor(), timing.MustBase(1, 1), property.New())
	leftFingerprint, err := left.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	rightFingerprint, err := right.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if leftFingerprint == rightFingerprint || left.SameState(right) {
		t.Fatal("timeline presence was omitted from descriptor state")
	}
}

func TestDescriptorFingerprintIncludesPayloadType(t *testing.T) {
	type payloadID struct{}
	type firstValue struct{}
	type secondValue struct{}
	firstSchema := schema.Define[payloadID, firstValue](schema.Traits[firstValue]{})
	secondSchema := schema.Define[payloadID, secondValue](schema.Traits[secondValue]{})
	first := MustDescriptor("stream", firstSchema.Descriptor(), timing.Base{}, property.New())
	second := MustDescriptor("stream", secondSchema.Descriptor(), timing.Base{}, property.New())
	firstFingerprint, err := first.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	secondFingerprint, err := second.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if firstFingerprint == secondFingerprint || first.SameState(second) {
		t.Fatal("same marker with different payload types was accepted as the same stream state")
	}
}
