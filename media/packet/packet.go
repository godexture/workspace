// Package packet separates container chunks from codec packets.
package packet

import (
	"context"

	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/side"
	"github.com/godexture/godec/media/timing"
)

// Chunk is the unit emitted by a container format. A Chunk never represents
// end-of-stream; edge closure carries that lifecycle event.
type Chunk struct {
	sequence uint64
	pts      timing.OptionalPTS
	payload  buffer.Handle
	sideData side.Data
}

// NewChunk builds a container chunk. A chunk whose source states no time
// passes timing.UnknownPTS, which stays distinct from a PTS of zero.
func NewChunk(sequence uint64, pts timing.OptionalPTS, payload buffer.Handle) Chunk {
	return Chunk{sequence: sequence, pts: pts, payload: payload}
}

func (c Chunk) Valid() bool             { return c.payload.Valid() }
func (c Chunk) Sequence() uint64        { return c.sequence }
func (c Chunk) PTS() timing.OptionalPTS { return c.pts }
func (c Chunk) SideData() side.Data     { return c.sideData }

// WithSideData returns a copy carrying immutable side data.
func (c Chunk) WithSideData(value side.Data) Chunk { c.sideData = value; return c }

// Payload returns a borrowed view valid until the chunk owner is released.
// Call View.Share when the payload must outlive this chunk.
func (c Chunk) Payload() buffer.View { return c.payload.Borrow() }
func (c Chunk) Bytes() []byte        { return c.payload.Bytes() }
func (c Chunk) Share() Chunk         { c.payload = c.payload.Share(); return c }
func (c Chunk) Release()             { c.payload.Release() }

// Detach hands the owned payload to the caller and empties the chunk, so
// Release afterwards does nothing. It rewraps a payload for another item type
// without retaining it; Share is for the case where both must stay alive.
func (c *Chunk) Detach() buffer.Handle {
	payload := c.payload
	c.payload = buffer.Handle{}
	return payload
}

// Packet is the unit requested by a codec parser/decoder. Its timestamp types
// are distinct and optional; a zero value is a valid timestamp when present.
type Packet struct {
	sequence uint64
	pts      timing.OptionalPTS
	dts      timing.OptionalDTS
	duration timing.OptionalDuration
	payload  buffer.Handle
	sideData side.Data
}

func NewPacket(sequence uint64, pts timing.OptionalPTS, dts timing.OptionalDTS, duration timing.OptionalDuration, payload buffer.Handle) Packet {
	return Packet{sequence: sequence, pts: pts, dts: dts, duration: duration, payload: payload}
}

func (p Packet) Valid() bool                       { return p.payload.Valid() }
func (p Packet) Sequence() uint64                  { return p.sequence }
func (p Packet) PTS() timing.OptionalPTS           { return p.pts }
func (p Packet) DTS() timing.OptionalDTS           { return p.dts }
func (p Packet) Duration() timing.OptionalDuration { return p.duration }
func (p Packet) SideData() side.Data               { return p.sideData }

// WithSideData returns a copy carrying immutable side data.
func (p Packet) WithSideData(value side.Data) Packet { p.sideData = value; return p }

// Payload returns a borrowed view valid until the packet owner is released.
// Call View.Share when the payload must outlive this packet.
func (p Packet) Payload() buffer.View { return p.payload.Borrow() }
func (p Packet) Bytes() []byte        { return p.payload.Bytes() }
func (p Packet) Share() Packet {
	p.payload = p.payload.Share()
	return p
}
func (p Packet) Release() { p.payload.Release() }

// Detach hands the owned payload to the caller and empties the packet, so
// Release afterwards does nothing. It rewraps a payload for another item type
// without retaining it; Share is for the case where both must stay alive.
func (p *Packet) Detach() buffer.Handle {
	payload := p.payload
	p.payload = buffer.Handle{}
	return payload
}

// Filter is a packet-to-packet transformation independent of a decoder or
// encoder. It may return the input unchanged when no transformation is needed.
type Filter interface {
	Process(context.Context, Packet) (Packet, error)
}

type FilterFunc func(context.Context, Packet) (Packet, error)

func (f FilterFunc) Process(ctx context.Context, value Packet) (Packet, error) {
	return f(ctx, value)
}
