package engine

import (
	"time"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
)

type MuxerEngine interface {
	AddStream(info media.StreamInfo) (streamIndex int, err error)
	SetMetadata(meta metadata.Bundle) error

	WriteHeader() error
	WriteTrailer() error

	WritePacket(streamIndex int, pkt *media.Packet) error
}

type DemuxerEngine interface {
	Analyze() (streams []media.StreamInfo, globalMeta metadata.Bundle, err error)
	ReadPacket() (pkt *media.Packet, streamIndex int, err error)
}

type SeekerEngine interface {
	Seek(offset time.Duration) error
}

type EncoderEngine interface {
	SendFrame(frame *media.Frame) error
	ReceivePacket() (*media.Packet, error)
	Flush() error
}

type DecoderEngine interface {
	SendPacket(pkt *media.Packet) error
	ReceiveFrame() (*media.Frame, error)
	Flush() error
}

type FilterEngine interface {
	SendFrame(frame *media.Frame) error
	ReceiveFrame() (*media.Frame, error)
	Flush() error
}
