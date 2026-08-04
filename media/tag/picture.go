package tag

import "github.com/godexture/godec/media/metadata"

// PictureType identifies the role of an artwork payload in a carrier.
type PictureType uint8

const (
	PictureTypeOther      PictureType = 0
	PictureTypeFileIcon   PictureType = 1
	PictureTypeFrontCover PictureType = 3
	PictureTypeBackCover  PictureType = 4
)

// Picture is the shared artwork value. Data is an immutable Blob, so copying
// an entry or document does not duplicate its byte payload.
type Picture struct {
	Data          metadata.Blob
	MediaType     string
	PictureType   PictureType
	Description   string
	Width         uint32
	Height        uint32
	ColorDepth    uint32
	IndexedColors uint32
}

// Valid reports whether the picture carries a payload.
func (p Picture) Valid() bool { return p.Data.Valid() }

// EffectiveMediaType returns the explicit value when present and otherwise
// the type carried by the immutable Blob.
func (p Picture) EffectiveMediaType() string {
	if p.MediaType != "" {
		return p.MediaType
	}
	return p.Data.MediaType()
}
