package sample

import (
	"testing"

	"github.com/godexture/godec/media/audio"
)

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
