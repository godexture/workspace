package buffer

import (
	"slices"
	"testing"
)

func TestRingZeroValue(t *testing.T) {
	var ring Ring[byte]
	ring.Append([]byte{1, 2, 3})
	if !slices.Equal(ring.Data(), []byte{1, 2, 3}) {
		t.Fatalf("Data = %v", ring.Data())
	}
}

func TestRingTakeAndDiscard(t *testing.T) {
	ring := NewRing[int](8)
	ring.Append([]int{1, 2, 3, 4, 5})

	if got := ring.Take(2); !slices.Equal(got, []int{1, 2}) {
		t.Fatalf("Take = %v", got)
	}
	ring.Discard(1)
	if got := ring.Data(); !slices.Equal(got, []int{4, 5}) {
		t.Fatalf("Data = %v", got)
	}
}

func TestRingGrowAndTruncate(t *testing.T) {
	var ring Ring[float32]
	tail := ring.Grow(4)
	copy(tail, []float32{1, 2, 3, 4})
	ring.Truncate(3)

	if got := ring.Data(); !slices.Equal(got, []float32{1, 2, 3}) {
		t.Fatalf("Data = %v", got)
	}
}

func TestRingCompactsConsumedPrefix(t *testing.T) {
	ring := NewRing[int](8)
	ring.Append([]int{1, 2, 3, 4, 5, 6})
	ring.Discard(4)
	capacity := ring.Cap()
	ring.Append([]int{7, 8, 9, 10})

	if got := ring.Data(); !slices.Equal(got, []int{5, 6, 7, 8, 9, 10}) {
		t.Fatalf("Data = %v", got)
	}
	if ring.Cap() != capacity {
		t.Fatalf("capacity grew from %d to %d", capacity, ring.Cap())
	}
}

func TestRingGrowsWhenPrefixIsTooSmallToCompact(t *testing.T) {
	ring := NewRing[int](8)
	ring.Append([]int{1, 2, 3, 4, 5, 6, 7, 8})
	ring.Discard(2)
	ring.Append([]int{9, 10, 11, 12})

	if got := ring.Data(); !slices.Equal(got, []int{3, 4, 5, 6, 7, 8, 9, 10, 11, 12}) {
		t.Fatalf("Data = %v", got)
	}
	if ring.Cap() <= 8 {
		t.Fatalf("capacity = %d, want growth", ring.Cap())
	}
}

func TestRingResetRetainsCapacity(t *testing.T) {
	ring := NewRing[byte](16)
	ring.Append([]byte{1, 2, 3})
	ring.Reset()

	if ring.Len() != 0 || ring.Cap() != 16 {
		t.Fatalf("Len = %d, Cap = %d", ring.Len(), ring.Cap())
	}
}

func TestRingTakeAllReleasesStorage(t *testing.T) {
	ring := NewRing[byte](16)
	ring.Append([]byte{1, 2, 3, 4})
	ring.Discard(1)
	got := ring.TakeAll()
	ring.Append([]byte{9, 9, 9})

	if !slices.Equal(got, []byte{2, 3, 4}) {
		t.Fatalf("TakeAll = %v", got)
	}
	if ring.Cap() == 16 {
		t.Fatal("TakeAll retained the backing storage")
	}
}

func BenchmarkBufferAppendTake(b *testing.B) {
	data := make([]byte, 4096)
	ring := NewRing[byte](8192)

	b.ReportAllocs()
	for b.Loop() {
		ring.Append(data)
		n := ring.Len() / 1012 * 1012
		if got := ring.Take(n); len(got) == 0 || len(got)%1012 != 0 {
			b.Fatalf("Take returned %d bytes", len(got))
		}
	}
}
