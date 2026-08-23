package sample

import (
	"testing"

	"github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/timing"
)

type foreignPropertyID struct{}

func wireDescription() Description {
	return Description{Signal: Signal{Rate: 48_000, Layout: Stereo(), ValidBits: 16}, Coding: S16, Packing: Interleaved, Endian: LittleEndian}
}

func TestDescriptionRoundTripAndCanonicalState(t *testing.T) {
	description := wireDescription()
	properties, err := description.Properties()
	if err != nil {
		t.Fatal(err)
	}
	got, err := FromProperties(properties)
	if err != nil {
		t.Fatal(err)
	}
	if got != description || properties.Len() != 6 || properties.Fingerprint().IsZero() {
		t.Fatalf("description = %#v, properties = %d", got, properties.Len())
	}

	// Every axis has to reach the canonical state a plan is compared by.
	for name, changed := range map[string]Description{
		"endian":    {Signal: Signal{Rate: 48_000, Layout: Stereo(), ValidBits: 16}, Coding: S16, Packing: Interleaved, Endian: BigEndian},
		"coding":    {Signal: Signal{Rate: 48_000, Layout: Stereo(), ValidBits: 16}, Coding: S32, Packing: Interleaved, Endian: LittleEndian},
		"packing":   {Signal: Signal{Rate: 48_000, Layout: Stereo(), ValidBits: 16}, Coding: S16, Packing: Planar, Endian: NoEndian},
		"rate":      {Signal: Signal{Rate: 44_100, Layout: Stereo(), ValidBits: 16}, Coding: S16, Packing: Interleaved, Endian: LittleEndian},
		"layout":    {Signal: Signal{Rate: 48_000, Layout: Channels(2), ValidBits: 16}, Coding: S16, Packing: Interleaved, Endian: LittleEndian},
		"validBits": {Signal: Signal{Rate: 48_000, Layout: Stereo(), ValidBits: 12}, Coding: S16, Packing: Interleaved, Endian: LittleEndian},
	} {
		other, err := changed.Properties()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if properties.Fingerprint() == other.Fingerprint() {
			t.Fatalf("%s did not participate in canonical property state", name)
		}
	}
}

func TestApplyPreservesUnknownProperties(t *testing.T) {
	foreign := property.Define[foreignPropertyID](property.Scalar[string]())
	properties, err := property.Put(property.New(), foreign, "kept")
	if err != nil {
		t.Fatal(err)
	}
	properties, err = wireDescription().Decoded().Apply(properties)
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := foreign.Get(properties); !ok || value != "kept" || properties.Len() != 7 {
		t.Fatalf("foreign property = %q, %v; count = %d", value, ok, properties.Len())
	}
}

// Widening records the effective depth in ValidBits instead of adding a schema
// per wire width, and never changes the scale a frame is stored at.
func TestDecodedWidensCodingAndKeepsValidBits(t *testing.T) {
	cases := map[Coding]struct {
		coding Coding
		bits   int
	}{
		U8:  {S16, 8},
		S24: {S32, 24},
		F32: {F32, 32},
	}
	for wire, want := range cases {
		description := Description{Signal: Signal{Rate: 48_000, Layout: Mono(), ValidBits: wire.Bits()}, Coding: wire, Packing: Interleaved, Endian: LittleEndian}
		if wire.Bytes() == 1 {
			description.Endian = NoEndian
		}
		decoded := description.Decoded()
		if !description.Valid() || !decoded.Valid() {
			t.Fatalf("%s: wire %v decoded %v", wire, description.Valid(), decoded.Valid())
		}
		if decoded.Coding != want.coding || decoded.ValidBits != want.bits || decoded.Packing != Planar || decoded.Endian != NoEndian {
			t.Fatalf("%s decoded = %#v", wire, decoded)
		}
		if _, ok := Schema(decoded.Coding); !ok {
			t.Fatalf("%s has no canonical schema", decoded.Coding)
		}
	}
}

func TestInconsistentDescriptionsAreRejected(t *testing.T) {
	cases := map[string]Description{
		"interleaved without byte order": {Signal: Signal{Rate: 48_000, Layout: Mono(), ValidBits: 16}, Coding: S16, Packing: Interleaved, Endian: NoEndian},
		"planar with byte order":         {Signal: Signal{Rate: 48_000, Layout: Mono(), ValidBits: 16}, Coding: S16, Packing: Planar, Endian: LittleEndian},
		"byte order on a single byte":    {Signal: Signal{Rate: 48_000, Layout: Mono(), ValidBits: 8}, Coding: U8, Packing: Interleaved, Endian: LittleEndian},
		"planar wire-only coding":        {Signal: Signal{Rate: 48_000, Layout: Mono(), ValidBits: 24}, Coding: S24, Packing: Planar, Endian: NoEndian},
		"valid bits wider than coding":   {Signal: Signal{Rate: 48_000, Layout: Mono(), ValidBits: 24}, Coding: S16, Packing: Interleaved, Endian: LittleEndian},
		"narrowed float":                 {Signal: Signal{Rate: 48_000, Layout: Mono(), ValidBits: 24}, Coding: F32, Packing: Planar, Endian: NoEndian},
		"no layout":                      {Signal: Signal{Rate: 48_000, ValidBits: 16}, Coding: S16, Packing: Interleaved, Endian: LittleEndian},
		"no rate":                        {Signal: Signal{Layout: Mono(), ValidBits: 16}, Coding: S16, Packing: Interleaved, Endian: LittleEndian},
	}
	for name, value := range cases {
		if value.Valid() {
			t.Errorf("%s was accepted", name)
		}
		if _, err := value.Properties(); err == nil {
			t.Errorf("%s produced properties", name)
		}
	}
}

