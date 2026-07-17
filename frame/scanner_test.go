package frame

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/godexture/format-flac/streaminfo"
)

const appendixDStreamInfo = "1000100000000f00000f0ac442f0000000013e84b41807dc690307586a3dad1a2e0f"
const appendixDFrame = "fff869180000bf0358fd03128baa9a"

func TestScannerSyncSkipsLeadingGarbage(t *testing.T) {
	info, err := streaminfo.Parse(decodeHex(t, appendixDStreamInfo))
	if err != nil {
		t.Fatal(err)
	}
	want := decodeHex(t, appendixDFrame)
	scanner, err := NewScanner(bytes.NewReader(append([]byte{1, 2, 3}, want...)), info, Options{Sync: true})
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := scanner.Next()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) || scanner.FrameOffset() != 3 {
		t.Fatalf("frame = %x, offset = %d", got, scanner.FrameOffset())
	}
}

func TestScannerStrictRejectsLeadingGarbage(t *testing.T) {
	info, err := streaminfo.Parse(decodeHex(t, appendixDStreamInfo))
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := NewScanner(bytes.NewReader(append([]byte{1}, decodeHex(t, appendixDFrame)...)), info, Options{Strict: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := scanner.Next(); err == nil {
		t.Fatal("expected strict scanner to reject leading garbage")
	}
}

func decodeHex(t testing.TB, value string) []byte {
	t.Helper()
	data, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
