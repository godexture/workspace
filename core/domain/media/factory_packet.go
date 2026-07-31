package media

import (
	"github.com/godexture/core/domain/time"
	"github.com/godexture/sdk/pool"
)

// New is assigned in init rather than the var declaration: see the
// analogous comment on audioFramePool in factory_frame_audio.go for why
// (free refers to packetPool, so inlining here would create an
// initialization cycle).
var packetPool pool.Typed[*Packet]

func init() {
	packetPool.Init(func() *Packet {
		packet := &Packet{}
		packet.freeFunc = packet.free
		return packet
	})
}

type PacketOption func(*Packet)

func WithStreamIndex(idx int) PacketOption {
	return func(p *Packet) {
		p.StreamIndex = idx
	}
}

func WithPts(pts Pts) PacketOption {
	return func(p *Packet) {
		p.PTS = pts
	}
}

func WithDts(dts Dts) PacketOption {
	return func(p *Packet) {
		p.DTS = dts
	}
}

func NewPacket(size int, opts ...PacketOption) *Packet {
	b := pool.Get(size)
	(*b) = (*b)[:size]
	return newPacket(b, opts...)
}

// NewPacketFromData transfers ownership of data to the returned packet.
// The caller must not modify or retain data after this call.
func NewPacketFromData(data []byte, opts ...PacketOption) *Packet {
	return newPacket(&data, opts...)
}

func NewPacketEvent(kind PacketKind, streamIndex int, parameters []CodecParameters) *Packet {
	pkt := NewPacketFromData(nil, WithStreamIndex(streamIndex))
	pkt.Kind = kind
	pkt.CodecParameters = parameters
	return pkt
}

func newPacket(data *[]byte, opts ...PacketOption) *Packet {
	pkt := packetPool.Get()
	pkt.reset(data)
	pkt.refCount.Store(1)

	for _, opt := range opts {
		opt(pkt)
	}

	return pkt
}

func (p *Packet) free() {
	pool.Put(p.data)
	p.reset(nil)
	packetPool.Put(p)
}

func (p *Packet) reset(data *[]byte) {
	p.data = data
	p.MediaType = ""
	p.StreamIndex = 0
	p.Kind = PacketKindData
	p.CodecParameters = nil
	p.PTS = 0
	p.DTS = 0
	p.Timebase = time.Rational{}
}
