package id3

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/sdk/date"
)

func TestParseHeader(t *testing.T) {
	header, err := ParseHeader([]byte{'I', 'D', '3', 0x04, 0x00, 0x10, 0x00, 0x00, 0x00, 0x21})
	if err != nil {
		t.Fatalf("ParseHeader returned error: %v", err)
	}
	if header.VersionMajor != 4 {
		t.Fatalf("VersionMajor = %d, want 4", header.VersionMajor)
	}
	if header.TagSize != 33 {
		t.Fatalf("TagSize = %d, want 33", header.TagSize)
	}
	if !header.HasFooter() {
		t.Fatalf("HasFooter = false, want true")
	}
	if header.TotalSize() != 53 {
		t.Fatalf("TotalSize = %d, want 53", header.TotalSize())
	}
}

func TestParseHeaderRejectsInvalidSize(t *testing.T) {
	_, err := ParseHeader([]byte{'I', 'D', '3', 0x03, 0x00, 0x00, 0x80, 0x00, 0x00, 0x00})
	if !errors.Is(err, ErrInvalidHeader) {
		t.Fatalf("ParseHeader error = %v, want ErrInvalidHeader", err)
	}
}

func TestSkipV2(t *testing.T) {
	tag1 := append([]byte{'I', 'D', '3', 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03}, []byte("abc")...)
	tag2 := append([]byte{'I', 'D', '3', 0x04, 0x00, 0x10, 0x00, 0x00, 0x00, 0x02}, []byte("de1234567890")...)
	payload := append(append(tag1, tag2...), []byte{0xFF, 0xFB, 0x90, 0x00}...)

	br := bufio.NewReader(bytes.NewReader(payload))
	skipped, err := SkipID3v2(br)
	if err != nil {
		t.Fatalf("SkipID3v2 returned error: %v", err)
	}
	if skipped != len(tag1)+len(tag2) {
		t.Fatalf("skipped = %d, want %d", skipped, len(tag1)+len(tag2))
	}
	next, err := br.Peek(4)
	if err != nil {
		t.Fatalf("Peek returned error: %v", err)
	}
	if !bytes.Equal(next, []byte{0xFF, 0xFB, 0x90, 0x00}) {
		t.Fatalf("next bytes = %v, want MP3 sync", next)
	}
}

