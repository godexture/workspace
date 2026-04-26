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

	pkt := &Packet{
		data: b,
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
