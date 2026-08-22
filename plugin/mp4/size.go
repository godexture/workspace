package mp4

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/godexture/godec/access"
)

type sizeCursor struct {
	fixed   uint32
	compact bool
	bits    byte
	entries rawTableReader
	half    bool
	pending byte
}

func newSizeCursor(ctx context.Context, reader access.Random, value box, compact bool) (sizeCursor, error) {
	var prefix [12]byte
	if err := readBoxPrefix(ctx, reader, value, prefix[:], "sample sizes"); err != nil {
		return sizeCursor{}, err
	}
	if !compact {
		fixed := binary.BigEndian.Uint32(prefix[4:8])
		if fixed != 0 {
			return sizeCursor{fixed: fixed}, nil
		}
		entries := rawTableReader{reader: reader, offset: value.payloadOffset + 12, remaining: binary.BigEndian.Uint32(prefix[8:]), entrySize: 4, what: "stsz"}
		return sizeCursor{entries: entries}, nil
	}
	bits := prefix[7]
	count := binary.BigEndian.Uint32(prefix[8:])
	bytes, ok := compactSizeBytes(uint64(count), bits)
	if !ok || bytes > math.MaxUint32 {
		return sizeCursor{}, fmt.Errorf("%w: stz2 entries", errMalformedMovie)
	}
	entrySize := uint64(1)
	if bits == 16 {
		entrySize = 2
	}
	return sizeCursor{compact: true, bits: bits, entries: rawTableReader{reader: reader, offset: value.payloadOffset + 12, remaining: uint32(bytes / entrySize), entrySize: entrySize, what: "stz2"}}, nil
}

func (c *sizeCursor) next(ctx context.Context) (uint32, error) {
	if c.fixed != 0 {
		return c.fixed, nil
	}
	if c.compact && c.bits == 4 && c.half {
		c.half = false
		return uint32(c.pending & 0x0f), nil
	}
	entry, err := c.entries.next(ctx)
	if err != nil {
		return 0, err
	}
	if !c.compact {
		return binary.BigEndian.Uint32(entry[:4]), nil
	}
	switch c.bits {
	case 4:
		c.pending = entry[0]
		c.half = true
		return uint32(entry[0] >> 4), nil
	case 8:
		return uint32(entry[0]), nil
	case 16:
		return uint32(binary.BigEndian.Uint16(entry[:2])), nil
	default:
		return 0, fmt.Errorf("%w: stz2 field size %d", errUnsupportedMovie, c.bits)
	}
}
