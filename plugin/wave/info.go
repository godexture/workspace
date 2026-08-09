package wave

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/tag"
)

const (
	tagLIST = "LIST"
	tagINFO = "INFO"
)

type infoMapping struct {
	native  string
	key     key.ID
	parse   func(*metadata.Builder, string, metadata.Origin) error
	marshal func(any) (string, error)
}

var infoMappings = []infoMapping{
	stringInfoMapping("INAM", tag.Title()),
	stringInfoMapping("IART", tag.Artist()),
	stringInfoMapping("IPRD", tag.Album()),
	dateInfoMapping(),
	stringInfoMapping("ICMT", tag.Comment()),
	stringInfoMapping("IGNR", tag.Genre()),
	stringInfoMapping("ISFT", tag.Encoder()),
	stringInfoMapping("ICOP", tag.Copyright()),
}

func stringInfoMapping(native string, declaration key.Key[string]) infoMapping {
	return infoMapping{
		native: native,
		key:    declaration.ID(),
		parse: func(builder *metadata.Builder, value string, origin metadata.Origin) error {
			metadata.Add(builder, declaration, value, origin)
			return nil
		},
		marshal: func(value any) (string, error) {
			text, ok := value.(string)
			if !ok {
				return "", fmt.Errorf("RIFF INFO %s value has type %T", native, value)
			}
			return text, nil
		},
	}
}

func dateInfoMapping() infoMapping {
	return infoMapping{
		native: "ICRD",
		key:    tag.Date().ID(),
		parse: func(builder *metadata.Builder, value string, origin metadata.Origin) error {
			date, err := tag.ParseDate(value)
			if err != nil {
				return err
			}
			metadata.Add(builder, tag.Date(), date, origin)
			return nil
		},
		marshal: func(value any) (string, error) {
			date, ok := value.(tag.PartialDate)
			if !ok || !date.HasValue() {
				return "", fmt.Errorf("RIFF INFO ICRD value has type %T or no date", value)
			}
			return date.ToISOString(), nil
		},
	}
}

func parseInfo(ctx metadata.ParseContext) (metadata.Document, error) {
	value := ctx.Payload().AppendTo(nil)
	payload, err := infoPayload(value)
	if err != nil {
		return metadata.Document{}, err
	}
	builder := metadata.NewBuilder(ctx.Scope())
	builder.AddBlock(metadata.NewRawBlock(ctx.Block(), ctx.Carrier(), ctx.Encoding(), ctx.Payload()))
	for offset := 4; offset < len(payload); {
		if len(payload)-offset < 8 {
			return metadata.Document{}, fmt.Errorf("%w: LIST/INFO subchunk header at %d is truncated", ErrMalformed, offset)
		}
		native := string(payload[offset : offset+4])
		size := uint64(binary.LittleEndian.Uint32(payload[offset+4 : offset+8]))
		end, ok := infoChunkEnd(uint64(offset), size, uint64(len(payload)))
		if !ok {
			return metadata.Document{}, fmt.Errorf("%w: LIST/INFO subchunk %q at %d exceeds its carrier", ErrMalformed, native, offset)
		}
		payloadEnd := uint64(offset+8) + size
		raw := payload[offset:int(end)]
		text := string(bytes.TrimRight(payload[offset+8:int(payloadEnd)], "\x00"))
		mapping, known := infoMappingForNative(native)
		origin := metadata.Origin{Encoding: ctx.Encoding(), Carrier: ctx.Carrier(), Block: ctx.Block(), Native: native}
		if !known || mapping.parse(builder, text, origin) != nil {
			block := metadata.BlockID(fmt.Sprintf("%s/field/%08d", ctx.Block(), offset))
			builder.AddBlock(metadata.NewRawBlock(block, ctx.Carrier(), ctx.Encoding(), metadata.NewBlob("application/octet-stream", raw)))
		}
		offset = int(end)
	}
	return builder.Build()
}

func marshalInfo(ctx metadata.MarshalContext) (metadata.Blob, error) {
	if original, ok := ctx.Document().Block(ctx.Block()); ok {
		if original.Carrier() != ctx.Carrier() || original.Encoding() != ctx.Encoding() {
			return metadata.Blob{}, fmt.Errorf("%w: RIFF INFO block %s belongs to another encoding", ErrMalformed, ctx.Block())
		}
		if _, err := infoPayload(original.Payload().AppendTo(nil)); err != nil {
			return metadata.Blob{}, err
		}
		return original.Payload(), nil
	}

	payload := []byte(tagINFO)
	for _, entry := range ctx.Document().Entries() {
		mapping, ok := infoMappingForKey(entry.Key())
		if !ok {
			return metadata.Blob{}, fmt.Errorf("%w: metadata key %s has no RIFF INFO representation", ErrUnsupported, entry.Key())
		}
		text, err := mapping.marshal(entry.Value())
		if err != nil {
			return metadata.Blob{}, err
		}
		subchunk, err := marshalInfoChunk(mapping.native, append([]byte(text), 0))
		if err != nil {
			return metadata.Blob{}, err
		}
		payload = append(payload, subchunk...)
	}
	chunk, err := marshalInfoChunk(tagLIST, payload)
	if err != nil {
		return metadata.Blob{}, err
	}
	return metadata.NewBlob("application/x-riff-info", chunk), nil
}

func infoPayload(value []byte) ([]byte, error) {
	if len(value) < 12 || string(value[0:4]) != tagLIST {
		return nil, fmt.Errorf("%w: RIFF INFO carrier is not a LIST chunk", ErrMalformed)
	}
	size := uint64(binary.LittleEndian.Uint32(value[4:8]))
	if size < 4 {
		return nil, fmt.Errorf("%w: LIST/INFO payload is shorter than its type", ErrMalformed)
	}
	total := uint64(8) + size + size&1
	if total != uint64(len(value)) {
		return nil, fmt.Errorf("%w: LIST/INFO size is %d but carrier has %d bytes", ErrMalformed, size, len(value)-8)
	}
	payload := value[8 : 8+int(size)]
	if string(payload[0:4]) != tagINFO {
		return nil, fmt.Errorf("%w: LIST carrier is not INFO", ErrMalformed)
	}
	return payload, nil
}

func infoChunkEnd(offset, size, limit uint64) (uint64, bool) {
	if offset > limit || limit-offset < 8 || size > math.MaxUint64-offset-8 {
		return 0, false
	}
	end := offset + 8 + size
	if size&1 != 0 {
		if end == math.MaxUint64 {
			return 0, false
		}
		end++
	}
	return end, end <= limit
}

func marshalInfoChunk(native string, payload []byte) ([]byte, error) {
	if len(native) != 4 || uint64(len(payload)) > math.MaxUint32 {
		return nil, fmt.Errorf("%w: RIFF INFO chunk identity or size is unsupported", ErrUnsupported)
	}
	result := make([]byte, 8+len(payload)+(len(payload)&1))
	copy(result[0:4], native)
	binary.LittleEndian.PutUint32(result[4:8], uint32(len(payload)))
	copy(result[8:], payload)
	return result, nil
}

func infoMappingForNative(native string) (infoMapping, bool) {
	for _, mapping := range infoMappings {
		if mapping.native == native {
			return mapping, true
		}
	}
	return infoMapping{}, false
}

func infoMappingForKey(identity key.ID) (infoMapping, bool) {
	for _, mapping := range infoMappings {
		if mapping.key == identity {
			return mapping, true
		}
	}
	return infoMapping{}, false
}
