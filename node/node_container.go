package node

import (
	"time"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
)

type Muxer interface {
	Node

	AddStream(info media.StreamInfo) (streamIndex int, err error)
	SetMetadata(meta *metadata.Bundle) error

	InputPorts() map[string]*InPort[*media.Packet]
}

type Demuxer interface {
	Node

	Metadata() *metadata.Bundle
	Streams() ([]media.StreamInfo, error)

	OutputPorts() map[string]*OutPort[*media.Packet]
}

type Seeker interface {
	Seek(offset time.Duration) error
}
