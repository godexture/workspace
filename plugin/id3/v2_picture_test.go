package id3

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/media/tag"
)

func TestV2ParsesAPICAndPICWithImmutableImagePayload(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	image := bytes.Repeat([]byte{0x7f}, 1<<20)
	for _, test := range []struct {
		name        string
		version     byte
		frameID     string
		frameData   []byte
		mediaType   string
		pictureType tag.ArtworkType
		description string
	}{
		{
			name: "v2.4 APIC", version: 4, frameID: "APIC",
			frameData: append(append([]byte{3}, append([]byte("image/png"), 0, byte(tag.ArtworkFrontCover), 'c', 'o', 'v', 'e', 'r', 0)...), image...),
			mediaType: "image/png", pictureType: tag.ArtworkFrontCover, description: "cover",
		},
		{
			name: "v2.3 APIC", version: 3, frameID: "APIC",
			frameData: append(append([]byte{0}, append([]byte("image/jpeg"), 0, byte(tag.ArtworkBackCover), 'b', 'a', 'c', 'k', 0)...), image...),
			mediaType: "image/jpeg", pictureType: tag.ArtworkBackCover, description: "back",
		},
		{
			name: "v2.2 PIC", version: 2, frameID: "PIC",
			frameData: append([]byte{0, 'P', 'N', 'G', byte(tag.ArtworkFrontCover), 0}, image...),
			mediaType: "image/png", pictureType: tag.ArtworkFrontCover, description: "",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := v2TestTagVersion(test.version, 0, v2TestFrame(test.version, test.frameID, test.frameData, [2]byte{}))
			document, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, payload))
			if err != nil {
				t.Fatal(err)
			}
			picture, ok := metadata.First(document, tag.Picture())
			if !ok || picture.MediaType != test.mediaType || picture.Type != test.pictureType || picture.Description != test.description || picture.Data.Len() != len(image) || !bytes.Equal(picture.Data.AppendTo(nil), image) {
				t.Fatalf("picture = %#v/%v", picture, ok)
			}
			encoded, reports, err := resolver.Marshal(t.Context(), slot, "head", metadata.MustAvailable(document))
			if err != nil || len(reports) != 0 || !bytes.Equal(encoded.AppendTo(nil), payload) {
				t.Fatalf("unchanged picture = %d bytes, reports %#v, error %v", encoded.Len(), reports, err)
			}
		})
	}
}

