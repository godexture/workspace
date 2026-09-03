package wave

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"reflect"
	"strings"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/plugin"
)

const (
	tagLIST = "LIST"
	tagINFO = "INFO"
	// LIST's 32-bit size field covers its payload; the extra byte is the
	// optional RIFF pad and the other eight bytes are the chunk header.
	maxInfoCarrierBytes = uint64(math.MaxUint32) + 9
)

type infoMapping struct {
	native      string
	key         key.ID
	declaration key.Erased
	parse       func(*metadata.Builder, string, metadata.Origin) error
	marshal     func(any) (string, error)
}

// infoRewriteLayout is the compact source-side index retained for the one
// INFO carrier that a range mux may rewrite. It records only accepted
// semantic children; Open scans the source plane directly, so invalid and
// unknown children remain opaque without being parsed or copied into a
// temporary model.
type infoRewriteLayout struct {
	present  bool
	semantic []infoSemanticChild
}

type infoSemanticChild struct {
	// offset is relative to the beginning of the LIST carrier.
	offset uint64
	length uint64
	key    key.ID
}

func (l infoRewriteLayout) valid() bool { return l.present }

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
		native:      native,
		key:         declaration.ID(),
		declaration: declaration.Erased(),
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
		native:      "ICRD",
		key:         tag.Date().ID(),
		declaration: tag.Date().Erased(),
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

func infoSupportedKeys() []key.Erased {
	result := make([]key.Erased, len(infoMappings))
	for index, mapping := range infoMappings {
		result[index] = mapping.declaration
	}
	return result
}

