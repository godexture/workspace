package vorbiscomment

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"reflect"
	"strings"
	"testing"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/media/tag"
)

func TestPictureFieldClosureAndEmptyMIMEConvention(t *testing.T) {
	slot := carrier.Define[testCarrierID]()
	resolver := testResolver(t, slot)
	picture := tag.Artwork{
		Data:          metadata.NewBlob("", []byte{1, 2, 3}),
		Type:          tag.ArtworkFrontCover,
		Description:   "cover",
		Width:         100,
		Height:        200,
		ColorDepth:    24,
		IndexedColors: 2,
	}
	builder := metadata.NewBuilder(metadata.AssetScope)
	metadata.Add(builder, tag.Picture(), picture, metadata.Origin{})
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "comment", metadata.MustAvailable(document))
	if err != nil || len(reports) != 0 {
		t.Fatalf("picture marshal reports %#v, error %v", reports, err)
	}
	_, fields := testFields(t, encoded.AppendTo(nil))
	if len(fields) != 1 || len(fields[0]) <= len("METADATA_BLOCK_PICTURE=") {
		t.Fatalf("picture fields = %#v", fields)
	}
	parsed, err := resolver.Parse(t.Context(), slot, "comment", metadata.AssetScope, encoded)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := metadata.First(parsed, tag.Picture())
	if !ok || got.MediaType != "image/" || got.Type != picture.Type || got.Description != picture.Description || got.Width != picture.Width || got.Height != picture.Height || got.ColorDepth != picture.ColorDepth || got.IndexedColors != picture.IndexedColors || !reflect.DeepEqual(got.Data.AppendTo(nil), []byte{1, 2, 3}) {
		t.Fatalf("picture closure = %#v/%v", got, ok)
	}
	source := testPayload("vendor", "METADATA_BLOCK_PICTURE="+testPictureValue("", tag.ArtworkFrontCover, "raw", 1, 1, []byte{4}))
	parsed, err = resolver.Parse(t.Context(), slot, "comment", metadata.AssetScope, metadata.NewBlob(mediaType, source))
	if err != nil {
		t.Fatal(err)
	}
	got, ok = metadata.First(parsed, tag.Picture())
	if !ok || got.MediaType != "image/" || !reflect.DeepEqual(got.Data.AppendTo(nil), []byte{4}) {
		t.Fatalf("empty MIME source = %#v/%v", got, ok)
	}
	builder = parsed.Edit()
	metadata.Add(builder, tag.Title(), "edited", metadata.Origin{})
	edited, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err = resolver.Marshal(t.Context(), slot, "comment", metadata.MustAvailable(edited))
	if err != nil || len(reports) != 0 {
		t.Fatalf("empty MIME rewrite reports %#v, error %v", reports, err)
	}
	_, fields = testFields(t, encoded.AppendTo(nil))
	if len(fields) != 2 || !strings.HasPrefix(fields[0], "METADATA_BLOCK_PICTURE=") {
		t.Fatalf("empty MIME rewrite fields = %#v", fields)
	}
	wire, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(fields[0], "METADATA_BLOCK_PICTURE="))
	if err != nil || len(wire) < 8 || binary.BigEndian.Uint32(wire[4:]) != 0 {
		t.Fatalf("empty MIME rewrite wire = %x, error %v", wire, err)
	}
}

func TestPictureKeepsMalformedBase64AsSafeOpaque(t *testing.T) {
	slot := carrier.Define[testCarrierID]()
	resolver := testResolver(t, slot)
	valid := testPictureValue("image/png", tag.ArtworkFrontCover, "", 1, 1, []byte{0xfb, 0xff})
	if !strings.HasSuffix(valid, "==") {
		t.Fatalf("picture fixture has no padding: %q", valid)
	}
	url := strings.NewReplacer("+", "-", "/", "_").Replace(valid)
	if url == valid {
		t.Fatalf("picture fixture has no standard-only alphabet: %q", valid)
	}
	for _, test := range []struct {
		name  string
		value string
	}{
		{name: "line break", value: valid[:12] + "\r\n" + valid[12:]},
		{name: "non-zero pad bits", value: testPictureNonZeroPadBits(t, valid)},
		{name: "missing padding", value: strings.TrimSuffix(valid, "==")},
		{name: "URL alphabet", value: url},
	} {
		t.Run(test.name, func(t *testing.T) {
			testSafeOpaquePicture(t, resolver, slot, test.value)
		})
	}
}

