package engine

import (
	"time"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/domain/metadata"
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
	ReceiveFrame() (media.Frame, error)
	Flush() error
}

// FilterEngine is the only interface a filter engine must implement.
// Exactly one run-phase input port and one output port need nothing more
// than this; declaring additional ports (see FilterInput, WithInputs,
// WithOutputs) requires the engine to also implement AuxInputEngine and/or
// MultiOutputEngine below. Those are optional, type-asserted capabilities,
// not part of this base contract, so a plain single-port engine never
// needs to know about them.
type FilterEngine interface {
	SendFrame(frame *media.Frame) error
	ReceiveFrame() (media.Frame, error)
	Flush() error
}

// AuxInputEngine is an optional FilterEngine capability, detected via type
// assertion, needed only when a filter declares more than one run-phase
// input port and/or any preload input port. SendInput receives a frame for
// a named port; EndInput marks that port's EOF. The adapter retains and
// releases frames around both calls.
type AuxInputEngine interface {
	SendInput(port string, frame *media.Frame) error
	EndInput(port string) error
}

// MultiOutputEngine is an optional FilterEngine capability, detected via
// type assertion, needed only when a filter declares more than one output
// port. ReceiveOutput behaves like ReceiveFrame but also names which
// output port the frame belongs to.
type MultiOutputEngine interface {
	ReceiveOutput() (port string, frame media.Frame, err error)
}
