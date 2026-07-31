package id3

import (
	"bytes"
	"testing"

	"github.com/godexture/godec/core/domain/metadata"
	"github.com/godexture/godec/plugins/metadata-id3/id3v1"
	"github.com/godexture/godec/plugins/metadata-id3/id3v2"
	"github.com/godexture/godec/sdk/date"
)

func TestParse_ImportantFrames(t *testing.T) {
	t.Parallel()
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
	metadata.AssertBundleSlice(t, bundle, []metadata.KeyArtist{"Artist", "BandX"})
	metadata.AssertBundleValue(t, bundle, metadata.KeyAlbum("Album"))
	metadata.AssertBundleValue(t, bundle, metadata.KeyDate(date))
	metadata.AssertBundleValue(t, bundle, metadata.KeyComposer("Composer"))
	metadata.AssertBundleValue(t, bundle, metadata.KeyLyrics("Lyrics go!"))
	metadata.AssertBundleValue(t, bundle, metadata.KeyEncoder("LAME3.1"))
	metadata.AssertBundleValue(t, bundle, metadata.KeyCopyright("(c)2024"))
	metadata.AssertBundleValue(t, bundle, metadata.KeyWebsite("https://example.com"))

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
}

func TestParse_LegacyDateFrames_WithTime(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	woarFrame := append([]byte("WOAR"), []byte{0x00, 0x00, 0x00, 0x16, 0x00, 0x00, 'h', 't', 't', 'p', 's', ':', '/', '/', 'a', 'r', 't', 'i', 's', 't', '.', 'e', 'x', 'a', 'm', 'p', 'l', 'e'}...)
	woasFrame := append([]byte("WOAS"), []byte{0x00, 0x00, 0x00, 0x15, 0x00, 0x00, 'h', 't', 't', 'p', 's', ':', '/', '/', 'a', 'l', 'b', 'u', 'm', '.', 'e', 'x', 'a', 'm', 'p', 'l', 'e'}...)

	t.Run("WOAR", func(t *testing.T) {
		t.Parallel()
		tagHeader := makeTagHeader(0x03, len(woarFrame))
		bundle, err := Parse(append(tagHeader, woarFrame...))
		if err != nil {
			t.Fatalf("Parse returned error: %v", err)
		}
		metadata.AssertBundleSlice(t, bundle, []metadata.KeyWebsite{"https://artist.example"})
	})

	t.Run("WOAS", func(t *testing.T) {
		t.Parallel()
		tagHeader := makeTagHeader(0x03, len(woasFrame))
		bundle, err := Parse(append(tagHeader, woasFrame...))
		if err != nil {
			t.Fatalf("Parse returned error: %v", err)
		}
		metadata.AssertBundleSlice(t, bundle, []metadata.KeyWebsite{"https://album.example"})
	})
}

func TestParse_ID3v1Fallback(t *testing.T) {
	t.Parallel()
	audio := []byte{0xFF, 0xFB, 0x90, 0x00}
	tag := make([]byte, id3v1.TagSize)
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
	metadata.AssertBundleSlice(t, bundle, []metadata.KeyArtist{"Singer"})
	metadata.AssertBundleValue(t, bundle, metadata.KeyAlbum("Album"))
	parsedDate := date.Partial(metadata.Get[metadata.KeyDate](bundle))
	if !parsedDate.Year().Exists() || parsedDate.Year().Unwrap() != 1999 {
		t.Errorf("expected year 1999, got %v", parsedDate.Year())
	}
	metadata.AssertBundleValue(t, bundle, metadata.KeyGenre("Rock"))
}

func makeTagHeader(version byte, payloadSize int) []byte {
	header := []byte{'I', 'D', '3', version, 0x00, 0x00}
	header = append(header, id3v2.EncodeSyncSafeInt(payloadSize)...)
	return header
}
