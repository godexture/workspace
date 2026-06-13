package internal_test

import (
	"bytes"
	"testing"

	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/format-mp3/internal"
)

func TestProbe_ValidMP3(t *testing.T) {
	// 0xFF 0xFB = MPEG1 Layer3 の有効な同期ワード
	data := []byte{0xFF, 0xFB, 0x90, 0x00}
	score := internal.Probe(bytes.NewReader(data))
	if score < manifest.ProbeSingleSync {
		t.Errorf("expected ProbeSingleSync or higher, got %d", score)
	}
}

func TestProbe_ID3Header(t *testing.T) {
	data := []byte{'I', 'D', '3', 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	score := internal.Probe(bytes.NewReader(data))
	if score < manifest.ProbeSharedMetadata {
		t.Errorf("expected ProbeSharedMetadata or higher, got %d", score)
	}
}

func TestProbe_NotMP3(t *testing.T) {
	data := []byte{'R', 'I', 'F', 'F', 0x00, 0x00, 0x00, 0x00}
	score := internal.Probe(bytes.NewReader(data))
	if score != manifest.ProbeMismatch {
		t.Errorf("expected ProbeMismatch, got %d", score)
	}
}