func TestParse_ImportantFrames(t *testing.T) {
	titleFrame := append([]byte("TIT2"), []byte{0x00, 0x00, 0x00, 0x06, 0x00, 0x00, 0x03, 'T', 'i', 't', 'l', 'e'}...)
	artistFrame := append([]byte("TPE1"), []byte{0x00, 0x00, 0x00, 0x07, 0x00, 0x00, 0x03, 'A', 'r', 't', 'i', 's', 't'}...)
	albumFrame := append([]byte("TALB"), []byte{0x00, 0x00, 0x00, 0x06, 0x00, 0x00, 0x03, 'A', 'l', 'b', 'u', 'm'}...)
	albumArtistFrame := append([]byte("TPE2"), []byte{0x00, 0x00, 0x00, 0x06, 0x00, 0x00, 0x03, 'B', 'a', 'n', 'd', 'X'}...)
	discFrame := append([]byte("TPOS"), []byte{0x00, 0x00, 0x00, 0x04, 0x00, 0x00, 0x03, '1', '/', '2'}...)
	tdrcFrame := append([]byte("TDRC"), []byte{0x00, 0x00, 0x00, 0x0B, 0x00, 0x00, 0x03, '2', '0', '2', '4', '-', '0', '6', '-', '1', '7'}...)
	composerFrame := append([]byte("TCOM"), []byte{0x00, 0x00, 0x00, 0x09, 0x00, 0x00, 0x03, 'C', 'o', 'm', 'p', 'o', 's', 'e', 'r'}...)
	lyricsFrame := append([]byte("USLT"), []byte{0x00, 0x00, 0x00, 0x0F, 0x00, 0x00, 0x03, 'e', 'n', 'g', 0x00, 'L', 'y', 'r', 'i', 'c', 's', ' ', 'g', 'o', '!'}...)
	encoderFrame := append([]byte("TENC"), []byte{0x00, 0x00, 0x00, 0x08, 0x00, 0x00, 0x03, 'L', 'A', 'M', 'E', '3', '.', '1'}...)
	copyrightFrame := append([]byte("TCOP"), []byte{0x00, 0x00, 0x00, 0x08, 0x00, 0x00, 0x03, '(', 'c', ')', '2', '0', '2', '4'}...)
	wxxxFrame := append([]byte("WXXX"), []byte{0x00, 0x00, 0x00, 0x19, 0x00, 0x00, 0x03, 'h', 'o', 'm', 'e', 0x00, 'h', 't', 't', 'p', 's', ':', '/', '/', 'e', 'x', 'a', 'm', 'p', 'l', 'e', '.', 'c', 'o', 'm'}...)
	woarFrame := append([]byte("WOAR"), []byte{0x00, 0x00, 0x00, 0x16, 0x00, 0x00, 'h', 't', 't', 'p', 's', ':', '/', '/', 'a', 'r', 't', 'i', 's', 't', '.', 'e', 'x', 'a', 'm', 'p', 'l', 'e'}...)
	apicFrame := append([]byte("APIC"), []byte{0x00, 0x00, 0x00, 0x11, 0x00, 0x00, 0x03, 'i', 'm', 'a', 'g', 'e', '/', 'p', 'n', 'g', 0x00, 0x03, 0x00, 0x89, 'P', 'N', 'G'}...)
	privFrame := append([]byte("PRIV"), []byte{0x00, 0x00, 0x00, 0x08, 0x00, 0x00, 'o', 'w', 'n', 'e', 'r', 0x00, 0x01, 0x02}...)
	tagPayload := bytes.Join([][]byte{
		titleFrame, artistFrame, albumFrame, albumArtistFrame, discFrame, tdrcFrame,
		composerFrame, lyricsFrame, encoderFrame, copyrightFrame, wxxxFrame,
		woarFrame, apicFrame, privFrame,
	}, nil)
	tagHeader := makeTagHeader(0x03, len(tagPayload))

	bundle, err := Parse(append(tagHeader, tagPayload...))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	date, _ := date.NewPartial("2024-06-17")

	metadata.AssertBundleValue(t, bundle, metadata.KeyTitle("Title"))
	metadata.AssertBundleValue(t, bundle, metadata.KeyArtist("Artist"))
	metadata.AssertBundleSlice(t, bundle, []metadata.KeyArtist{"BandX"})
	metadata.AssertBundleValue(t, bundle, metadata.KeyAlbum("Album"))
	// metadata.AssertBundleValue(t, bundle, metadata.KeyAlbumArtist("BandX"))
	// metadata.AssertBundleValue(t, bundle, metadata.KeyDiscNumber("1/2"))
	metadata.AssertBundleValue(t, bundle, metadata.KeyDate(date))
	metadata.AssertBundleValue(t, bundle, metadata.KeyComposer("Composer"))
	metadata.AssertBundleValue(t, bundle, metadata.KeyLyrics("Lyrics go!"))
	metadata.AssertBundleValue(t, bundle, metadata.KeyEncoder("LAME3.1"))
	metadata.AssertBundleValue(t, bundle, metadata.KeyCopyright("(c)2024"))
	metadata.AssertBundleValue(t, bundle, metadata.KeyWebsite("https://example.com"))
	// metadata.AssertBundleValue(t, bundle, metadata.KeyArtistURL("https://artist.example"))

	thumbnails := metadata.Get[metadata.KeyThumbnail](bundle)
	if len(thumbnails) != 1 {
		t.Fatalf("len(KeyThumbnails) = %d, want 1", len(thumbnails))
	}
	if thumbnails[0].MIMEType != "image/png" {
		t.Fatalf("KeyThumbnails[0].MIMEType = %q, want image/png", thumbnails[0].MIMEType)
	}
	if thumbnails[0].PictureType != metadata.PictureTypeFrontCover {
		t.Fatalf("KeyThumbnails[0].PictureType = %d, want front cover", thumbnails[0].PictureType)
	}
	if !bytes.Equal(thumbnails[0].Data, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatalf("KeyThumbnails[0].Data = %v, want PNG bytes", thumbnails[0].Data)
	}

	// pic, err := metadata.Get[metadata.AttachedPicture](bundle, KeyAttachedPic)
	// if err != nil {
	// 	t.Fatalf("Get(KeyAttachedPic) returned error: %v", err)
	// }
	// if pic.MIMEType != "image/png" || pic.PictureType != 0x03 || !bytes.Equal(pic.Data, []byte{0x89, 'P', 'N', 'G'}) {
	// 	t.Fatalf("attached picture = %+v, want PNG front cover", pic)
	// }

	// rawFrames, err := metadata.Get[[]metadata.RawFrame](bundle, KeyRawFrames)
	// if err != nil {
	// 	t.Fatalf("Get(KeyRawFrames) returned error: %v", err)
	// }
	// if len(rawFrames) != 1 {
	// 	t.Fatalf("len(rawFrames) = %d, want 1", len(rawFrames))
	// }
	// if rawFrames[0].ID != "PRIV" {
	// 	t.Fatalf("rawFrames[0].ID = %q, want PRIV", rawFrames[0].ID)
	// }
}

