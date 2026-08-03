package stream

import (
	"testing"

	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/timing"
)

type streamSchemaID struct{}
type streamPayload struct{}
type streamPropertyID struct{}

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
