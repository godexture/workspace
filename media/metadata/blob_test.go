package metadata

import (
	"io"
	"testing"
)

func TestBlobCopiesOnceAndSharesAfterwards(t *testing.T) {
	source := []byte{1, 2, 3, 4}
	blob := NewBlob("image/png", source)
	source[0] = 9
	if got := blob.AppendTo(nil); got[0] != 1 {
		t.Fatalf("blob tracked its source slice: %v", got)
	}

	// Copying the value must not copy the payload: every document, entry, and
	// raw block that carries this artwork shares one backing array.
	copied := blob
	if copied.state != blob.state {
		t.Fatal("copying a blob duplicated its backing")
	}
	if copied.Len() != 4 || copied.MediaType() != "image/png" {
		t.Fatalf("copied blob = %d bytes, %q", copied.Len(), copied.MediaType())
	}
}

func TestBlobReadsWithoutHandingOutAMutableSlice(t *testing.T) {
	blob := NewBlob("", []byte{7, 8})
	streamed, err := io.ReadAll(blob.Reader())
	if err != nil {
		t.Fatal(err)
	}
	if len(streamed) != 2 || streamed[0] != 7 {
		t.Fatalf("streamed = %v", streamed)
	}
	appended := blob.AppendTo([]byte{0})
	appended[1] = 99
	if again := blob.AppendTo(nil); again[0] != 7 {
		t.Fatalf("AppendTo exposed the backing array: %v", again)
	}
}

func TestZeroBlobIsUsableAndEmpty(t *testing.T) {
	var blob Blob
	if blob.Valid() || blob.Len() != 0 || blob.MediaType() != "" || blob.AppendTo(nil) != nil {
		t.Fatalf("zero blob = %#v", blob)
	}
	if data, err := io.ReadAll(blob.Reader()); err != nil || len(data) != 0 {
		t.Fatalf("zero blob reader = %v, %v", data, err)
	}
}

func TestBlobEqualityComparesContentAcrossSources(t *testing.T) {
	left := NewBlob("image/png", []byte{1, 2})
	right := NewBlob("image/png", []byte{1, 2})
	if !left.Equal(right) || !left.Equal(left) {
		t.Fatal("equal payloads compared unequal")
	}
	if left.Equal(NewBlob("image/jpeg", []byte{1, 2})) {
		t.Fatal("different media types compared equal")
	}
	if left.Equal(Blob{}) || (Blob{}).Equal(left) {
		t.Fatal("absent payload compared equal to a present one")
	}
	if !(Blob{}).Equal(Blob{}) {
		t.Fatal("two absent payloads compared unequal")
	}
}

func TestBlobSliceSharesImmutableBackingWithBoundsAndMediaType(t *testing.T) {
	parent := NewBlob("application/octet-stream", []byte{1, 2, 3, 4})
	child, ok := parent.Slice("image/jpeg", 1, 3)
	if !ok || child.MediaType() != "image/jpeg" || child.Len() != 2 || string(child.AppendTo(nil)) != string([]byte{2, 3}) {
		t.Fatalf("slice = %#v/%v", child, ok)
	}
	if &child.state.data[0] != &parent.state.data[1] {
		t.Fatal("slice copied immutable backing")
	}
	copy := child.AppendTo(nil)
	copy[0] = 9
	if got := parent.AppendTo(nil); got[1] != 2 {
		t.Fatalf("slice exposed parent backing: %v", got)
	}
	empty, ok := parent.Slice("", 2, 2)
	if !ok || !empty.Valid() || empty.Len() != 0 {
		t.Fatalf("empty slice = %#v/%v", empty, ok)
	}
	for _, bounds := range [][2]int{{-1, 0}, {0, 5}, {3, 2}} {
		if _, ok := parent.Slice("", bounds[0], bounds[1]); ok {
			t.Fatalf("Slice(%d, %d) succeeded", bounds[0], bounds[1])
		}
	}
	if _, ok := (Blob{}).Slice("", 0, 0); ok {
		t.Fatal("zero blob sliced successfully")
	}
}
