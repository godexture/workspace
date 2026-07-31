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

// StreamSet holds one StreamInfo per named port, for nodes whose ports can
// carry different stream shapes (e.g. a mixer's independent input/output
// ports).
type StreamSet map[string]StreamInfo

// Clone returns an independent copy that shares no state with s.
func (s StreamSet) Clone() StreamSet {
	if s == nil {
		return nil
	}
	cloned := make(StreamSet, len(s))
	for port, stream := range s {
		cloned[port] = stream.Clone()
	}
	return cloned
}
