package media

import (
	"time"

	"github.com/godexture/core/domain/metadata"
)

type StreamInfo struct {
	Index int
	Type  MediaType

	Metadata metadata.Bundle

	IsDefault bool
	Duration  time.Duration

	MediaAttributes
}
