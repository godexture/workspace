package sample

import (
	"testing"

	"github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/timing"
)

type foreignPropertyID struct{}

func TestDescriptionRoundTripAndCanonicalState(t *testing.T) {
	description := Description{Format: S16Interleaved, ValidBits: 16, Rate: 48_000, Layout: Stereo, Endian: LittleEndian}
	properties, err := description.Properties()
	if err != nil {
		t.Fatal(err)
	}
	got, err := FromProperties(properties)
	if err != nil {
		t.Fatal(err)
	}
	if got != description || properties.Len() != 5 || properties.Fingerprint().IsZero() {
		t.Fatalf("description = %#v, properties = %d", got, properties.Len())
	}

	big := description
	big.Endian = BigEndian
	bigProperties, err := big.Properties()
	if err != nil {
		t.Fatal(err)
	}
	if properties.Fingerprint() == bigProperties.Fingerprint() {
		t.Fatal("wire endian did not participate in canonical property state")
	}
}

func TestApplyPreservesUnknownProperties(t *testing.T) {
	foreign := property.Define[foreignPropertyID](property.Scalar[string]())
	properties, err := property.Put(property.New(), foreign, "kept")
	if err != nil {
		t.Fatal(err)
	}
	description := Description{Format: S16Planar, ValidBits: 16, Rate: 44_100, Layout: Mono, Endian: NoEndian}
	properties, err = description.Apply(properties)
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := foreign.Get(properties); !ok || value != "kept" || properties.Len() != 6 {
		t.Fatalf("foreign property = %q, %v; count = %d", value, ok, properties.Len())
	}
}

func TestDescriptionRejectsWireAndTypedEndianMismatch(t *testing.T) {
	values := []Description{
		{Format: S16Interleaved, ValidBits: 16, Rate: 48_000, Layout: Mono, Endian: NoEndian},
		{Format: S16Planar, ValidBits: 16, Rate: 48_000, Layout: Mono, Endian: LittleEndian},
		{Format: F32Planar, ValidBits: 24, Rate: 48_000, Layout: Mono, Endian: NoEndian},
	}
	for _, value := range values {
		if value.Valid() {
			t.Fatalf("invalid description accepted: %#v", value)
		}
	}
}

func TestCanonicalSchemasKeepScalarTypeOutOfIdentity(t *testing.T) {
	if !S16().Valid() || !F32().Valid() || S16().Identity() == F32().Identity() {
		t.Fatal("canonical sample schemas are invalid or share an identity")
	}
	if got := S16().Descriptor().Payload(); got.String() != "audio.Frame[int16]" {
		t.Fatalf("S16 payload = %v", got)
	}
	var _ audio.Frame[int16]
}

func TestCanonicalFrameSchemasExposePTSForQueueWindows(t *testing.T) {
	allocator, err := buffer.NewAllocator(2)
	if err != nil {
		t.Fatal(err)
	}
	planes, err := allocator.FromBytes([]byte{0, 0}, 2)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := audio.NewFrame[int16](timing.SomePTS(timing.NewPTS(7)), 1, planes)
	if err != nil {
		planes.Release()
		t.Fatal(err)
	}
	defer frame.Release()
	if value, ok := S16().Time(frame); !ok || value != 7 {
		t.Fatalf("frame time = %d, %v", value, ok)
	}
}

func TestDeclarationsCoverVocabulary(t *testing.T) {
	declarations := Declarations()
	if len(declarations) != 5 {
		t.Fatalf("declaration count = %d", len(declarations))
	}
	for _, declaration := range declarations {
		if !declaration.Valid() {
			t.Fatalf("invalid declaration: %v", declaration.Problem())
		}
	}
	declarations[0] = declarations[1]
	if next := Declarations(); next[0].Key() == next[1].Key() {
		t.Fatal("Declarations returned shared slice storage")
	}
}