func TestParse_LegacyDateFrames_WithTime(t *testing.T) {
	tyerFrame := append([]byte("TYER"), []byte{0x00, 0x00, 0x00, 0x05, 0x00, 0x00, 0x03, '2', '0', '2', '4'}...)
	tdatFrame := append([]byte("TDAT"), []byte{0x00, 0x00, 0x00, 0x05, 0x00, 0x00, 0x03, '1', '7', '0', '6'}...)
	timeFrame := append([]byte("TIME"), []byte{0x00, 0x00, 0x00, 0x05, 0x00, 0x00, 0x03, '1', '2', '3', '4'}...)
	tagPayload := bytes.Join([][]byte{tyerFrame, tdatFrame, timeFrame}, nil)
	tagHeader := makeTagHeader(0x03, len(tagPayload))

	bundle, err := Parse(append(tagHeader, tagPayload...))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	parsedDate := date.Partial(metadata.Get[metadata.KeyDate](bundle))
	if !parsedDate.Year().Exists() || parsedDate.Year().Unwrap() != 2024 {
		t.Fatalf("expected year 2024, got %v", parsedDate.Year())
	}
	if !parsedDate.Month().Exists() || parsedDate.Month().Unwrap() != 6 {
		t.Fatalf("expected month 6, got %v", parsedDate.Month())
	}
	if !parsedDate.Day().Exists() || parsedDate.Day().Unwrap() != 17 {
		t.Fatalf("expected day 17, got %v", parsedDate.Day())
	}
	if !parsedDate.Hour().Exists() || parsedDate.Hour().Unwrap() != 12 {
		t.Fatalf("expected hour 12, got %v", parsedDate.Hour())
	}
	if !parsedDate.Minute().Exists() || parsedDate.Minute().Unwrap() != 34 {
		t.Fatalf("expected minute 34, got %v", parsedDate.Minute())
	}
}

func TestParse_WOAR_WOAS_FallbackToWebsite(t *testing.T) {
	woarFrame := append([]byte("WOAR"), []byte{0x00, 0x00, 0x00, 0x16, 0x00, 0x00, 'h', 't', 't', 'p', 's', ':', '/', '/', 'a', 'r', 't', 'i', 's', 't', '.', 'e', 'x', 'a', 'm', 'p', 'l', 'e'}...)
	woasFrame := append([]byte("WOAS"), []byte{0x00, 0x00, 0x00, 0x15, 0x00, 0x00, 'h', 't', 't', 'p', 's', ':', '/', '/', 'a', 'l', 'b', 'u', 'm', '.', 'e', 'x', 'a', 'm', 'p', 'l', 'e'}...)

	t.Run("WOAR", func(t *testing.T) {
		tagHeader := makeTagHeader(0x03, len(woarFrame))
		bundle, err := Parse(append(tagHeader, woarFrame...))
		if err != nil {
			t.Fatalf("Parse returned error: %v", err)
		}
		metadata.AssertBundleSlice(t, bundle, []metadata.KeyWebsite{"https://artist.example"})
	})

	t.Run("WOAS", func(t *testing.T) {
		tagHeader := makeTagHeader(0x03, len(woasFrame))
		bundle, err := Parse(append(tagHeader, woasFrame...))
		if err != nil {
			t.Fatalf("Parse returned error: %v", err)
		}
		metadata.AssertBundleSlice(t, bundle, []metadata.KeyWebsite{"https://album.example"})
	})
}

func TestParse_ID3v1Fallback(t *testing.T) {
	audio := []byte{0xFF, 0xFB, 0x90, 0x00}
	tag := make([]byte, V1TagSize)
	copy(tag[:3], []byte("TAG"))
	copy(tag[3:33], []byte("Song"))
	copy(tag[33:63], []byte("Singer"))
	copy(tag[63:93], []byte("Album"))
	copy(tag[93:97], []byte("1999"))
	copy(tag[97:125], []byte("Comment"))
	tag[125] = 0
	tag[126] = 7
	tag[127] = 17

	bundle, err := Parse(append(audio, tag...))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	metadata.AssertBundleValue(t, bundle, metadata.KeyTitle("Song"))
	metadata.AssertBundleValue(t, bundle, metadata.KeyArtist("Singer"))
	metadata.AssertBundleValue(t, bundle, metadata.KeyAlbum("Album"))
	parsedDate := date.Partial(metadata.Get[metadata.KeyDate](bundle))
	if !parsedDate.Year().Exists() || parsedDate.Year().Unwrap() != 1999 {
		t.Errorf("expected year 1999, got %v", parsedDate.Year())
	}
	// metadata.AssertBundleValue(t, bundle, metadata.KeyTrackNumber("7"))
	metadata.AssertBundleValue(t, bundle, metadata.KeyGenre("Rock"))
}

