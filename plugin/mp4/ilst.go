package mp4

import (
	"fmt"
	"reflect"

	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/plugin"
)

const (
	ilstMediaType     = "application/x-itunes-ilst"
	ilstItemMediaType = "application/x-itunes-ilst-item"

	ilstDataTypeUTF8 uint32 = 1
	ilstDataTypeJPEG uint32 = 13
	ilstDataTypePNG  uint32 = 14
)

var (
	errIlstMalformed   = fmt.Errorf("%w: malformed iTunes ilst metadata", ErrMalformed)
	errIlstUnsupported = fmt.Errorf("%w: unsupported iTunes ilst metadata", ErrUnsupported)
)

type ilstLayout struct {
	opaque map[metadata.BlockID]int
	known  []ilstKnownItem
}

type ilstKnownItem struct {
	entries []metadata.Entry
	raw     metadata.Blob
	start   int
	count   int
}

func ilstComponent() plugin.Component {
	return plugin.NewComponent[ilstID](plugin.Descriptor{DisplayName: "iTunes ilst metadata encoding"}, configurationSchema(),
		metadata.WithEncoding(parseIlst, marshalIlst,
			tag.Title().Erased(),
			tag.Artist().Erased(),
			tag.Album().Erased(),
			tag.Composer().Erased(),
			tag.Genre().Erased(),
			tag.Date().Erased(),
			tag.Comment().Erased(),
			tag.Lyrics().Erased(),
			tag.Copyright().Erased(),
			tag.Encoder().Erased(),
			tag.TrackNumber().Erased(),
			tag.TotalTracks().Erased(),
			tag.DiscNumber().Erased(),
			tag.TotalDiscs().Erased(),
			tag.Picture().Erased(),
		),
	)
}

func parseIlst(ctx metadata.ParseContext) (metadata.Document, error) {
	document, _, err := parseIlstDocument(ctx.Scope(), ctx.Carrier(), ctx.Encoding(), ctx.Block(), ctx.Payload())
	return document, err
}

func marshalIlst(ctx metadata.MarshalContext) (metadata.Blob, []loss.Loss, error) {
	if source, ok := ctx.Document().Block(ctx.Block()); ok && source.Source() && source.Carrier() == ctx.Carrier() && source.Encoding() == ctx.Encoding() {
		return marshalIlstSource(ctx, source)
	}
	return marshalIlstCanonical(ctx.Document(), nil, ctx.Carrier(), ctx.Encoding(), ctx.Block())
}

func marshalIlstSource(ctx metadata.MarshalContext, source metadata.RawBlock) (metadata.Blob, []loss.Loss, error) {
	parsed, layout, err := parseIlstDocument(ctx.Document().Scope(), ctx.Carrier(), ctx.Encoding(), ctx.Block(), source.Payload())
	if err != nil {
		return metadata.Blob{}, nil, err
	}
	if sameIlstSourceDocument(ctx.Document(), parsed, source.ID()) {
		return source.Payload(), nil, nil
	}
	return marshalIlstCanonical(ctx.Document(), &layout, ctx.Carrier(), ctx.Encoding(), ctx.Block())
}

func sameIlstMuxDocument(left, right metadata.Document) bool {
	leftBlocks, rightBlocks := left.Blocks(), right.Blocks()
	leftEmpty := left.Len() == 0 && len(leftBlocks) == 0
	rightEmpty := right.Len() == 0 && len(rightBlocks) == 0
	if leftEmpty && rightEmpty {
		if left.Scope() == right.Scope() {
			return left.Scope() == 0 || left.Scope() == metadata.AssetScope
		}
		return (left.Scope() == 0 && right.Scope() == metadata.AssetScope) || (left.Scope() == metadata.AssetScope && right.Scope() == 0)
	}
	if left.Scope() != right.Scope() {
		return false
	}
	leftEntries, rightEntries := left.Entries(), right.Entries()
	if len(leftEntries) != len(rightEntries) {
		return false
	}
	for index := range leftEntries {
		if !ilstSameEntry(leftEntries[index], rightEntries[index]) {
			return false
		}
	}
	if len(leftBlocks) != len(rightBlocks) {
		return false
	}
	for index := range leftBlocks {
		leftBlock, rightBlock := leftBlocks[index], rightBlocks[index]
		if leftBlock.ID() != rightBlock.ID() || leftBlock.Carrier() != rightBlock.Carrier() || leftBlock.Encoding() != rightBlock.Encoding() || leftBlock.Source() != rightBlock.Source() || !leftBlock.Payload().Equal(rightBlock.Payload()) {
			return false
		}
	}
	return true
}

func sameIlstSourceDocument(left, right metadata.Document, source metadata.BlockID) bool {
	if left.Scope() != right.Scope() {
		return false
	}
	leftEntries, rightEntries := left.Entries(), right.Entries()
	if len(leftEntries) != len(rightEntries) {
		return false
	}
	for index := range leftEntries {
		if !ilstSameEntry(leftEntries[index], rightEntries[index]) {
			return false
		}
	}
	leftSource, leftOK := left.Block(source)
	rightSource, rightOK := right.Block(source)
	if !leftOK || !rightOK || !sameIlstBlock(leftSource, rightSource) {
		return false
	}
	return sameIlstOpaque(left.Blocks(), right.Blocks())
}

func sameIlstOpaque(left, right []metadata.RawBlock) bool {
	left = ilstOpaqueBlocks(left)
	right = ilstOpaqueBlocks(right)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !sameIlstBlock(left[index], right[index]) {
			return false
		}
	}
	return true
}

func ilstOpaqueBlocks(values []metadata.RawBlock) []metadata.RawBlock {
	result := make([]metadata.RawBlock, 0, len(values))
	for _, value := range values {
		if !value.Source() {
			result = append(result, value)
		}
	}
	return result
}

func sameIlstBlock(left, right metadata.RawBlock) bool {
	return left.ID() == right.ID() && left.Carrier() == right.Carrier() && left.Encoding() == right.Encoding() && left.Source() == right.Source() && left.Payload().Equal(right.Payload())
}

func ilstSameEntry(left, right metadata.Entry) bool {
	return left.Key() == right.Key() && left.Origin() == right.Origin() && reflect.DeepEqual(left.Value(), right.Value())
}

func ilstLoss(entry metadata.Entry, kind loss.Kind, native, detail string) loss.Loss {
	return loss.Loss{Key: entry.Key(), Kind: kind, Native: native, Detail: detail, Source: entry.Origin().LossOrigin()}
}
