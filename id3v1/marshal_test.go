package id3v1

import (
	"bytes"
	"testing"

	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/sdk/date"
)

func TestMarshal(t *testing.T) {
	bundle := metadata.NewBundle()

	// 完全に空の場合
	tag, err := Marshal(*bundle)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tag != nil {
		t.Errorf("expected nil tag for empty bundle, got %x", tag)
	}

	// データを入れる
	bundle.Set(metadata.KeyTitle("Test Title"))
	bundle.PushBack(metadata.KeyArtist("Test Artist"))
	bundle.Set(metadata.KeyAlbum("Test Album"))
	d, _ := date.NewPartial("2023")
	bundle.Set(metadata.KeyDate(d))
	bundle.Set(metadata.KeyComment("Test Comment"))
	bundle.Set(metadata.KeyTrackNumber(1))
	bundle.Set(metadata.KeyGenre("Rock"))

	tag, err = Marshal(*bundle)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(tag) != TagSize {
		t.Fatalf("expected tag size %d, got %d", TagSize, len(tag))
	}

	if !bytes.Equal(tag[0:3], []byte(tagHeader)) {
		t.Errorf("expected TAG header, got %q", tag[0:3])
	}

	title := string(bytes.TrimRight(tag[3:33], "\x00"))
	if title != "Test Title" {
		t.Errorf("expected title 'Test Title', got '%s'", title)
	}

	artist := string(bytes.TrimRight(tag[33:63], "\x00"))
	if artist != "Test Artist" {
		t.Errorf("expected artist 'Test Artist', got '%s'", artist)
	}

	album := string(bytes.TrimRight(tag[63:93], "\x00"))
	if album != "Test Album" {
		t.Errorf("expected album 'Test Album', got '%s'", album)
	}

	yearStr := string(bytes.TrimRight(tag[93:97], "\x00"))
	if yearStr != "2023" {
		t.Errorf("expected year '2023', got '%s'", yearStr)
	}

	comment := string(bytes.TrimRight(tag[97:125], "\x00"))
	if comment != "Test Comment" {
		t.Errorf("expected comment 'Test Comment', got '%s'", comment)
	}

	if tag[125] != 0 {
		t.Errorf("expected 0 for track number indicator, got %d", tag[125])
	}

	if tag[126] != 1 {
		t.Errorf("expected track number 1, got %d", tag[126])
	}

	// Rock is index 17
	if tag[127] != 17 {
		t.Errorf("expected genre index 17 (Rock), got %d", tag[127])
	}
}
