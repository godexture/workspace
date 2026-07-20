package spool

import (
	"slices"
	"testing"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/filter-audio/internal/audio"
)

func TestBlocksNextMemoryDoesNotAliasStoredSamples(t *testing.T) {
	t.Parallel()
	blocks := New(1<<20, "")
	if err := blocks.Append(audio.Block{Channels: [][]float32{{1, 2}}, Layout: media.LayoutMono1, Rate: 48000}); err != nil {
		t.Fatal(err)
	}
	if err := blocks.Rewind(); err != nil {
		t.Fatal(err)
	}
	first, ok, err := blocks.Next()
	if err != nil || !ok {
		t.Fatalf("Next() = (%v, %t, %v)", first, ok, err)
	}
	first.Channels[0][0] = 99
	if err := blocks.Rewind(); err != nil {
		t.Fatal(err)
	}
	second, ok, err := blocks.Next()
	if err != nil || !ok {
		t.Fatalf("Next() = (%v, %t, %v)", second, ok, err)
	}
	if got, want := second.Channels[0][0], float32(1); got != want {
		t.Fatalf("stored sample = %v, want %v", got, want)
	}
}

func TestBlocksNextDiskReadsStoredSamples(t *testing.T) {
	t.Parallel()
	blocks := New(0, "")
	defer blocks.Close()
	if err := blocks.Append(audio.Block{Channels: [][]float32{{1, 2}}, Layout: media.LayoutMono1, Rate: 48000}); err != nil {
		t.Fatal(err)
	}
	if err := blocks.Rewind(); err != nil {
		t.Fatal(err)
	}
	block, ok, err := blocks.Next()
	if err != nil || !ok {
		t.Fatalf("Next() = (%v, %t, %v)", block, ok, err)
	}
	if got, want := block.Channels[0], []float32{1, 2}; !slices.Equal(got, want) {
		t.Fatalf("samples = %v, want %v", got, want)
	}
}
