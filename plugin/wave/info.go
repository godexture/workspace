package wave

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"reflect"
	"strings"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/plugin"
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
		carrier, err := parseInfoCarrier(original.Payload().AppendTo(nil), ctx.Block())
		if err != nil {
			return metadata.Blob{}, err
		}
		owned, additions := infoEntriesForBlock(ctx.Document(), ctx.Block(), ctx.Carrier(), ctx.Encoding())
		childrenChanged, err := applyInfoChildren(&carrier, ctx.Document(), ctx.Block(), ctx.Carrier(), ctx.Encoding())
		if err != nil {
			return metadata.Blob{}, err
		}
		if infoEntriesMatch(carrier.semantic, owned) && !childrenChanged {
			if len(additions) == 0 && len(owned) == len(carrier.semantic) {
				return original.Payload(), nil
			}
			value, err := appendInfoEntries(carrier.raw, append(append([]metadata.Entry(nil), owned[len(carrier.semantic):]...), additions...))
			if err != nil {
				return metadata.Blob{}, err
			}
			return metadata.NewBlob("application/x-riff-info", value), nil
		}
		value, err := reencodeInfoCarrier(carrier, append(append([]metadata.Entry(nil), owned...), additions...))
		if err != nil {
			return metadata.Blob{}, err
		}
		return metadata.NewBlob("application/x-riff-info", value), nil
	}

	chunk, err := marshalFreshInfoEntries(ctx.Document().Entries())
	if err != nil {
		return metadata.Blob{}, err
	}
	return metadata.NewBlob("application/x-riff-info", chunk), nil
}

// infoSubchunk is the parsed projection of one carrier subchunk. Raw keeps
// the original header, payload, and padding so an unchanged document can use
// the exact source bytes and an edit can retain unknown records verbatim.
type infoSubchunk struct {
	raw      []byte
	native   string
	child    metadata.BlockID
	semantic bool
	key      key.ID
	value    any
}

type infoCarrier struct {
	raw       []byte
	subchunks []infoSubchunk
	semantic  []infoSubchunk
}

func parseInfoCarrier(value []byte, block metadata.BlockID) (infoCarrier, error) {
	payload, err := infoPayload(value)
	if err != nil {
		return infoCarrier{}, err
	}
	carrier := infoCarrier{raw: append([]byte(nil), value...)}
	for offset := 4; offset < len(payload); {
		if len(payload)-offset < 8 {
			return infoCarrier{}, fmt.Errorf("%w: LIST/INFO subchunk header at %d is truncated", ErrMalformed, offset)
		}
		native := string(payload[offset : offset+4])
		size := uint64(binary.LittleEndian.Uint32(payload[offset+4 : offset+8]))
		end, ok := infoChunkEnd(uint64(offset), size, uint64(len(payload)))
		if !ok {
			return infoCarrier{}, fmt.Errorf("%w: LIST/INFO subchunk %q at %d exceeds its carrier", ErrMalformed, native, offset)
		}
		payloadEnd := uint64(offset+8) + size
		raw := append([]byte(nil), payload[offset:int(end)]...)
		subchunk := infoSubchunk{raw: raw, native: native}
		mapping, known := infoMappingForNative(native)
		if known {
			text := string(bytes.TrimRight(payload[offset+8:int(payloadEnd)], "\x00"))
			if value, ok := parseInfoValue(mapping, text); ok {
				subchunk.semantic = true
				subchunk.key = mapping.key
				subchunk.value = value
				carrier.semantic = append(carrier.semantic, subchunk)
			}
		}
		if !subchunk.semantic {
			subchunk.child = metadata.BlockID(fmt.Sprintf("%s/field/%08d", block, offset))
		}
		carrier.subchunks = append(carrier.subchunks, subchunk)
		offset = int(end)
	}
	return carrier, nil
}