func TestPictureKeepsInvalidFileIconAsSafeOpaque(t *testing.T) {
	slot := carrier.Define[testCarrierID]()
	resolver := testResolver(t, slot)
	for _, value := range []string{
		testPictureValue("image/jpeg", tag.ArtworkFileIcon, "", 32, 32, []byte{1}),
		testPictureValue("image/png", tag.ArtworkFileIcon, "", 0, 0, []byte{1}),
	} {
		testSafeOpaquePicture(t, resolver, slot, value)
	}
	builder := metadata.NewBuilder(metadata.AssetScope)
	metadata.Add(builder, tag.Picture(), tag.Artwork{Data: metadata.NewBlob("image/jpeg", []byte{1}), MediaType: "image/jpeg", Type: tag.ArtworkFileIcon, Width: 32, Height: 32}, metadata.Origin{})
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "comment", metadata.MustAvailable(document))
	if err != nil || encoded.Len() == 0 || len(reports) != 1 || reports[0].Loss.Kind != loss.Dropped {
		t.Fatalf("invalid icon = %x, reports %#v, error %v", encoded.AppendTo(nil), reports, err)
	}
	_, fields := testFields(t, encoded.AppendTo(nil))
	if len(fields) != 0 {
		t.Fatalf("invalid icon fields = %#v", fields)
	}
}

func TestPictureKeepsInvalidStructureAsSafeOpaque(t *testing.T) {
	slot := carrier.Define[testCarrierID]()
	resolver := testResolver(t, slot)
	valid := testPictureWire(uint32(tag.ArtworkFrontCover), []byte("image/png"), nil, 1, 1, 24, 0, []byte{1})
	shortData := append([]byte(nil), valid...)
	binary.BigEndian.PutUint32(shortData[len(shortData)-5:], 2)
	for _, test := range []struct {
		name  string
		value string
	}{
		{name: "reserved type", value: base64.StdEncoding.EncodeToString(testPictureWire(21, []byte("image/png"), nil, 1, 1, 24, 0, []byte{1}))},
		{name: "wide type", value: base64.StdEncoding.EncodeToString(testPictureWire(1<<16, []byte("image/png"), nil, 1, 1, 24, 0, []byte{1}))},
		{name: "non-image MIME", value: base64.StdEncoding.EncodeToString(testPictureWire(uint32(tag.ArtworkFrontCover), []byte("application/pdf"), nil, 1, 1, 24, 0, []byte{1}))},
		{name: "linked MIME", value: base64.StdEncoding.EncodeToString(testPictureWire(uint32(tag.ArtworkFrontCover), []byte("-->"), nil, 1, 1, 24, 0, []byte{1}))},
		{name: "parameter MIME", value: base64.StdEncoding.EncodeToString(testPictureWire(uint32(tag.ArtworkFrontCover), []byte("image/png;foo"), nil, 1, 1, 24, 0, []byte{1}))},
		{name: "space MIME", value: base64.StdEncoding.EncodeToString(testPictureWire(uint32(tag.ArtworkFrontCover), []byte("image/png foo"), nil, 1, 1, 24, 0, []byte{1}))},
		{name: "literal image sentinel", value: base64.StdEncoding.EncodeToString(testPictureWire(uint32(tag.ArtworkFrontCover), []byte("image/"), nil, 1, 1, 24, 0, []byte{1}))},
		{name: "invalid description", value: base64.StdEncoding.EncodeToString(testPictureWire(uint32(tag.ArtworkFrontCover), []byte("image/png"), []byte{0xff}, 1, 1, 24, 0, []byte{1}))},
		{name: "data length", value: base64.StdEncoding.EncodeToString(shortData)},
		{name: "trailing data", value: base64.StdEncoding.EncodeToString(append(valid, 0))},
	} {
		t.Run(test.name, func(t *testing.T) {
			testSafeOpaquePicture(t, resolver, slot, test.value)
		})
	}
}

