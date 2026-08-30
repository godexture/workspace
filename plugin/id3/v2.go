package id3

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"

	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/metadata/loss"
)

const (
	v2HeaderSize         = 10
	v2MediaType          = "application/x-id3v2"
	v2RawMediaType       = "application/x-id3v2.4-frame"
	v2UnsafeRawMediaType = "application/x-id3v2-unsafe-frame"
)

var (
	errV2Malformed   = errors.New("malformed ID3v2 tag")
	errV2Unsupported = errors.New("unsupported ID3v2 metadata")
)

type v2Tag struct {
	version         byte
	decodeUnsafe    bool
	editUnsafe      bool
	sourceUnsync    bool
	tagUnsync       bool
	footer          bool
	paddingDeclared bool
	frames          metadata.Blob
}

type v2Layout struct {
	unsafe   bool
	opaque   map[metadata.BlockID]int
	semantic int
}

func parseV2(ctx metadata.ParseContext) (metadata.Document, error) {
	document, _, err := parseV2Document(ctx.Scope(), ctx.Carrier(), ctx.Encoding(), ctx.Block(), ctx.Payload())
	return document, err
}

func marshalV2(ctx metadata.MarshalContext) (metadata.Blob, []loss.Loss, error) {
	if source, ok := ctx.Document().Block(ctx.Block()); ok && source.Source() && source.Carrier() == ctx.Carrier() && source.Encoding() == ctx.Encoding() {
		return marshalV2Source(ctx, source)
	}
	return marshalV2Canonical(ctx.Document(), nil, ctx.Carrier(), ctx.Encoding())
}

func marshalV2Source(ctx metadata.MarshalContext, source metadata.RawBlock) (metadata.Blob, []loss.Loss, error) {
	parsed, layout, err := parseV2Document(ctx.Document().Scope(), ctx.Carrier(), ctx.Encoding(), ctx.Block(), source.Payload())
	if err != nil {
		return metadata.Blob{}, nil, err
	}
	if sameV2Content(ctx.Document(), parsed) {
		return source.Payload(), nil, nil
	}
	if layout.unsafe {
		return metadata.Blob{}, nil, fmt.Errorf("%w: ID3v2 source restrictions cannot be migrated", errV2Unsupported)
	}
	return marshalV2Canonical(ctx.Document(), &layout, ctx.Carrier(), ctx.Encoding())
}

func sameV2Content(left, right metadata.Document) bool {
	leftEntries, rightEntries := left.Entries(), right.Entries()
	if len(leftEntries) != len(rightEntries) {
		return false
	}
	for index := range leftEntries {
		if leftEntries[index].Key() != rightEntries[index].Key() || leftEntries[index].Origin() != rightEntries[index].Origin() || !reflect.DeepEqual(leftEntries[index].Value(), rightEntries[index].Value()) {
			return false
		}
	}
	return sameV2OpaqueBlocks(left.Blocks(), right.Blocks())
}

func sameV2OpaqueBlocks(left, right []metadata.RawBlock) bool {
	left = v2OpaqueBlocks(left)
	right = v2OpaqueBlocks(right)
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

func v2OpaqueBlocks(values []metadata.RawBlock) []metadata.RawBlock {
	result := make([]metadata.RawBlock, 0, len(values))
	for _, value := range values {
		if !value.Source() {
			result = append(result, value)
		}
	}
	return result
}

func v2SafeRawFrame(value []byte) bool {
	if len(value) < 10 || bytes.Equal(value[:4], []byte{0, 0, 0, 0}) || value[8] != 0 || value[9] != 0 {
		return false
	}
	size, ok := v2DecodeSyncSafe(value[4:8])
	return ok && size == len(value)-10
}

func v2BuildFrame(id string, payload []byte) []byte {
	frame := make([]byte, 0, 10+len(payload))
	frame = v2AppendFrame(frame, id, len(payload))
	return append(frame, payload...)
}

func v2AppendFrame(destination []byte, id string, payloadSize int) []byte {
	destination = append(destination, id...)
	destination = append(destination, v2EncodeSyncSafe(payloadSize)...)
	return append(destination, 0, 0)
}

func v2Loss(entry metadata.Entry, kind loss.Kind, native, detail string) loss.Loss {
	return loss.Loss{Key: entry.Key(), Kind: kind, Native: native, Detail: detail, Source: entry.Origin().LossOrigin()}
}

func v2DecodeSyncSafe(value []byte) (int, bool) {
	if len(value) != 4 || value[0]&0x80 != 0 || value[1]&0x80 != 0 || value[2]&0x80 != 0 || value[3]&0x80 != 0 {
		return 0, false
	}
	return int(value[0])<<21 | int(value[1])<<14 | int(value[2])<<7 | int(value[3]), true
}

func v2EncodeSyncSafe(value int) []byte {
	return []byte{byte(value>>21) & 0x7f, byte(value>>14) & 0x7f, byte(value>>7) & 0x7f, byte(value) & 0x7f}
}

func v2RemoveUnsync(value []byte) []byte {
	result := make([]byte, 0, len(value))
	for index := 0; index < len(value); index++ {
		result = append(result, value[index])
		if value[index] == 0xff && index+1 < len(value) && value[index+1] == 0 {
			index++
		}
	}
	return result
}