func parseInfo(ctx metadata.ParseContext) (metadata.Document, error) {
	value := ctx.Payload().AppendTo(nil)
	payload, err := infoPayload(value)
	if err != nil {
		return metadata.Document{}, err
	}
	builder := metadata.NewBuilder(ctx.Scope())
	builder.AddBlock(metadata.NewSourceBlock(ctx.Block(), ctx.Carrier(), ctx.Encoding(), ctx.Payload()))
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

func inspectInfoRewriteLayout(ctx context.Context, value []byte) (infoRewriteLayout, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := context.Cause(ctx); err != nil {
		return infoRewriteLayout{}, false, err
	}
	payload, err := infoPayload(value)
	if err != nil {
		return infoRewriteLayout{}, false, err
	}
	layout := infoRewriteLayout{present: true}
	complete := true
	for offset := 4; offset < len(payload); {
		if err := context.Cause(ctx); err != nil {
			return infoRewriteLayout{}, false, err
		}
		if len(payload)-offset < 8 {
			return infoRewriteLayout{}, false, fmt.Errorf("%w: LIST/INFO subchunk header at %d is truncated", ErrMalformed, offset)
		}
		native := string(payload[offset : offset+4])
		size := uint64(binary.LittleEndian.Uint32(payload[offset+4 : offset+8]))
		end, ok := infoChunkEnd(uint64(offset), size, uint64(len(payload)))
		if !ok {
			return infoRewriteLayout{}, false, fmt.Errorf("%w: LIST/INFO subchunk %q at %d exceeds its carrier", ErrMalformed, native, offset)
		}
		payloadEnd := uint64(offset+8) + size
		mapping, known := infoMappingForNative(native)
		if known {
			text := string(bytes.TrimRight(payload[offset+8:int(payloadEnd)], "\x00"))
			if _, ok := parseInfoValue(mapping, text); ok {
				layout.semantic = append(layout.semantic, infoSemanticChild{
					offset: uint64(offset + 8),
					length: end - uint64(offset),
					key:    mapping.key,
				})
			} else {
				complete = false
			}
		} else {
			complete = false
		}
		offset = int(end)
	}
	return layout, complete, nil
}

// infoEntryCursor walks only the expressible target entries without exposing
// the document's backing slice. Compile and Open retain the immutable target
// document, while this cursor keeps no per-entry snapshot.
type infoEntryCursor struct {
	document metadata.Document
	index    int
}

func (c *infoEntryCursor) next(ctx context.Context) (metadata.Entry, bool, error) {
	for c.index < c.document.Len() {
		if ctx != nil {
			if err := context.Cause(ctx); err != nil {
				return metadata.Entry{}, false, err
			}
		}
		entry, ok := c.document.EntryAt(c.index)
		c.index++
		if !ok {
			continue
		}
		if _, ok := infoMappingForKey(entry.Key()); ok {
			return entry, true, nil
		}
	}
	return metadata.Entry{}, false, nil
}

// validateInfoRewrite checks the immutable source and target documents against
// the compact source layout. No Entries snapshot or matching array is retained
// by the mux plan; Open obtains those facts from the documents while scanning
// the source once in wire order.
func validateInfoRewrite(original, document metadata.Document, layout infoRewriteLayout) error {
	if !original.Valid() || !document.Valid() || original.Scope() != document.Scope() || !layout.valid() {
		return fmt.Errorf("%w: RIFF INFO rewrite documents or source layout are invalid", ErrUnsupported)
	}
	if original.Len() != len(layout.semantic) {
		return fmt.Errorf("%w: RIFF INFO semantic source layout is inconsistent", ErrUnsupported)
	}
	for index, child := range layout.semantic {
		entry, ok := original.EntryAt(index)
		if !ok || child.key != entry.Key() {
			return fmt.Errorf("%w: RIFF INFO semantic source layout does not match its document", ErrUnsupported)
		}
	}
	return nil
}

// rewriteInfoSource applies a semantic edit to one source LIST/INFO carrier.
// The caller supplies the authoritative detached source document; the mux
// Open path uses the plan form below with allocator planes.
func rewriteInfoSource(value []byte, original, document metadata.Document) ([]byte, error) {
	layout, _, err := inspectInfoRewriteLayout(context.Background(), value)
	if err != nil {
		return nil, err
	}
	if err := validateInfoRewrite(original, document, layout); err != nil {
		return nil, err
	}
	_, output, err := infoRewriteWorkspaceAgainst(uint64(len(value)), original, document)
	if err != nil {
		return nil, err
	}
	if output > uint64(math.MaxInt) {
		return nil, fmt.Errorf("%w: RIFF INFO rewrite output exceeds runtime memory", ErrUnsupported)
	}
	return rewriteInfoSourceIntoPlan(context.Background(), value, original, layout, document, make([]byte, int(output)))
}

func rewriteInfoSourceIntoPlan(ctx context.Context, value []byte, original metadata.Document, layout infoRewriteLayout, target metadata.Document, destination []byte) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !original.Valid() || !target.Valid() || original.Scope() != target.Scope() || !layout.valid() || original.Len() != len(layout.semantic) {
		return nil, fmt.Errorf("%w: RIFF INFO rewrite documents or source layout are invalid", ErrUnsupported)
	}
	payload, err := infoPayload(value)
	if err != nil {
		return nil, err
	}
	childrenEnd := 8 + len(payload)
	if childrenEnd < 12 {
		return nil, fmt.Errorf("%w: RIFF INFO carrier is shorter than its type", ErrMalformed)
	}
	if len(destination) < 12 {
		return nil, fmt.Errorf("%w: RIFF INFO rewrite destination is too small", ErrUnsupported)
	}
	copy(destination[0:4], tagLIST)
	binary.LittleEndian.PutUint32(destination[4:8], 0)
	copy(destination[8:12], tagINFO)
	position := 12
	writeRaw := func(raw []byte) error {
		if len(raw) > len(destination)-position {
			return fmt.Errorf("%w: RIFF INFO rewrite destination is too small", ErrUnsupported)
		}
		copy(destination[position:], raw)
		position += len(raw)
		return nil
	}
	writeEntry := func(entry metadata.Entry) error {
		if position > len(destination) {
			return fmt.Errorf("%w: RIFF INFO rewrite destination is too small", ErrUnsupported)
		}
		written, writeErr := writeInfoEntryInto(destination[position:], entry)
		if writeErr != nil {
			return writeErr
		}
		position += written
		return nil
	}
	childBounds := func(offset int) (int, error) {
		if offset < 12 || offset > childrenEnd || childrenEnd-offset < 8 {
			return 0, fmt.Errorf("%w: RIFF INFO subchunk header at %d is truncated", ErrMalformed, offset)
		}
		size := uint64(binary.LittleEndian.Uint32(value[offset+4 : offset+8]))
		end, ok := infoChunkEnd(uint64(offset-8), size, uint64(childrenEnd-8))
		if !ok || end > uint64(childrenEnd-8) {
			return 0, fmt.Errorf("%w: RIFF INFO subchunk at %d exceeds its carrier", ErrMalformed, offset)
		}
		absolute := end + 8
		if absolute > uint64(childrenEnd) || absolute > uint64(math.MaxInt) {
			return 0, fmt.Errorf("%w: RIFF INFO subchunk exceeds runtime memory", ErrUnsupported)
		}
		return int(absolute), nil
	}
	childRaw := func(child infoSemanticChild) ([]byte, error) {
		if child.offset > uint64(len(value)) || child.length > uint64(len(value))-child.offset || child.offset > uint64(math.MaxInt) || child.length > uint64(math.MaxInt) {
			return nil, fmt.Errorf("%w: RIFF INFO source child exceeds runtime memory", ErrUnsupported)
		}
		start := int(child.offset)
		end := start + int(child.length)
		if end < start || end > len(value) {
			return nil, fmt.Errorf("%w: RIFF INFO source child is outside its carrier", ErrMalformed)
		}
		return value[start:end], nil
	}
	targetCursor := infoEntryCursor{document: target}
	semanticOrdinal := 0
	for offset := 12; offset < childrenEnd; {
		if err := context.Cause(ctx); err != nil {
			return nil, err
		}
		end, endErr := childBounds(offset)
		if endErr != nil {
			return nil, endErr
		}
		semantic := semanticOrdinal < len(layout.semantic) && layout.semantic[semanticOrdinal].offset == uint64(offset)
		if semanticOrdinal < len(layout.semantic) && layout.semantic[semanticOrdinal].offset < uint64(offset) {
			return nil, fmt.Errorf("%w: RIFF INFO semantic source layout is stale", ErrUnsupported)
		}
		if !semantic {
			if rawErr := writeRaw(value[offset:end]); rawErr != nil {
				return nil, rawErr
			}
			offset = end
			continue
		}
		child := layout.semantic[semanticOrdinal]
		semanticOrdinal++
		if child.length != uint64(end-offset) {
			return nil, fmt.Errorf("%w: RIFF INFO source semantic layout is stale", ErrUnsupported)
		}
		targetEntry, targetOK, targetErr := targetCursor.next(ctx)
		if targetErr != nil {
			return nil, targetErr
		}
		if targetOK {
			source, sourceOK := original.EntryAt(semanticOrdinal - 1)
			if sourceOK && source.Key() == targetEntry.Key() && reflect.DeepEqual(source.Value(), targetEntry.Value()) {
				raw, rawErr := childRaw(child)
				if rawErr != nil {
					return nil, rawErr
				}
				if rawErr = writeRaw(raw); rawErr != nil {
					return nil, rawErr
				}
			} else if entryErr := writeEntry(targetEntry); entryErr != nil {
				return nil, entryErr
			}
		}
		offset = end
	}
	for {
		target, ok, err := targetCursor.next(ctx)
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		if entryErr := writeEntry(target); entryErr != nil {
			return nil, entryErr
		}
	}
	if uint64(position-8) > math.MaxUint32 {
		return nil, fmt.Errorf("%w: RIFF INFO rewrite exceeds its size field", ErrUnsupported)
	}
	binary.LittleEndian.PutUint32(destination[4:8], uint32(position-8))
	if (position-8)&1 != 0 {
		if position >= len(destination) {
			return nil, fmt.Errorf("%w: RIFF INFO rewrite destination is too small", ErrUnsupported)
		}
		destination[position] = 0
		position++
	}
	return destination[:position], nil
}

