package media

import "github.com/godexture/core/domain/metadata"

type StreamInfo struct {
	Index int
	Type  MediaType

	Metadata metadata.Bundle

	IsDefault bool

	MediaAttributes
}
