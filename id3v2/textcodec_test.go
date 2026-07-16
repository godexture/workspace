package id3v2

import (
	"bytes"
	"testing"
)

func TestRemoveUnsynchronisation(t *testing.T) {
	t.Parallel()
	input := []byte{0xFF, 0x00, 0x12, 0xFF, 0x00, 0x00}
	expected := []byte{0xFF, 0x12, 0xFF, 0x00}
	result := removeUnsynchronisation(input)
	if !bytes.Equal(result, expected) {
		t.Errorf("removeUnsynchronisation(%v) = %v; want %v", input, result, expected)
	}
}

func TestUTF16Endian(t *testing.T) {
	t.Parallel()
	be := utf16BigEndian([]byte{0x12, 0x34})
	if be != 0x1234 {
		t.Errorf("utf16BigEndian failed: %x", be)
	}
	le := utf16LittleEndian([]byte{0x34, 0x12})
	if le != 0x1234 {
		t.Errorf("utf16LittleEndian failed: %x", le)
	}
}

func TestEncodeDecodeText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		encoding Encoding
		version  Version
	}{
		{"ASCII", "hello", EncodingISO88591, Version3},
		{"UTF-8", "hello UTF-8", EncodingUTF8, Version4},
		{"UTF-16", "hello UTF-16", EncodingUTF16, Version3},
		{"UTF-16BE", "hello UTF-16BE", EncodingUTF16BE, Version4},
		{"Default_v3", "hello default", EncodingDefault, Version3},
		{"Default_v4", "hello default", EncodingDefault, Version4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
	t.Parallel()
			encByte, payload := encodeText(tt.input, tt.encoding, tt.version)
			decoded := decodeEncodedText(encByte, payload)
			if decoded != tt.input {
				t.Errorf("encode/decode failed: want %q, got %q", tt.input, decoded)
			}
		})
	}
}