func applyInfoChildren(carrier *infoCarrier, document metadata.Document, block metadata.BlockID, slot carrier.ID, encoding plugin.Identity) (bool, error) {
	if carrier == nil {
		return false, nil
	}
	prefix := string(block) + "/field/"
	children := make(map[metadata.BlockID]metadata.RawBlock)
	for _, raw := range document.Blocks() {
		if !strings.HasPrefix(string(raw.ID()), prefix) {
			continue
		}
		if raw.Carrier() != slot || raw.Encoding() != encoding {
			return false, fmt.Errorf("%w: RIFF INFO child block %s has incompatible provenance", ErrMalformed, raw.ID())
		}
		children[raw.ID()] = raw
	}
	original := make(map[metadata.BlockID]struct{})
	changed := false
	for index := range carrier.subchunks {
		subchunk := &carrier.subchunks[index]
		if subchunk.semantic {
			continue
		}
		original[subchunk.child] = struct{}{}
		child, ok := children[subchunk.child]
		if !ok {
			subchunk.raw = nil
			changed = true
			continue
		}
		value := child.Payload().AppendTo(nil)
		if bytes.Equal(value, subchunk.raw) {
			continue
		}
		if _, err := validateInfoSubchunk(value); err != nil {
			return false, fmt.Errorf("%s: %w", child.ID(), err)
		}
		subchunk.raw = value
		changed = true
	}
	for _, child := range document.Blocks() {
		if !strings.HasPrefix(string(child.ID()), prefix) {
			continue
		}
		if _, exists := original[child.ID()]; exists {
			continue
		}
		value := child.Payload().AppendTo(nil)
		native, err := validateInfoSubchunk(value)
		if err != nil {
			return false, fmt.Errorf("%s: %w", child.ID(), err)
		}
		carrier.subchunks = append(carrier.subchunks, infoSubchunk{
			raw:    value,
			native: native,
			child:  child.ID(),
		})
		changed = true
	}
	return changed, nil
}

func validateInfoSubchunk(value []byte) (string, error) {
	if len(value) < 8 {
		return "", fmt.Errorf("%w: RIFF INFO child subchunk is shorter than its header", ErrMalformed)
	}
	native := string(value[:4])
	size := uint64(binary.LittleEndian.Uint32(value[4:8]))
	end, ok := infoChunkEnd(0, size, uint64(len(value)))
	if !ok || end != uint64(len(value)) {
		return "", fmt.Errorf("%w: RIFF INFO child subchunk %q has inconsistent size", ErrMalformed, native)
	}
	return native, nil
}

func parseInfoValue(mapping infoMapping, text string) (any, bool) {
	if mapping.native == "ICRD" {
		value, err := tag.ParseDate(text)
		return value, err == nil
	}
	return text, true
}

func infoEntriesForBlock(document metadata.Document, block metadata.BlockID, carrier carrier.ID, encoding plugin.Identity) (owned, additions []metadata.Entry) {
	firstBlock := metadata.BlockID("")
	for _, raw := range document.Blocks() {
		if raw.Carrier() == carrier && raw.Encoding() == encoding {
			firstBlock = raw.ID()
			break
		}
	}
	isAdditionTarget := firstBlock == "" || block == firstBlock
	for _, entry := range document.Entries() {
		origin := entry.Origin()
		switch origin.Block {
		case "":
			if isAdditionTarget {
				additions = append(additions, entry)
			}
		case block:
			owned = append(owned, entry)
		default:
			// Entries from another parsed INFO block remain attached to that
			// block. They must not be copied into this carrier.
			if raw, ok := document.Block(origin.Block); ok && raw.Carrier() == carrier && raw.Encoding() == encoding {
				continue
			}
		}
	}
	return owned, additions
}

func infoEntriesMatch(original []infoSubchunk, current []metadata.Entry) bool {
	if len(current) < len(original) {
		return false
	}
	for index, subchunk := range original {
		entry := current[index]
		if !subchunk.semantic || entry.Key() != subchunk.key || !reflect.DeepEqual(entry.Value(), subchunk.value) {
			return false
		}
	}
	return true
}

func appendInfoEntries(original []byte, entries []metadata.Entry) ([]byte, error) {
	payload, err := infoPayload(original)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return append([]byte(nil), original...), nil
	}
	resultPayload := append([]byte(nil), payload...)
	for _, entry := range entries {
		subchunk, err := marshalInfoEntry(entry)
		if err != nil {
			return nil, err
		}
		resultPayload = append(resultPayload, subchunk...)
	}
	return marshalInfoChunk(tagLIST, resultPayload)
}

func reencodeInfoCarrier(carrier infoCarrier, entries []metadata.Entry) ([]byte, error) {
	matched, entryMatch := matchInfoEntries(carrier.semantic, entries)
	payload := []byte(tagINFO)
	semanticIndex := 0
	for _, subchunk := range carrier.subchunks {
		if !subchunk.semantic {
			if len(subchunk.raw) == 0 {
				continue
			}
			payload = append(payload, subchunk.raw...)
			continue
		}
		entryIndex := matched[semanticIndex]
		semanticIndex++
		if entryIndex < 0 {
			continue
		}
		entry := entries[entryIndex]
		if entry.Key() == subchunk.key && reflect.DeepEqual(entry.Value(), subchunk.value) {
			payload = append(payload, subchunk.raw...)
			continue
		}
		encoded, err := marshalInfoEntry(entry)
		if err != nil {
			return nil, err
		}
		payload = append(payload, encoded...)
	}
	for entryIndex, entry := range entries {
		if entryMatch[entryIndex] >= 0 {
			continue
		}
		encoded, err := marshalInfoEntry(entry)
		if err != nil {
			return nil, err
		}
		payload = append(payload, encoded...)
	}
	return marshalInfoChunk(tagLIST, payload)
}