func TestMarshal_RoundTrip_Importantmetadata(t *testing.T) {
	date, _ := date.NewPartial("2024-06-17")

	bundle := metadata.NewBundle()
	bundle.Set(metadata.KeyTitle("Song"))
	bundle.Set(metadata.KeyArtist("Singer"))
	bundle.Set(metadata.KeyAlbum("Album"))
	bundle.PushBack(metadata.KeyArtist("Band"))
	bundle.Set(metadata.KeyDate(date))
	// bundle.Set(metadata.KeyTrackNumber("3/12"))
	// bundle.Set(metadata.KeyDiscNumber("1/2"))
	bundle.Set(metadata.KeyGenre("Rock"))
	bundle.Set(metadata.KeyComment("Comment"))
	bundle.Set(metadata.KeyComposer("Composer"))
	bundle.Set(metadata.KeyLyrics("Lyrics"))
	bundle.Set(metadata.KeyEncoder("LAME"))
	bundle.Set(metadata.KeyCopyright("(c)2024"))
	bundle.Set(metadata.KeyWebsite("https://example.com"))
	// bundle.Set(metadata.KeyArtistURL("https://artist.example"))
	// bundle.Set(metadata.metadataourceURL("https://source.example"))
	// bundle.Set(metadata.KeyAttachedPic, metadata.AttachedPicture{
	// 	MIMEType:    "image/png",
	// 	PictureType: 0x03,
	// 	Description: "cover",
	// 	Data:        []byte{0x89, 'P', 'N', 'G'},
	// })

	encoded, err := Marshal(*bundle, MarshalOptions{Version: Version2v3})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if len(encoded) == 0 {
		t.Fatalf("Marshal returned empty tag")
	}

	parsed, err := Parse(encoded)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	metadata.AssertBundleValue(t, parsed, metadata.KeyTitle("Song"))
	metadata.AssertBundleValue(t, parsed, metadata.KeyArtist("Singer"))
	metadata.AssertBundleValue(t, parsed, metadata.KeyAlbum("Album"))
	metadata.AssertBundleSlice(t, parsed, []metadata.KeyArtist{"Band"})
	metadata.AssertBundleValue(t, parsed, metadata.KeyComposer("Composer"))
	metadata.AssertBundleValue(t, parsed, metadata.KeyDate(date))
	// metadata.AssertBundleValue(t, parsed, metadata.KeyTrackNumber("3/12"))
	// metadata.AssertBundleValue(t, parsed, metadata.KeyDiscNumber("1/2"))
	metadata.AssertBundleValue(t, parsed, metadata.KeyGenre("Rock"))
	metadata.AssertBundleValue(t, parsed, metadata.KeyComment("Comment"))
	metadata.AssertBundleValue(t, parsed, metadata.KeyLyrics("Lyrics"))
	metadata.AssertBundleValue(t, parsed, metadata.KeyEncoder("LAME"))
	metadata.AssertBundleValue(t, parsed, metadata.KeyCopyright("(c)2024"))
	metadata.AssertBundleValue(t, parsed, metadata.KeyWebsite("https://example.com"))
	// metadata.AssertBundleValue(t, parsed, metadata.KeyArtistURL("https://artist.example"))
	// metadata.AssertBundleValue(t, parsed, metadata.metadataourceURL("https://source.example"))
}