func TestPictureDropsInvalidFreshMediaType(t *testing.T) {
	slot := carrier.Define[testCarrierID]()
	resolver := testResolver(t, slot)
	for _, mediaType := range []string{"image/png;foo", "image/png foo", "image/"} {
		builder := metadata.NewBuilder(metadata.AssetScope)
		metadata.Add(builder, tag.Picture(), tag.Artwork{Data: metadata.NewBlob(mediaType, []byte{1}), MediaType: mediaType, Type: tag.ArtworkFrontCover}, metadata.Origin{})
		document, err := builder.Build()
		if err != nil {
			t.Fatal(err)
		}
		encoded, reports, err := resolver.Marshal(t.Context(), slot, "comment", metadata.MustAvailable(document))
		if err != nil || len(reports) != 1 || reports[0].Loss.Kind != loss.Dropped {
			t.Fatalf("invalid fresh MIME %q = %x, reports %#v, error %v", mediaType, encoded.AppendTo(nil), reports, err)
		}
		_, fields := testFields(t, encoded.AppendTo(nil))
		if len(fields) != 0 {
			t.Fatalf("invalid fresh MIME %q fields = %#v", mediaType, fields)
		}
	}
}

func testPictureNonZeroPadBits(t testing.TB, value string) string {
	t.Helper()
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	index := len(value) - 3
	encoded := strings.IndexByte(alphabet, value[index])
	if encoded < 0 || encoded&0x0f != 0 {
		t.Fatalf("picture fixture has non-canonical pad bits: %q", value)
	}
	return value[:index] + string(alphabet[encoded|1]) + value[index+1:]
}

func testSafeOpaquePicture(t testing.TB, resolver metadata.Resolver, slot carrier.ID, value string) {
	t.Helper()
	field := "METADATA_BLOCK_PICTURE=" + value
	payload := testPayload("vendor", field)
	document, err := resolver.Parse(t.Context(), slot, "comment", metadata.AssetScope, metadata.NewBlob(mediaType, payload))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := metadata.First(document, tag.Picture()); ok {
		t.Fatalf("unsafe picture became semantic: %#v", document)
	}
	blocks := document.Blocks()
	if len(blocks) != 3 || blocks[2].Payload().MediaType() != fieldMediaType || !bytes.Equal(blocks[2].Payload().AppendTo(nil), []byte(field)) {
		t.Fatalf("safe opaque picture field = %#v", blocks)
	}
	builder := document.Edit()
	metadata.Add(builder, tag.Title(), "edited", metadata.Origin{})
	edited, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "comment", metadata.MustAvailable(edited))
	if err != nil || len(reports) != 0 {
		t.Fatalf("safe opaque rewrite reports %#v, error %v", reports, err)
	}
	_, fields := testFields(t, encoded.AppendTo(nil))
	if !reflect.DeepEqual(fields, []string{field, "TITLE=edited"}) || !bytes.Equal([]byte(fields[0]), []byte(field)) {
		t.Fatalf("safe opaque picture rewrite = %#v", fields)
	}
}