func TestV2CanonicalizesPictureAndReportsUnrepresentableDimensions(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	builder := metadata.NewBuilder(metadata.StreamScope)
	metadata.Add(builder, tag.Picture(), tag.Artwork{
		Data: metadata.NewBlob("image/png", []byte{1, 2, 3}), MediaType: "image/png",
		Type: tag.ArtworkFrontCover, Description: "cover", Width: 100, Height: 100,
	}, metadata.Origin{})
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "head", metadata.MustAvailable(document))
	if err != nil || !bytes.Contains(encoded.AppendTo(nil), []byte("APIC")) {
		t.Fatalf("canonical picture = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
	got := make([]loss.Loss, len(reports))
	for index, report := range reports {
		got[index] = report.Loss
	}
	want := []loss.Loss{{Key: tag.Picture().ID(), Kind: loss.Truncated, Native: "APIC", Detail: "id3v2.apic-dimensions"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("picture reports = %#v, want %#v", got, want)
	}
}

func TestV2RetainsInvalidPictureFramesOpaque(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	for _, test := range []struct {
		version byte
		frame   []byte
	}{
		{version: 4, frame: v2BuildFrame("APIC", []byte{3, 0, byte(tag.ArtworkFrontCover), 0, 1})},
		{version: 4, frame: v2BuildFrame("APIC", []byte{3, '-', '-', '>', 0, byte(tag.ArtworkFrontCover), 0, 1})},
		{version: 4, frame: v2BuildFrame("APIC", []byte{3, 'f', 'o', 'o', 0, byte(tag.ArtworkFrontCover), 0, 1})},
		{version: 4, frame: v2BuildFrame("APIC", []byte{3, 'a', 'p', 'p', 'l', 'i', 'c', 'a', 't', 'i', 'o', 'n', '/', 'p', 'd', 'f', 0, byte(tag.ArtworkFrontCover), 0, 1})},
		{version: 4, frame: v2BuildFrame("APIC", []byte{3, 'i', 'm', 'a', 'g', 'e', '/', 'p', 'n', 'g', ';', 'f', 'o', 'o', 0, byte(tag.ArtworkFrontCover), 0, 1})},
		{version: 4, frame: v2BuildFrame("APIC", []byte{3, 'i', 'm', 'a', 'g', 'e', '/', 'p', 'n', 'g', ' ', 'f', 'o', 'o', 0, byte(tag.ArtworkFrontCover), 0, 1})},
		{version: 4, frame: v2BuildFrame("APIC", []byte{3, 'i', 'm', 'a', 'g', 'e', '/', 0, byte(tag.ArtworkFrontCover), 0, 1})},
		{version: 4, frame: v2BuildFrame("APIC", []byte{3, 'i', 'm', 'a', 'g', 'e', '/', 'p', 'n', 'g', 0, 0x15, 0, 1})},
		{version: 4, frame: v2BuildFrame("APIC", []byte{3, 'i', 'm', 'a', 'g', 'e', '/', 'p', 'n', 'g', 0, byte(tag.ArtworkFrontCover), 0})},
		{version: 2, frame: v2TestFrame(2, "PIC", []byte{0, 'G', 'I', 'F', byte(tag.ArtworkFrontCover), 0, 1}, [2]byte{})},
	} {
		payload := v2TestTagVersion(test.version, 0, test.frame)
		document, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, payload))
		if err != nil {
			t.Fatal(err)
		}
		if len(document.Entries()) != 0 || len(document.Blocks()) != 2 || document.Blocks()[1].Source() {
			t.Fatalf("invalid picture was not opaque: %#v", document)
		}
	}
}

