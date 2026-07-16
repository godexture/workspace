package id3v2_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/godexture/core/domain/metadata"
	id3 "github.com/godexture/metadata-id3"
	"github.com/godexture/metadata-id3/id3v2"
	"github.com/godexture/sdk/date"
)

func TestMarshal_RoundTrip_ImportantMetadata(t *testing.T) {
	t.Parallel()
	dateVal, _ := date.NewPartial("2024-06-17")

	bundle := metadata.NewBundle()
	bundle.Set(metadata.KeyTitle("Song"))
	bundle.PushBack(metadata.KeyArtist("Singer"))
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

	encoded, err := id3v2.Marshal(*bundle, id3v2.MarshalOptions{Version: id3v2.Version3})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if len(encoded) == 0 {
		t.Fatalf("Marshal returned empty tag")
	}

	parsed, err := id3.Parse(encoded)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	metadata.AssertBundleValue(t, parsed, metadata.KeyTitle("Song"))
	metadata.AssertBundleSlice(t, parsed, []metadata.KeyArtist{"Singer", "Band"})
	metadata.AssertBundleValue(t, parsed, metadata.KeyAlbum("Album"))
	metadata.AssertBundleValue(t, parsed, metadata.KeyComposer("Composer"))
	metadata.AssertBundleValue(t, parsed, metadata.KeyDate(dateVal))
	metadata.AssertBundleValue(t, parsed, metadata.KeyGenre("Rock"))
	metadata.AssertBundleValue(t, parsed, metadata.KeyComment("Comment"))
	metadata.AssertBundleValue(t, parsed, metadata.KeyLyrics("Lyrics"))
	metadata.AssertBundleValue(t, parsed, metadata.KeyEncoder("LAME"))
	metadata.AssertBundleValue(t, parsed, metadata.KeyCopyright("(c)2024"))
	metadata.AssertBundleValue(t, parsed, metadata.KeyWebsite("https://example.com"))
}

func TestMarshalV2_RoundTrip_KeyThumbnails(t *testing.T) {
	t.Parallel()
	picture := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	bundle := metadata.NewBundle()
	bundle.Set(metadata.KeyThumbnail{{
		Data:        picture,
		MIMEType:    "image/png",
		PictureType: metadata.PictureTypeFrontCover,
		Description: "cover",
	}})

	encoded, err := id3v2.Marshal(*bundle, id3v2.MarshalOptions{Version: id3v2.Version3})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if len(encoded) == 0 {
		t.Fatalf("Marshal returned empty tag")
	}

	parsed, err := id3.Parse(encoded)
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

func TestMarshal_RoundTrip_Versions(t *testing.T) {
	t.Parallel()
	versions := []id3v2.Version{id3v2.Version2, id3v2.Version3, id3v2.Version4}
	for _, v := range versions {
		t.Run(fmt.Sprintf("Version%d", v), func(t *testing.T) {
	t.Parallel()
			dateVal, _ := date.NewPartial("2024-06-17")

			bundle := metadata.NewBundle()
			bundle.Set(metadata.KeyTitle("Song"))
			bundle.PushBack(metadata.KeyArtist("Singer"))
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
			encoded, err := id3v2.Marshal(*bundle, id3v2.MarshalOptions{Version: v})
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
			parsed, err := id3.Parse(encoded)
			if err != nil {
				t.Fatalf("Parse returned error: %v", err)
			}

			metadata.AssertBundleValue(t, parsed, metadata.KeyTitle("Song"))
			metadata.AssertBundleSlice(t, parsed, []metadata.KeyArtist{"Singer", "Band"})
			metadata.AssertBundleValue(t, parsed, metadata.KeyAlbum("Album"))
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
	t.Parallel()
	bundle := metadata.NewBundle()
	bundle.Set(metadata.KeyTitle("Song"))

	encoded, err := id3v2.Marshal(*bundle, id3v2.MarshalOptions{
		Version:  id3v2.Version3,
		Encoding: id3v2.EncodingUTF8,
	})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	parsed, err := id3.Parse(encoded)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	metadata.AssertBundleValue(t, parsed, metadata.KeyTitle("Song"))
}