func TestSchemaCoversOnlyCanonicalCodings(t *testing.T) {
	if got, ok := Schema(S16); !ok || !got.Equal(Frames[int16]().Descriptor()) {
		t.Fatal("S16 does not resolve to the canonical int16 frame schema")
	}
	for _, coding := range []Coding{U8, S8, S24, Coding("s20")} {
		if _, ok := Schema(coding); ok {
			t.Errorf("%s resolved to a frame schema", coding)
		}
	}
	first, _ := Schema(S32)
	second, _ := Schema(F32)
	if first.Equal(second) {
		t.Fatal("canonical schemas share an identity")
	}
}

func TestDeclarationsCoverVocabulary(t *testing.T) {
	declarations := Declarations()
	if len(declarations) != 6 {
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

func TestBlockBytesCoversEveryChannel(t *testing.T) {
	description := Description{Signal: Signal{Rate: 48_000, Layout: Channels(6), ValidBits: 24}, Coding: S24, Packing: Interleaved, Endian: LittleEndian}
	if got := description.BlockBytes(); got != 18 {
		t.Fatalf("block bytes = %d", got)
	}
}

// The four canonical schemas exist so a decoded frame has one representation
// per Go scalar type. Frames keeps that mapping in one place instead of a
// per-codec switch, and reports an unsupported scalar type as an invalid
// schema the declaring port surfaces.
func TestFramesCoversEveryCanonicalScalarType(t *testing.T) {
	identities := map[string]struct{}{}
	for _, descriptor := range []schema.Descriptor{
		Frames[int16]().Descriptor(), Frames[int32]().Descriptor(),
		Frames[float32]().Descriptor(), Frames[float64]().Descriptor(),
	} {
		if !descriptor.Valid() {
			t.Fatalf("canonical schema is invalid: %v", descriptor.Problem())
		}
		identities[descriptor.Identity().String()] = struct{}{}
	}
	if len(identities) != 4 {
		t.Fatalf("canonical schema identities = %d, want 4", len(identities))
	}
	if Frames[uint8]().Valid() || Frames[int64]().Valid() {
		t.Fatal("a scalar type outside the canonical four produced a usable schema")
	}
}

func TestFramesCarriesOwnershipAndTimingTraits(t *testing.T) {
	allocator, err := buffer.NewAllocator(8)
	if err != nil {
		t.Fatal(err)
	}
	planes, err := allocator.FromBytes([]byte{1, 2, 3, 4, 5, 6, 7, 8}, 8)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := audio.NewFrame[float64](timing.SomePTS(timing.NewPTS(9)), 1, planes)
	if err != nil {
		planes.Release()
		t.Fatal(err)
	}
	frames := Frames[float64]()
	if value, ok := frames.Time(frame); !ok || value != 9 {
		t.Fatalf("frame time = %d, %v", value, ok)
	}
	if size, ok := frames.Size(frame); !ok || size != 8 {
		t.Fatalf("frame size = %d, %v", size, ok)
	}
	frames.Drop(frames.Fork(frame))
	if !frame.Valid() {
		t.Fatal("dropping a fork released the original frame")
	}
	frames.Drop(frame)
	if used := allocator.Used(); used != 0 {
		t.Fatalf("allocator retained %d bytes", used)
	}
}

func TestStoresMatchesWireCodingsToTheirFrameType(t *testing.T) {
	if CodingOf[int16]() != S16 || CodingOf[float64]() != F64 || CodingOf[uint8]() != "" {
		t.Fatal("scalar type does not map to its canonical coding")
	}
	for coding, want := range map[Coding]bool{U8: true, S8: true, S16: true, S24: false, F32: false} {
		if got := Stores[int16](coding); got != want {
			t.Errorf("Stores[int16](%s) = %v, want %v", coding, got, want)
		}
	}
	if !Stores[int32](S24) || !Stores[int32](S32) || Stores[int32](S16) {
		t.Error("24 and 32 bit wire codings do not both store in int32 frames")
	}
	if Stores[uint8](U8) {
		t.Error("a scalar type outside the canonical four accepted a coding")
	}
}
