package id3v2

import (
	"bufio"
	"bytes"
	"errors"
	"testing"
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

func TestSkip(t *testing.T) {
	tag1 := append([]byte{'I', 'D', '3', 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03}, []byte("abc")...)
	tag2 := append([]byte{'I', 'D', '3', 0x04, 0x00, 0x10, 0x00, 0x00, 0x00, 0x02}, []byte("de1234567890")...)
	payload := append(append(tag1, tag2...), []byte{0xFF, 0xFB, 0x90, 0x00}...)

	br := bufio.NewReader(bytes.NewReader(payload))
	skipped, err := Skip(br)
	if err != nil {
		t.Fatalf("Skip returned error: %v", err)
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