func TestV2EditingSourceRewritesAPICAsCanonicalV24(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	image := []byte{9, 8, 7}
	source := v2TestTagVersion(3, 0, v2TestFrame(3, "APIC", append(append([]byte{0}, append([]byte("image/jpeg"), 0, byte(tag.ArtworkFrontCover), 'c', 0)...), image...), [2]byte{}))
	document, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, source))
	if err != nil {
		t.Fatal(err)
	}
	builder := document.Edit()
	metadata.Add(builder, tag.Title(), "edited", metadata.Origin{})
	edited, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "head", metadata.MustAvailable(edited))
	if err != nil || len(reports) != 0 {
		t.Fatalf("edited APIC = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
	value := encoded.AppendTo(nil)
	if !bytes.HasPrefix(value, []byte{'I', 'D', '3', 4, 0, 0}) || !bytes.Contains(value, []byte("APIC")) || !bytes.Contains(value, image) {
		t.Fatalf("canonical APIC = %x", value)
	}
}

func TestV2UnsynchronisedAPICDecodesImageBytes(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	logical := []byte{0, 'i', 'm', 'a', 'g', 'e', '/', 'p', 'n', 'g', 0, byte(tag.ArtworkFrontCover), 0, 0xff, 0}
	oldFrame := v2TestUnsynchronise(v2TestFrame(3, "APIC", logical, [2]byte{}))
	oldPayload := v2TestTagVersion(3, 0x80, oldFrame)
	newWire := v2TestFrame(4, "APIC", v2TestUnsynchronise(logical), [2]byte{0, 0x02})
	newPayload := v2TestTagVersion(4, 0, newWire)
	for _, payload := range [][]byte{oldPayload, newPayload} {
		document, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, metadata.NewBlob(v2MediaType, payload))
		if err != nil {
			t.Fatal(err)
		}
		picture, ok := metadata.First(document, tag.Picture())
		if !ok || !bytes.Equal(picture.Data.AppendTo(nil), []byte{0xff, 0}) {
			t.Fatalf("unsynchronised APIC = %#v/%v", picture, ok)
		}
		encoded, reports, err := resolver.Marshal(t.Context(), slot, "head", metadata.MustAvailable(document))
		if err != nil || len(reports) != 0 || !bytes.Equal(encoded.AppendTo(nil), payload) {
			t.Fatalf("unchanged unsynchronised APIC = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
		}
	}
}

func TestV2DropsFreshPictureWithNonMIMEOrReservedType(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	for _, picture := range []tag.Artwork{
		{Data: metadata.NewBlob("-->", []byte{1}), MediaType: "-->", Type: tag.ArtworkFrontCover},
		{Data: metadata.NewBlob("application/pdf", []byte{1}), MediaType: "application/pdf", Type: tag.ArtworkFrontCover},
		{Data: metadata.NewBlob("image/png;foo", []byte{1}), MediaType: "image/png;foo", Type: tag.ArtworkFrontCover},
		{Data: metadata.NewBlob("image/png foo", []byte{1}), MediaType: "image/png foo", Type: tag.ArtworkFrontCover},
		{Data: metadata.NewBlob("image/", []byte{1}), MediaType: "image/", Type: tag.ArtworkFrontCover},
		{Data: metadata.NewBlob("image/png", []byte{1}), MediaType: "image/png", Type: tag.ArtworkType(0x15)},
		{Data: metadata.NewBlob("image/png", nil), MediaType: "image/png", Type: tag.ArtworkFrontCover},
	} {
		builder := metadata.NewBuilder(metadata.StreamScope)
		metadata.Add(builder, tag.Picture(), picture, metadata.Origin{})
		document, err := builder.Build()
		if err != nil {
			t.Fatal(err)
		}
		encoded, reports, err := resolver.Marshal(t.Context(), slot, "head", metadata.MustAvailable(document))
		if err != nil || encoded.Len() != 0 || len(reports) != 1 || reports[0].Loss.Kind != loss.Dropped {
			t.Fatalf("invalid fresh picture = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
		}
	}
}

func TestV2FreshLargeAPICUsesSyncSafeFrameSize(t *testing.T) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(t, slot)
	image := bytes.Repeat([]byte{0x42}, (1<<20)+37)
	builder := metadata.NewBuilder(metadata.StreamScope)
	metadata.Add(builder, tag.Picture(), tag.Artwork{
		Data: metadata.NewBlob("image/png", image), MediaType: "image/png", Type: tag.ArtworkFrontCover,
	}, metadata.Origin{})
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "head", metadata.MustAvailable(document))
	if err != nil || len(reports) != 0 {
		t.Fatalf("large APIC marshal reports %#v, error %v", reports, err)
	}
	parsed, err := resolver.Parse(t.Context(), slot, "head", metadata.StreamScope, encoded)
	if err != nil {
		t.Fatal(err)
	}
	picture, ok := metadata.First(parsed, tag.Picture())
	if !ok || picture.Data.Len() != len(image) || !bytes.Equal(picture.Data.AppendTo(nil), image) {
		t.Fatalf("large APIC = %#v/%v", picture, ok)
	}
}

func BenchmarkV2ParseLargeAPIC(b *testing.B) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(b, slot)
	image := bytes.Repeat([]byte{0x42}, 1<<20)
	frame := v2BuildFrame("APIC", append(append([]byte{3}, append([]byte("image/jpeg"), 0, byte(tag.ArtworkFrontCover), 0)...), image...))
	payload := metadata.NewBlob(v2MediaType, v2TestTag(frame))
	b.ReportAllocs()
	b.SetBytes(int64(payload.Len()))
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := resolver.Parse(b.Context(), slot, "head", metadata.StreamScope, payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkV2MarshalLargeAPIC(b *testing.B) {
	slot := carrier.Define[v2TestCarrierID]()
	resolver := v2TestResolver(b, slot)
	image := bytes.Repeat([]byte{0x42}, 1<<20)
	builder := metadata.NewBuilder(metadata.StreamScope)
	metadata.Add(builder, tag.Picture(), tag.Artwork{
		Data: metadata.NewBlob("image/png", image), MediaType: "image/png", Type: tag.ArtworkFrontCover,
	}, metadata.Origin{})
	document, err := builder.Build()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(image)))
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, _, err := resolver.Marshal(b.Context(), slot, "head", metadata.MustAvailable(document)); err != nil {
			b.Fatal(err)
		}
	}
}
