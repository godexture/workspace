package wave

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"sort"

	"github.com/godexture/godec/media/metadata"
)

type muxChunks struct {
	reservation  []byte
	beforeFormat []byte
	beforeData   []byte
	afterData    []byte
	trailer      []byte
}

type positionedMuxChunk struct {
	position uint64
	anchor   chunkAnchor
	payload  []byte
}

func marshalMuxChunks(ctx context.Context, resolver metadata.Resolver, document metadata.Document) (muxChunks, error) {
	blocks := document.Blocks()
	if !document.Scope().Valid() {
		if document.Len() == 0 && len(blocks) == 0 {
			return muxChunks{}, nil
		}
		return muxChunks{}, fmt.Errorf("%w: WAVE metadata document has no valid scope", ErrMalformed)
	}
	positioned := make([]positionedMuxChunk, 0, len(blocks))
	positions := make(map[uint64]struct{}, len(blocks))
	for _, block := range blocks {
		placement, ok := parseChunkBlockID(block.ID())
		if !ok {
			continue
		}
		if _, exists := positions[placement.position]; exists {
			return muxChunks{}, fmt.Errorf("%w: WAVE metadata chunk position %d is repeated", ErrMalformed, placement.position)
		}
		positions[placement.position] = struct{}{}

		var payload []byte
		switch placement.kind {
		case chunkRaw:
			if block.Carrier() != rawChunkCarrier() || !block.Encoding().IsZero() {
				return muxChunks{}, fmt.Errorf("%w: raw WAVE chunk %s has incompatible provenance", ErrMalformed, block.ID())
			}
			payload = block.Payload().AppendTo(nil)
		case chunkInfo:
			if block.Carrier() != RIFFInfo() {
				return muxChunks{}, fmt.Errorf("%w: RIFF INFO chunk %s has incompatible carrier", ErrMalformed, block.ID())
			}
			value, err := resolver.Marshal(ctx, RIFFInfo(), block.ID(), document)
			if err != nil {
				return muxChunks{}, err
			}
			payload = value.AppendTo(nil)
		default:
			return muxChunks{}, fmt.Errorf("%w: WAVE metadata chunk %s has an unsupported kind", ErrUnsupported, block.ID())
		}
		// The trailing region is raw bytes past the RIFF chunk, so it carries
		// no chunk header to validate.
		if placement.anchor == chunkAfterRIFF {
			positioned = append(positioned, positionedMuxChunk{position: placement.position, anchor: placement.anchor, payload: payload})
			continue
		}
		identity, err := validateMuxChunk(payload)
		if err != nil {
			return muxChunks{}, fmt.Errorf("%s: %w", block.ID(), err)
		}
		if placement.kind == chunkInfo {
			if identity != tagLIST {
				return muxChunks{}, fmt.Errorf("%w: RIFF INFO encoding returned %q instead of LIST", ErrMalformed, identity)
			}
			if _, err := infoPayload(payload); err != nil {
				return muxChunks{}, err
			}
		}
		positioned = append(positioned, positionedMuxChunk{position: placement.position, anchor: placement.anchor, payload: payload})
	}
	sort.Slice(positioned, func(left, right int) bool { return positioned[left].position < positioned[right].position })

	var result muxChunks
	for _, chunk := range positioned {
		var err error
		switch chunk.anchor {
		case chunkReservation:
			if result.reservation != nil {
				err = fmt.Errorf("%w: WAVE reservation slot is claimed twice", ErrMalformed)
				break
			}
			result.reservation, err = reservationChunk(chunk.payload)
		case chunkBeforeFormat:
			result.beforeFormat, err = appendMuxChunk(result.beforeFormat, chunk.payload)
		case chunkBeforeData:
			result.beforeData, err = appendMuxChunk(result.beforeData, chunk.payload)
		case chunkAfterData:
			result.afterData, err = appendMuxChunk(result.afterData, chunk.payload)
		case chunkAfterRIFF:
			result.trailer, err = appendMuxChunk(result.trailer, chunk.payload)
		default:
			err = fmt.Errorf("%w: WAVE metadata chunk has an invalid anchor", ErrMalformed)
		}
		if err != nil {
			return muxChunks{}, err
		}
	}
	return result, nil
}

func validateMuxChunk(value []byte) (string, error) {
	if len(value) < 8 {
		return "", fmt.Errorf("%w: preserved WAVE chunk is shorter than its header", ErrMalformed)
	}
	identity := string(value[0:4])
	if identity == tagFMT || identity == tagDATA || identity == tagDS64 {
		return "", fmt.Errorf("%w: reserved WAVE chunk %q cannot be restored as metadata", ErrUnsupported, identity)
	}
	size := uint64(binary.LittleEndian.Uint32(value[4:8]))
	total := uint64(8) + size + size&1
	if total != uint64(len(value)) {
		return "", fmt.Errorf("%w: preserved WAVE chunk %q has inconsistent size", ErrMalformed, identity)
	}
	return identity, nil
}

// reservationChunk accepts only a chunk that fits the slot it is written back
// into: the writer's header layout is fixed before the data size is known, so
// the slot cannot grow or shrink for preserved content.
func reservationChunk(payload []byte) ([]byte, error) {
	if len(payload) != 8+ds64PayloadSize || string(payload[0:4]) != tagJUNK {
		return nil, fmt.Errorf("%w: WAVE reservation chunk must be a %d byte JUNK chunk", ErrMalformed, 8+ds64PayloadSize)
	}
	return append([]byte(nil), payload...), nil
}

func appendMuxChunk(destination, payload []byte) ([]byte, error) {
	if len(payload) > math.MaxInt-len(destination) {
		return nil, fmt.Errorf("%w: WAVE metadata chunks exceed runtime address space", ErrUnsupported)
	}
	return append(destination, payload...), nil
}
