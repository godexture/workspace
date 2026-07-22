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

// Clone returns an independent copy that shares no state with s.
func (s StreamInfo) Clone() StreamInfo {
	s.Metadata = *s.Metadata.Clone()
	s.CodecParameters = s.CodecParameters.Clone()
	return s
}

// CloneStreams returns an independent copy of streams, deep-cloning each element.
func CloneStreams(streams []StreamInfo) []StreamInfo {
	cloned := make([]StreamInfo, len(streams))
	for i := range streams {
		cloned[i] = streams[i].Clone()
	}
	return cloned
}
