package id3v2

import (
	"bytes"
	"testing"
)

func TestDecodePICFrame(t *testing.T) {
	payload := []byte{
		0x00,             // encoding (Latin1)
		'P', 'N', 'G',    // image format
		0x03,             // picture type (Front Cover)
		'C', 'o', 'v', 'e', 'r', 0x00, // description
		0x89, 'P', 'N', 'G', // data
	}
	thumb := decodePICFrame(payload)
	if thumb.MIMEType != "image/png" {
		t.Errorf("MIMEType = %q; want image/png", thumb.MIMEType)
	}
	if thumb.PictureType != 3 {
		t.Errorf("PictureType = %d; want 3", thumb.PictureType)
	}
	if thumb.Description != "Cover" {
		t.Errorf("Description = %q; want Cover", thumb.Description)
	}
	if !bytes.Equal(thumb.Data, []byte{0x89, 'P', 'N', 'G'}) {
		t.Errorf("Data = %v; want PNG data", thumb.Data)
	}
}

func TestPicFormatToMIME(t *testing.T) {
	tests := []struct {
		format string
		mime   string
	}{
		{"PNG", "image/png"},
		{"JPG", "image/jpeg"},
		{"BMP", "BMP"},
		{"GIF", "GIF"},
		{"UNKNOWN", "UNKNOWN"},
	}

	for _, tt := range tests {
		actual := picFormatToMIME([]byte(tt.format))
		if actual != tt.mime {
			t.Errorf("picFormatToMIME(%q) = %q; want %q", tt.format, actual, tt.mime)
		}
	}
}

func TestMimeToFormat(t *testing.T) {
	tests := []struct {
		mime   string
		format string
	}{
		{"image/png", "PNG"},
		{"image/jpeg", "JPG"},
		{"image/bmp", "BMP"},
		{"image/gif", "GIF"},
		{"image/unknown", "UNK"},
	}

	for _, tt := range tests {
		actual := mimeToFormat(tt.mime)
		if actual != tt.format {
			t.Errorf("mimeToFormat(%q) = %q; want %q", tt.mime, actual, tt.format)
		}
	}
}
