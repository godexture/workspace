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
	typ := schema.Define[streamSchemaID, streamPayload](schema.Traits[streamPayload]{})
	rate := property.Define[streamPropertyID](property.Scalar[int]())
	properties, err := rate.Set(property.New(), 48000)
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := NewDescriptor("audio-0", typ.Identity(), timing.MustBase(1, 48000), properties)
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

func TestDescriptorCarriesImmutableStaticMetadata(t *testing.T) {
	typ := schema.Define[streamSchemaID, streamPayload](schema.Traits[streamPayload]{})
	descriptor, err := NewDescriptor("audio-0", typ.Identity(), timing.MustBase(1, 48000), property.New())
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
	typ := schema.Define[streamSchemaID, streamPayload](schema.Traits[streamPayload]{})
	left := MustDescriptor("audio", typ.Identity(), timing.MustBase(1, 48000), leftProperties)
	same := MustDescriptor("audio", typ.Identity(), timing.MustBase(1, 48000), leftProperties)
	right := MustDescriptor("audio", typ.Identity(), timing.MustBase(1, 44100), rightProperties)
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