func TestPictureUniqueIconsFoldButOtherPicturesDoNot(t *testing.T) {
	slot := carrier.Define[testCarrierID]()
	resolver := testResolver(t, slot)
	builder := metadata.NewBuilder(metadata.AssetScope)
	icon := tag.Artwork{Data: metadata.NewBlob("image/png", []byte{1}), MediaType: "image/png", Type: tag.ArtworkFileIcon, Width: 32, Height: 32}
	metadata.Add(builder, tag.Picture(), icon, metadata.Origin{})
	metadata.Add(builder, tag.Picture(), icon, metadata.Origin{})
	typeTwo := tag.Artwork{Data: metadata.NewBlob("image/png", []byte{5}), MediaType: "image/png", Type: tag.ArtworkType(2)}
	metadata.Add(builder, tag.Picture(), typeTwo, metadata.Origin{})
	metadata.Add(builder, tag.Picture(), typeTwo, metadata.Origin{})
	metadata.Add(builder, tag.Picture(), tag.Artwork{Data: metadata.NewBlob("image/png", []byte{2}), MediaType: "image/png", Type: tag.ArtworkFrontCover}, metadata.Origin{})
	metadata.Add(builder, tag.Picture(), tag.Artwork{Data: metadata.NewBlob("image/png", []byte{3}), MediaType: "image/png", Type: tag.ArtworkFrontCover}, metadata.Origin{})
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "comment", metadata.MustAvailable(document))
	if err != nil || len(reports) != 2 || reports[0].Loss.Kind != loss.Folded || reports[1].Loss.Kind != loss.Folded {
		t.Fatalf("picture fold reports %#v, error %v", reports, err)
	}
	_, fields := testFields(t, encoded.AppendTo(nil))
	if len(fields) != 4 {
		t.Fatalf("picture field count = %#v", fields)
	}
}

func testPictureValue(mediaType string, pictureType tag.ArtworkType, description string, width, height uint32, image []byte) string {
	return base64.StdEncoding.EncodeToString(testPictureWire(uint32(pictureType), []byte(mediaType), []byte(description), width, height, 24, 0, image))
}

func testPictureWire(pictureType uint32, mediaType, description []byte, width, height, depth, colors uint32, image []byte) []byte {
	value := make([]byte, 0, 32+len(mediaType)+len(description)+len(image))
	value = binary.BigEndian.AppendUint32(value, pictureType)
	value = vcAppendPictureString(value, mediaType)
	value = vcAppendPictureString(value, description)
	value = binary.BigEndian.AppendUint32(value, width)
	value = binary.BigEndian.AppendUint32(value, height)
	value = binary.BigEndian.AppendUint32(value, depth)
	value = binary.BigEndian.AppendUint32(value, colors)
	return vcAppendPictureString(value, image)
}

func TestPictureLargeClosure(t *testing.T) {
	slot := carrier.Define[testCarrierID]()
	resolver := testResolver(t, slot)
	image := bytes.Repeat([]byte{0x42}, 64*1024+1)
	builder := metadata.NewBuilder(metadata.AssetScope)
	metadata.Add(builder, tag.Picture(), tag.Artwork{Data: metadata.NewBlob("image/png", image), MediaType: "image/png", Type: tag.ArtworkFrontCover, Description: "large", Width: 400, Height: 400}, metadata.Origin{})
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	encoded, reports, err := resolver.Marshal(t.Context(), slot, "comment", metadata.MustAvailable(document))
	if err != nil || len(reports) != 0 {
		t.Fatalf("large picture marshal reports %#v, error %v", reports, err)
	}
	parsed, err := resolver.Parse(t.Context(), slot, "comment", metadata.AssetScope, encoded)
	if err != nil {
		t.Fatal(err)
	}
	picture, ok := metadata.First(parsed, tag.Picture())
	if !ok || picture.Description != "large" || picture.Width != 400 || picture.Height != 400 || !bytes.Equal(picture.Data.AppendTo(nil), image) {
		t.Fatalf("large picture closure = %#v/%v", picture, ok)
	}
}

func BenchmarkMarshalLargePicture(b *testing.B) {
	slot := carrier.Define[testCarrierID]()
	resolver := testResolver(b, slot)
	image := bytes.Repeat([]byte{0x42}, 1<<20)
	builder := metadata.NewBuilder(metadata.AssetScope)
	metadata.Add(builder, tag.Picture(), tag.Artwork{Data: metadata.NewBlob("image/png", image), MediaType: "image/png", Type: tag.ArtworkFrontCover}, metadata.Origin{})
	document, err := builder.Build()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(image)))
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, _, err := resolver.Marshal(b.Context(), slot, "comment", metadata.MustAvailable(document)); err != nil {
			b.Fatal(err)
		}
	}
}
