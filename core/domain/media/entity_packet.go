package media

import (
	"github.com/godexture/core/domain/time"
)

type PacketKind uint8

const (
	PacketKindData PacketKind = iota
	PacketKindStreamEnd
)

type Packet struct {
	ResourceBase
	data *[]byte

	MediaType       MediaType
	StreamIndex     int
	Kind            PacketKind
	CodecParameters []CodecParameters

	PTS      Pts
	DTS      Dts
	Timebase time.Rational
}

func (p *Packet) Data() []byte {
	if p == nil || p.data == nil {
		return nil
	}
	return *p.data
}
