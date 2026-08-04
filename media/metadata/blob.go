package metadata

import "bytes"

// Blob is an immutable byte payload such as artwork or an uninterpreted block.
//
// Copying a Blob shares the backing array, so duplicating an entry or a whole
// document never copies the payload. The payload is copied once on the way in
// and never handed out as a mutable slice, which is what lets the sharing be
// safe without a reference count.
type Blob struct{ state *blobState }

type blobState struct {
	mediaType string
	data      []byte
}

// NewBlob copies data into an immutable payload. This is the only copy: every
// later Blob value shares this backing.
func NewBlob(mediaType string, data []byte) Blob {
	return Blob{state: &blobState{mediaType: mediaType, data: append([]byte(nil), data...)}}
}

func (b Blob) Valid() bool { return b.state != nil }

// MediaType is an advisory content type such as "image/png". It is empty when
// the producing encoding did not state one.
func (b Blob) MediaType() string {
	if b.state == nil {
		return ""
	}
	return b.state.mediaType
}

func (b Blob) Len() int {
	if b.state == nil {
		return 0
	}
	return len(b.state.data)
}

// AppendTo copies the payload only when a caller actually needs the bytes.
func (b Blob) AppendTo(destination []byte) []byte {
	if b.state == nil {
		return destination
	}
	return append(destination, b.state.data...)
}

// Reader streams the payload without copying it.
func (b Blob) Reader() *bytes.Reader {
	if b.state == nil {
		return bytes.NewReader(nil)
	}
	return bytes.NewReader(b.state.data)
}

// Equal reports payload equality. Blobs from different sources compare by
// content, so a re-encoded document can be checked against its original.
func (b Blob) Equal(other Blob) bool {
	if b.state == nil || other.state == nil {
		return b.state == other.state
	}
	if b.state == other.state {
		return true
	}
	return b.state.mediaType == other.state.mediaType && bytes.Equal(b.state.data, other.state.data)
}
