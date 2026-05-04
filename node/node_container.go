package node

import (
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/core/domain/time"
)

type Muxer interface {
	Node

	AddStream(codecName string, tb time.Rational) (streamIndex int, err error)
	SetMetadata(meta *metadata.Bundle) error

	InputPorts() map[string]*InPort[*media.Packet]
}

type Demuxer interface {
	Node

	Metadata() *metadata.Bundle

	OutputPorts() map[string]*OutPort[*media.Packet]
}