func marshalInfo(ctx metadata.MarshalContext) (metadata.Blob, []loss.Loss, error) {
	if original, ok := ctx.Document().Block(ctx.Block()); ok {
		if original.Carrier() != ctx.Carrier() || original.Encoding() != ctx.Encoding() {
			return metadata.Blob{}, nil, fmt.Errorf("%w: RIFF INFO block %s belongs to another encoding", ErrMalformed, ctx.Block())
		}
		carrier, err := parseInfoCarrier(original.Payload().AppendTo(nil), ctx.Block())
		if err != nil {
			return metadata.Blob{}, nil, err
		}
		owned, additions := infoEntriesForBlock(ctx.Document(), ctx.Block(), ctx.Carrier(), ctx.Encoding())
		owned, ownedLost := expressibleInfoEntries(owned)
		additions, addedLost := expressibleInfoEntries(additions)
		lost := append(ownedLost, addedLost...)
		childrenChanged, err := applyInfoChildren(&carrier, ctx.Document(), ctx.Block(), ctx.Carrier(), ctx.Encoding())
		if err != nil {
			return metadata.Blob{}, nil, err
		}
		if !original.Source() && ctx.Document().Len() == 0 && !childrenChanged {
			return original.Payload(), lost, nil
		}
		if infoEntriesMatch(carrier.semantic, owned) && !childrenChanged {
			if len(additions) == 0 && len(owned) == len(carrier.semantic) {
				return original.Payload(), lost, nil
			}
			value, err := appendInfoEntries(carrier.raw, append(append([]metadata.Entry(nil), owned[len(carrier.semantic):]...), additions...))
			if err != nil {
				return metadata.Blob{}, nil, err
			}
			return metadata.NewBlob("application/x-riff-info", value), lost, nil
		}
		value, err := reencodeInfoCarrier(carrier, append(append([]metadata.Entry(nil), owned...), additions...))
		if err != nil {
			return metadata.Blob{}, nil, err
		}
		return metadata.NewBlob("application/x-riff-info", value), lost, nil
	}

	entries, lost := expressibleInfoEntries(ctx.Document().Entries())
	chunk, err := marshalFreshInfoEntries(entries)
	if err != nil {
		return metadata.Blob{}, nil, err
	}
	return metadata.NewBlob("application/x-riff-info", chunk), lost, nil
}

