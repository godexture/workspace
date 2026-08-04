package tag

import "github.com/godexture/godec/media/metadata"

// ArtworkType identifies the role an artwork payload plays in a carrier. The
// values follow the numbering shared by ID3 APIC and FLAC PICTURE.
type ArtworkType uint8

const (
	ArtworkOther      ArtworkType = 0
	ArtworkFileIcon   ArtworkType = 1
	ArtworkFrontCover ArtworkType = 3
	ArtworkBackCover  ArtworkType = 4
)

// Artwork is the value carried by the Picture key. Data is an immutable Blob,
// so copying an entry or a document does not duplicate its byte payload.
//
// The dimension fields exist because the carriers state them in the bitstream;
// nothing here decodes an image to fill them.
type Artwork struct {
	Data          metadata.Blob
	MediaType     string
	Type          ArtworkType
	Description   string
	Width         uint32
	Height        uint32
	ColorDepth    uint32
	IndexedColors uint32
}

// Valid reports whether the artwork carries a payload.
func (a Artwork) Valid() bool { return a.Data.Valid() }

// EffectiveMediaType returns the explicit value when present and otherwise the
// type carried by the immutable Blob.
func (a Artwork) EffectiveMediaType() string {
	if a.MediaType != "" {
		return a.MediaType
	}
	return a.Data.MediaType()
}
