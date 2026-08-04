package stream

import (
	"testing"

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
	rate := property.Define[streamPropertyID, int]()
	properties, err := rate.Set(property.New(), 48000)
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := NewDescriptor(typ.Identity(), timing.MustBase(1, 48000), properties)
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
	descriptor, err := NewDescriptor(typ.Identity(), timing.MustBase(1, 48000), property.New())
	if err != nil {
		t.Fatal(err)
	}
	title := metadata.DefineKey[streamMetadataID, string]()
	document, err := metadata.Add(metadata.NewBuilder(metadata.StreamScope), title, "stream title", metadata.Origin{}).Build()
	if err != nil {
		t.Fatal(err)
	}
	withMetadata := descriptor.WithMetadata(document)
	if descriptor.Metadata().Len() != 0 || withMetadata.Metadata().Len() != 1 {
		t.Fatalf("metadata lengths = %d, %d", descriptor.Metadata().Len(), withMetadata.Metadata().Len())
	}
	if value, ok := title.First(withMetadata.Metadata()); !ok || value != "stream title" {
		t.Fatalf("metadata title = %q, %v", value, ok)
	}
}