func matchInfoEntries(original []infoSubchunk, entries []metadata.Entry) ([]int, []int) {
	matched := make([]int, len(original))
	for index := range matched {
		matched[index] = -1
	}
	entryMatch := make([]int, len(entries))
	for index := range entryMatch {
		entryMatch[index] = -1
	}
	if len(entries) == len(original) {
		for index := range entries {
			matched[index] = index
			entryMatch[index] = index
		}
		return matched, entryMatch
	}
	if len(entries) > len(original) {
		return matchInfoSubsequence(original, entries, matched, entryMatch)
	}
	matched, entryMatch = matchInfoSubsequence(original, entries, matched, entryMatch)
	return assignInfoSubsequence(original, entries, matched, entryMatch)
}

func matchInfoSubsequence(original []infoSubchunk, entries []metadata.Entry, matched, entryMatch []int) ([]int, []int) {
	rows := len(entries) + 1
	columns := len(original) + 1
	dp := make([]int, rows*columns)
	for row := len(entries) - 1; row >= 0; row-- {
		for column := len(original) - 1; column >= 0; column-- {
			best := dp[(row+1)*columns+column]
			if value := dp[row*columns+column+1]; value > best {
				best = value
			}
			if entries[row].Origin().Block != "" && infoSemanticEqual(original[column], entries[row], true) {
				value := 1 + dp[(row+1)*columns+column+1]
				if value > best {
					best = value
				}
			}
			dp[row*columns+column] = best
		}
	}
	row, column := 0, 0
	for row < len(entries) && column < len(original) {
		if entries[row].Origin().Block != "" && infoSemanticEqual(original[column], entries[row], true) && dp[row*columns+column] == 1+dp[(row+1)*columns+column+1] {
			matched[column] = row
			entryMatch[row] = column
			row++
			column++
			continue
		}
		if dp[(row+1)*columns+column] >= dp[row*columns+column+1] {
			row++
		} else {
			column++
		}
	}
	return matched, entryMatch
}

func assignInfoSubsequence(original []infoSubchunk, entries []metadata.Entry, matched, entryMatch []int) ([]int, []int) {
	for entryIndex := range entries {
		if entryMatch[entryIndex] >= 0 {
			continue
		}
		previous := -1
		for index := entryIndex - 1; index >= 0; index-- {
			if entryMatch[index] >= 0 {
				previous = entryMatch[index]
				break
			}
		}
		next := len(original)
		for index := entryIndex + 1; index < len(entries); index++ {
			if entryMatch[index] >= 0 {
				next = entryMatch[index]
				break
			}
		}
		for slot := previous + 1; slot < next; slot++ {
			if matched[slot] < 0 {
				matched[slot] = entryIndex
				entryMatch[entryIndex] = slot
				break
			}
		}
	}
	return matched, entryMatch
}

func infoSemanticEqual(subchunk infoSubchunk, entry metadata.Entry, exactValue bool) bool {
	if entry.Key() != subchunk.key {
		return false
	}
	origin := entry.Origin()
	if origin.Native != "" && origin.Native != subchunk.native {
		return false
	}
	return !exactValue || reflect.DeepEqual(entry.Value(), subchunk.value)
}

func marshalFreshInfoEntries(entries []metadata.Entry) ([]byte, error) {
	payload := []byte(tagINFO)
	for _, entry := range entries {
		subchunk, err := marshalInfoEntry(entry)
		if err != nil {
			return nil, err
		}
		payload = append(payload, subchunk...)
	}
	return marshalInfoChunk(tagLIST, payload)
}

func marshalInfoEntry(entry metadata.Entry) ([]byte, error) {
	mapping, ok := infoMappingForKey(entry.Key())
	if !ok {
		return nil, fmt.Errorf("%w: metadata key %s has no RIFF INFO representation", ErrUnsupported, entry.Key())
	}
	text, err := mapping.marshal(entry.Value())
	if err != nil {
		return nil, err
	}
	return marshalInfoChunk(mapping.native, append([]byte(text), 0))
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
