package vorbiscomment

import (
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/plugin"
)

const (
	mediaType       = "application/x-vorbis-comment"
	vendorMediaType = "application/x-vorbis-comment-vendor"
	fieldMediaType  = "application/x-vorbis-comment-field"
	unsafeMediaType = "application/x-vorbis-comment-unsafe"
	defaultVendor   = "godexture/godec"
)

var (
	errMalformed   = errors.New("malformed Vorbis Comment block")
	errUnsupported = errors.New("unsupported Vorbis Comment metadata")
)

type layout struct {
	unsafe bool
	raw    map[metadata.BlockID]int
}

func parse(ctx metadata.ParseContext) (metadata.Document, error) {
	document, _, err := parseDocument(ctx.Scope(), ctx.Carrier(), ctx.Encoding(), ctx.Block(), ctx.Payload())
	return document, err
}

func parseDocument(scope metadata.Scope, slot carrier.ID, encoding plugin.Identity, root metadata.BlockID, payload metadata.Blob) (metadata.Document, layout, error) {
	reader := vcReader{blob: payload}
	vendorStart, vendorEnd, ok := reader.stringRange()
	if !ok {
		return metadata.Document{}, layout{}, errMalformed
	}
	count, ok := reader.u32()
	if !ok || uint64(count) > uint64((payload.Len()-reader.offset)/4) {
		return metadata.Document{}, layout{}, errMalformed
	}
	builder := metadata.NewBuilder(scope)
	builder.AddBlock(metadata.NewSourceBlock(root, slot, encoding, payload))
	result := layout{raw: make(map[metadata.BlockID]int)}
	vendor, _ := payload.Slice(vendorMediaType, vendorStart, vendorEnd)
	if !utf8.Valid(vendor.AppendTo(nil)) {
		vendor, _ = payload.Slice(unsafeMediaType, vendorStart, vendorEnd)
		result.unsafe = true
	}
	builder.AddBlock(metadata.NewRawBlock(vendorBlockID(root), slot, encoding, vendor))
	semantic := 0
	for index := uint32(0); index < count; index++ {
		start, end, ok := reader.stringRange()
		if !ok {
			return metadata.Document{}, layout{}, errMalformed
		}
		fieldBlock, _ := payload.Slice(fieldMediaType, start, end)
		id := fieldBlockID(root, int(index))
		value := fieldBlock.AppendTo(nil)
		if !utf8.Valid(value) || !vcFieldSyntax(value) {
			unsafe, _ := payload.Slice(unsafeMediaType, start, end)
			builder.AddBlock(metadata.NewRawBlock(id, slot, encoding, unsafe))
			result.raw[id] = semantic
			result.unsafe = true
			continue
		}
		units, known := applyField(builder, slot, encoding, root, string(value))
		if known {
			semantic += units
			continue
		}
		builder.AddBlock(metadata.NewRawBlock(id, slot, encoding, fieldBlock))
		result.raw[id] = semantic
	}
	if reader.offset != payload.Len() {
		return metadata.Document{}, layout{}, errMalformed
	}
	document, err := builder.Build()
	if err != nil {
		return metadata.Document{}, layout{}, err
	}
	return document, result, nil
}

func marshal(ctx metadata.MarshalContext) (metadata.Blob, []loss.Loss, error) {
	if source, ok := ctx.Document().Block(ctx.Block()); ok && source.Source() && source.Carrier() == ctx.Carrier() && source.Encoding() == ctx.Encoding() {
		return marshalSource(ctx, source)
	}
	return marshalCanonical(ctx.Document(), nil, ctx.Carrier(), ctx.Encoding(), ctx.Block())
}

func marshalSource(ctx metadata.MarshalContext, source metadata.RawBlock) (metadata.Blob, []loss.Loss, error) {
	parsed, layout, err := parseDocument(ctx.Document().Scope(), ctx.Carrier(), ctx.Encoding(), ctx.Block(), source.Payload())
	if err != nil {
		return metadata.Blob{}, nil, err
	}
	if sameDocument(ctx.Document(), parsed) {
		return source.Payload(), nil, nil
	}
	if layout.unsafe {
		return metadata.Blob{}, nil, fmt.Errorf("%w: source contains invalid vendor or field", errUnsupported)
	}
	return marshalCanonical(ctx.Document(), &layout, ctx.Carrier(), ctx.Encoding(), ctx.Block())
}

func sameDocument(left, right metadata.Document) bool {
	leftEntries, rightEntries := left.Entries(), right.Entries()
	if len(leftEntries) != len(rightEntries) {
		return false
	}
	for index := range leftEntries {
		if leftEntries[index].Key() != rightEntries[index].Key() || leftEntries[index].Origin() != rightEntries[index].Origin() || !reflect.DeepEqual(leftEntries[index].Value(), rightEntries[index].Value()) {
			return false
		}
	}
	return sameOpaque(left.Blocks(), right.Blocks())
}

func sameOpaque(left, right []metadata.RawBlock) bool {
	left, right = opaque(left), opaque(right)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].ID() != right[index].ID() || left[index].Carrier() != right[index].Carrier() || left[index].Encoding() != right[index].Encoding() || !left[index].Payload().Equal(right[index].Payload()) {
			return false
		}
	}
	return true
}

func opaque(blocks []metadata.RawBlock) []metadata.RawBlock {
	result := make([]metadata.RawBlock, 0, len(blocks))
	for _, block := range blocks {
		if !block.Source() {
			result = append(result, block)
		}
	}
	return result
}

func vendorBlockID(root metadata.BlockID) metadata.BlockID { return root + "/vendor" }

func fieldBlockID(root metadata.BlockID, index int) metadata.BlockID {
	return metadata.BlockID(fmt.Sprintf("%s/field/%08d", root, index))
}

func parseFieldBlockID(root, value metadata.BlockID) (int, bool) {
	prefix := string(root) + "/field/"
	text := strings.TrimPrefix(string(value), prefix)
	if text == string(value) || len(text) != 8 {
		return 0, false
	}
	index, err := strconv.ParseUint(text, 10, 31)
	return int(index), err == nil
}

type vcReader struct {
	blob   metadata.Blob
	offset int
}

func (r *vcReader) u32() (uint32, bool) {
	if r.offset > r.blob.Len()-4 {
		return 0, false
	}
	value := make([]byte, 4)
	if _, err := r.blob.Reader().ReadAt(value, int64(r.offset)); err != nil {
		return 0, false
	}
	r.offset += 4
	return binary.LittleEndian.Uint32(value), true
}

func (r *vcReader) stringRange() (int, int, bool) {
	length, ok := r.u32()
	if !ok || uint64(length) > uint64(r.blob.Len()-r.offset) {
		return 0, 0, false
	}
	start := r.offset
	r.offset += int(length)
	return start, r.offset, true
}

func vcLoss(entry metadata.Entry, kind loss.Kind, native, detail string) loss.Loss {
	return loss.Loss{Key: entry.Key(), Kind: kind, Native: native, Detail: detail, Source: entry.Origin().LossOrigin()}
}