// expressibleInfoEntries splits what this carrier can say from what it cannot.
// A key with no INFO name is not a mistake by whoever asked for it -- it is a
// fact about RIFF -- so it is reported and the rest of the document is still
// written. A job that would rather lose nothing says so in its policy.
func expressibleInfoEntries(entries []metadata.Entry) ([]metadata.Entry, []loss.Loss) {
	var kept []metadata.Entry
	var lost []loss.Loss
	for _, entry := range entries {
		if _, ok := infoMappingForKey(entry.Key()); ok {
			kept = append(kept, entry)
			continue
		}
		lost = append(lost, loss.Loss{
			Key:    entry.Key(),
			Kind:   loss.Dropped,
			Detail: "wave.info-unrepresentable",
			Source: entry.Origin().LossOrigin(),
		})
	}
	return kept, lost
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
	carrier := infoCarrier{raw: value}
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
		raw := payload[offset:int(end)]
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
		if raw.Source() {
			return false, fmt.Errorf("%w: RIFF INFO child block %s must be opaque", ErrMalformed, raw.ID())
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
	payload := []byte(tagINFO)
	entryIndex := 0
	for _, subchunk := range carrier.subchunks {
		if !subchunk.semantic {
			if len(subchunk.raw) == 0 {
				continue
			}
			payload = append(payload, subchunk.raw...)
			continue
		}
		if entryIndex >= len(entries) {
			continue
		}
		entry := entries[entryIndex]
		entryIndex++
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
	for ; entryIndex < len(entries); entryIndex++ {
		entry := entries[entryIndex]
		encoded, err := marshalInfoEntry(entry)
		if err != nil {
			return nil, err
		}
		payload = append(payload, encoded...)
	}
	return marshalInfoChunk(tagLIST, payload)
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
	size, err := infoEntryWireSize(entry)
	if err != nil {
		return nil, err
	}
	result := make([]byte, size)
	if _, err := writeInfoEntryInto(result, entry); err != nil {
		return nil, err
	}
	return result, nil
}

func infoEntryText(entry metadata.Entry) (infoMapping, string, error) {
	mapping, ok := infoMappingForKey(entry.Key())
	if !ok {
		return infoMapping{}, "", fmt.Errorf("%w: metadata key %s has no RIFF INFO representation", ErrUnsupported, entry.Key())
	}
	value := entry.Value()
	if mapping.native != "ICRD" {
		text, ok := value.(string)
		if !ok {
			return infoMapping{}, "", fmt.Errorf("RIFF INFO %s value has type %T", mapping.native, value)
		}
		return mapping, text, nil
	}
	text, err := mapping.marshal(value)
	if err != nil {
		return infoMapping{}, "", err
	}
	return mapping, text, nil
}

func infoEntryWireSize(entry metadata.Entry) (int, error) {
	_, text, err := infoEntryText(entry)
	if err != nil {
		return 0, err
	}
	if uint64(len(text)) >= math.MaxUint32 {
		return 0, fmt.Errorf("%w: RIFF INFO entry text exceeds its size field", ErrUnsupported)
	}
	payload := uint64(len(text)) + 1
	total := uint64(8) + payload + payload&1
	if total > uint64(math.MaxInt) || total > maxInfoCarrierBytes {
		return 0, fmt.Errorf("%w: RIFF INFO entry exceeds runtime memory", ErrUnsupported)
	}
	return int(total), nil
}

// writeInfoEntryInto writes directly into a caller-owned fixed plane. It
// never converts string values to a second byte slice, which keeps Open's
// rewrite allocation independent of artwork/text payload length.
func writeInfoEntryInto(destination []byte, entry metadata.Entry) (int, error) {
	mapping, text, err := infoEntryText(entry)
	if err != nil {
		return 0, err
	}
	if uint64(len(text)) >= math.MaxUint32 {
		return 0, fmt.Errorf("%w: RIFF INFO entry text exceeds its size field", ErrUnsupported)
	}
	payload := uint64(len(text)) + 1
	total := uint64(8) + payload + payload&1
	if total > uint64(math.MaxInt) || total > maxInfoCarrierBytes || total > uint64(len(destination)) {
		return 0, fmt.Errorf("%w: RIFF INFO entry destination is too small", ErrUnsupported)
	}
	if len(mapping.native) != 4 {
		return 0, fmt.Errorf("%w: RIFF INFO entry identity is unsupported", ErrUnsupported)
	}
	copy(destination[0:4], mapping.native)
	binary.LittleEndian.PutUint32(destination[4:8], uint32(payload))
	copy(destination[8:], text)
	destination[8+len(text)] = 0
	if payload&1 != 0 {
		destination[8+int(payload)] = 0
	}
	return int(total), nil
}

// infoRewriteWorkspaceAgainst returns the source-plus-output workspace
// required by a source-aware edit. The source range is retained while the
// replacement is assembled, and the output bound conservatively includes
// every expressible target entry in addition to the source bytes preserved for
// unknown fields. Both planes are checked because RIFF fields and runtime
// slices have finite widths.
func infoRewriteWorkspaceAgainst(sourceLength uint64, original, document metadata.Document) (workspace, output uint64, err error) {
	if !original.Valid() || !document.Valid() || original.Scope() != document.Scope() || sourceLength > uint64(math.MaxInt) {
		return 0, 0, fmt.Errorf("%w: RIFF INFO source exceeds runtime memory", ErrUnsupported)
	}
	output = sourceLength
	cursor := infoEntryCursor{document: document}
	for {
		entry, cursorOK, cursorErr := cursor.next(nil)
		if cursorErr != nil {
			return 0, 0, cursorErr
		}
		if !cursorOK {
			break
		}
		encodedSize, encodeErr := infoEntryWireSize(entry)
		if encodeErr != nil {
			return 0, 0, encodeErr
		}
		var addOK bool
		output, addOK = checkedAdd(output, uint64(encodedSize))
		if !addOK || output > uint64(math.MaxInt) {
			return 0, 0, fmt.Errorf("%w: RIFF INFO rewrite output exceeds runtime memory", ErrUnsupported)
		}
	}
	if output > maxInfoCarrierBytes {
		output = maxInfoCarrierBytes
	}
	var ok bool
	workspace, ok = checkedAdd(sourceLength, output)
	if !ok || workspace > uint64(math.MaxInt) {
		return 0, 0, fmt.Errorf("%w: RIFF INFO rewrite workspace exceeds runtime memory", ErrUnsupported)
	}
	return workspace, output, nil
}

func infoPayload(value []byte) ([]byte, error) {
	if len(value) < 12 || string(value[0:4]) != tagLIST {
		return nil, fmt.Errorf("%w: RIFF INFO carrier is not a LIST chunk", ErrMalformed)
	}
	size := uint64(binary.LittleEndian.Uint32(value[4:8]))
	if size < 4 {
		return nil, fmt.Errorf("%w: LIST/INFO payload is shorter than its type", ErrMalformed)
	}
	if size > uint64(math.MaxInt)-8 {
		return nil, fmt.Errorf("%w: LIST/INFO payload exceeds runtime memory", ErrUnsupported)
	}
	total := uint64(8) + size + size&1
	if total > uint64(math.MaxInt) {
		return nil, fmt.Errorf("%w: LIST/INFO carrier exceeds runtime memory", ErrUnsupported)
	}
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
	if len(native) != 4 || uint64(len(payload)) > math.MaxUint32 || len(payload) > math.MaxInt-8 {
		return nil, fmt.Errorf("%w: RIFF INFO chunk identity or size is unsupported", ErrUnsupported)
	}
	total := 8 + len(payload) + (len(payload) & 1)
	if total < len(payload) || total > math.MaxInt {
		return nil, fmt.Errorf("%w: RIFF INFO chunk exceeds runtime memory", ErrUnsupported)
	}
	result := make([]byte, total)
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