func TestMarshalV2_RoundTrip_KeyThumbnails(t *testing.T) {
	picture := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	bundle := metadata.NewBundle()
	bundle.Set(metadata.KeyThumbnail{{
		Data:        picture,
		MIMEType:    "image/png",
		PictureType: metadata.PictureTypeFrontCover,
		Description: "cover",
	}})

	encoded, err := Marshal(*bundle, MarshalOptions{Version: Version2v3})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if len(encoded) == 0 {
		t.Fatalf("Marshal returned empty tag")
	}

	parsed, err := Parse(encoded)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	thumbnails := metadata.Get[metadata.KeyThumbnail](parsed)
	if len(thumbnails) != 1 {
		t.Fatalf("len(KeyThumbnails) = %d, want 1", len(thumbnails))
	}
	if thumbnails[0].MIMEType != "image/png" {
		t.Fatalf("KeyThumbnails[0].MIMEType = %q, want image/png", thumbnails[0].MIMEType)
	}
	if thumbnails[0].PictureType != metadata.PictureTypeFrontCover {
		t.Fatalf("KeyThumbnails[0].PictureType = %d, want front cover", thumbnails[0].PictureType)
	}
	if thumbnails[0].Description != "cover" {
		t.Fatalf("KeyThumbnails[0].Description = %q, want cover", thumbnails[0].Description)
	}
	if !bytes.Equal(thumbnails[0].Data, picture) {
		t.Fatalf("KeyThumbnails[0].Data = %v, want %v", thumbnails[0].Data, picture)
	}
}

func makeTagHeader(version byte, payloadSize int) []byte {
	header := []byte{'I', 'D', '3', version, 0x00, 0x00}
	header = append(header, encodeSyncSafeInt(payloadSize)...)
	return header
}

func TestMarshal_RoundTrip_Versions(t *testing.T) {
	versions := []Version{Version2v2, Version2v3, Version2v4}
	for _, v := range versions {
		t.Run(fmt.Sprintf("Version2%d", v), func(t *testing.T) {
			dateVal, _ := date.NewPartial("2024-06-17")

			bundle := metadata.NewBundle()
			bundle.Set(metadata.KeyTitle("Song"))
			bundle.Set(metadata.KeyArtist("Singer"))
			bundle.Set(metadata.KeyAlbum("Album"))
			bundle.PushBack(metadata.KeyArtist("Band"))
			bundle.Set(metadata.KeyDate(dateVal))
			bundle.Set(metadata.KeyGenre("Rock"))
			bundle.Set(metadata.KeyComment("Comment"))
			bundle.Set(metadata.KeyComposer("Composer"))
			bundle.Set(metadata.KeyLyrics("Lyrics"))
			bundle.Set(metadata.KeyEncoder("LAME"))
			bundle.Set(metadata.KeyCopyright("(c)2024"))
			bundle.Set(metadata.KeyWebsite("https://example.com"))

			// Encode
			encoded, err := Marshal(*bundle, MarshalOptions{Version: v})
			if err != nil {
				t.Fatalf("Marshal returned error: %v", err)
			}
			if len(encoded) == 0 {
				t.Fatalf("Marshal returned empty tag")
			}

			// Validate tag major version
			if encoded[3] != byte(v) {
				t.Fatalf("Encoded tag has version %d, want %d", encoded[3], v)
			}

			// Decode
			parsed, err := Parse(encoded)
			if err != nil {
				t.Fatalf("Parse returned error: %v", err)
			}

			metadata.AssertBundleValue(t, parsed, metadata.KeyTitle("Song"))
			metadata.AssertBundleValue(t, parsed, metadata.KeyArtist("Singer"))
			metadata.AssertBundleValue(t, parsed, metadata.KeyAlbum("Album"))
			metadata.AssertBundleSlice(t, parsed, []metadata.KeyArtist{"Band"})
			metadata.AssertBundleValue(t, parsed, metadata.KeyComposer("Composer"))
			parsedDate := date.Partial(metadata.Get[metadata.KeyDate](parsed))
			if !parsedDate.Year().Exists() || parsedDate.Year().Unwrap() != 2024 {
				t.Errorf("expected year 2024, got %v", parsedDate.Year())
			}
			metadata.AssertBundleValue(t, parsed, metadata.KeyGenre("Rock"))
			metadata.AssertBundleValue(t, parsed, metadata.KeyComment("Comment"))
			metadata.AssertBundleValue(t, parsed, metadata.KeyLyrics("Lyrics"))
			metadata.AssertBundleValue(t, parsed, metadata.KeyEncoder("LAME"))
			metadata.AssertBundleValue(t, parsed, metadata.KeyCopyright("(c)2024"))
			metadata.AssertBundleValue(t, parsed, metadata.KeyWebsite("https://example.com"))
		})
	}
}

func TestMarshal_RoundTrip_EncodingWarning(t *testing.T) {
	bundle := metadata.NewBundle()
	bundle.Set(metadata.KeyTitle("Song"))

	encoded, err := Marshal(*bundle, MarshalOptions{
		Version:  Version2v3,
		Encoding: EncodingUTF8,
	})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	parsed, err := Parse(encoded)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	metadata.AssertBundleValue(t, parsed, metadata.KeyTitle("Song"))
}
