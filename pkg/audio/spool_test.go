package audio

import (
	"slices"
	"testing"

	"github.com/godexture/core/domain/media"
)

func TestSpoolNextMemoryDoesNotAliasStoredSamples(t *testing.T) {
	t.Parallel()
	spool := NewSpool(1<<20, "")
	if err := spool.Append(Block{Channels: [][]float32{{1, 2}}, Layout: media.LayoutMono1, Rate: 48000}); err != nil {
		t.Fatal(err)
	}
	if err := spool.Rewind(); err != nil {
		t.Fatal(err)
	}
	first, ok, err := spool.Next()
	if err != nil || !ok {
		t.Fatalf("Next() = (%v, %t, %v)", first, ok, err)
	}
	first.Channels[0][0] = 99
	if err := spool.Rewind(); err != nil {
		t.Fatal(err)
	}
	second, ok, err := spool.Next()
	if err != nil || !ok {
		t.Fatalf("Next() = (%v, %t, %v)", second, ok, err)
	}
	if got, want := second.Channels[0][0], float32(1); got != want {
		t.Fatalf("stored sample = %v, want %v", got, want)
	}
}

func TestSpoolNextDiskReadsStoredSamples(t *testing.T) {
	t.Parallel()
	spool := NewSpool(0, "")
	defer spool.Close()
	if err := spool.Append(Block{Channels: [][]float32{{1, 2}}, Layout: media.LayoutMono1, Rate: 48000}); err != nil {
		t.Fatal(err)
	}
	if err := spool.Rewind(); err != nil {
		t.Fatal(err)
	}
	block, ok, err := spool.Next()
	if err != nil || !ok {
		t.Fatalf("Next() = (%v, %t, %v)", block, ok, err)
	}
	if got, want := block.Channels[0], []float32{1, 2}; !slices.Equal(got, want) {
		t.Fatalf("samples = %v, want %v", got, want)
	}
}
