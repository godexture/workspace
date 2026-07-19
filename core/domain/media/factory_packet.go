package media

import "github.com/godexture/sdk/pool"

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
	pkt := &Packet{
		data: data,
	}
	pkt.refCount.Store(1)

	for _, opt := range opts {
		opt(pkt)
	}

	pkt.Init(func() {
		pool.Put(pkt.data)
		pkt.data = nil
	})

	return pkt
}
