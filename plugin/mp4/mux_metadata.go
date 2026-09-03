package mp4

import (
	"context"
	"errors"
	"fmt"

	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/media/stream"
)

// muxMetadataPlan is settled at compile time, while the source is still an
// immutable inspection. A rewrite owns the encoded ilst payload and the
// envelope geometry needed by the layout writer.
type muxMetadataPlan struct {
	attachment metadata.Attachment
	rewrite    muxIlstRewrite
	reports    []loss.Report
}

func compileMuxWithResolver(ctx context.Context, resolver metadata.Resolver, inputs []stream.Descriptor, inspected movie) (muxLayout, error) {
	selected, err := selectMuxTracks(inputs, inspected)
	if err != nil {
		return muxLayout{}, err
	}
	attachment, err := muxMetadataConsensus(inputs)
	if err != nil {
		return muxLayout{}, err
	}
	metadataPlan, err := planMuxMetadata(ctx, resolver, inspected, attachment)
	if err != nil {
		return muxLayout{}, err
	}
	return buildMuxLayout(inspected, selected, metadataPlan)
}

func muxMetadataConsensus(inputs []stream.Descriptor) (metadata.Attachment, error) {
	if len(inputs) == 0 {
		return metadata.Absent(), fmt.Errorf("%w: MP4 mux has no metadata-bearing tracks", ErrUnsupported)
	}
	result := inputs[0].Metadata()
	if !result.Valid() {
		return metadata.Absent(), fmt.Errorf("%w: MP4 mux input metadata state is invalid", ErrUnsupported)
	}
	if !muxAssetAttachment(result) {
		return metadata.Absent(), fmt.Errorf("%w: MP4 mux input metadata is not an asset attachment", ErrUnsupported)
	}
	for index := 1; index < len(inputs); index++ {
		valueAttachment := inputs[index].Metadata()
		if !valueAttachment.Valid() {
			return metadata.Absent(), fmt.Errorf("%w: MP4 mux input metadata state is invalid", ErrUnsupported)
		}
		if !muxAssetAttachment(valueAttachment) {
			return metadata.Absent(), fmt.Errorf("%w: MP4 mux input metadata is not an asset attachment", ErrUnsupported)
		}
		if !result.SameState(valueAttachment) {
			return metadata.Absent(), fmt.Errorf("%w: MP4 selected tracks disagree on metadata state", ErrUnsupported)
		}
	}
	if !result.IsAvailable() {
		return result, nil
	}
	document, err := muxSemanticDocument(result)
	if err != nil {
		return metadata.Absent(), err
	}
	if !muxAssetDocument(document) {
		return metadata.Absent(), fmt.Errorf("%w: MP4 mux input metadata is not an asset document", ErrUnsupported)
	}
	for index := 1; index < len(inputs); index++ {
		valueAttachment := inputs[index].Metadata()
		value, err := muxSemanticDocument(valueAttachment)
		if err != nil {
			return metadata.Absent(), err
		}
		if !muxAssetDocument(value) || !sameIlstMuxDocument(document, value) {
			return metadata.Absent(), fmt.Errorf("%w: MP4 selected tracks disagree on metadata", ErrUnsupported)
		}
	}
	return result, nil
}

func muxSemanticDocument(value metadata.Attachment) (metadata.Document, error) {
	document, err := value.Semantic()
	if errors.Is(err, metadata.ErrMetadataAbsent) {
		return metadata.Document{}, nil
	}
	if err != nil {
		return metadata.Document{}, fmt.Errorf("%w: MP4 mux input metadata is unavailable: %w", ErrUnsupported, err)
	}
	return document, nil
}

func muxAssetDocument(value metadata.Document) bool {
	if value.Scope() == 0 && value.Len() == 0 && len(value.Blocks()) == 0 {
		return true
	}
	return value.Scope() == metadata.AssetScope
}

func muxAssetAttachment(value metadata.Attachment) bool {
	return value.IsAbsent() || value.Scope() == metadata.AssetScope
}

func planMuxMetadata(ctx context.Context, resolver metadata.Resolver, inspected movie, attachment metadata.Attachment) (muxMetadataPlan, error) {
	result := muxMetadataPlan{attachment: attachment}
	sourceAttachment := inspected.metadataAttachment()
	if !attachment.Valid() || !sourceAttachment.Valid() {
		return muxMetadataPlan{}, fmt.Errorf("%w: MP4 metadata state is invalid", ErrUnsupported)
	}
	if !muxAssetAttachment(attachment) || !muxAssetAttachment(sourceAttachment) {
		return muxMetadataPlan{}, fmt.Errorf("%w: MP4 metadata is not an asset attachment", ErrUnsupported)
	}
	if !attachment.SameState(sourceAttachment) {
		return muxMetadataPlan{}, fmt.Errorf("%w: MP4 metadata state does not match inspected source", ErrUnsupported)
	}
	if sourceAttachment.IsUnavailable() || sourceAttachment.IsAbsent() {
		return result, nil
	}
	document, err := muxSemanticDocument(attachment)
	if err != nil {
		return muxMetadataPlan{}, err
	}
	sourceDocument, err := muxSemanticDocument(sourceAttachment)
	if err != nil {
		return muxMetadataPlan{}, err
	}
	if sameIlstMuxDocument(document, sourceDocument) {
		return result, nil
	}
	if !inspected.ilst.valid() {
		return muxMetadataPlan{}, fmt.Errorf("%w: MP4 metadata changed but no inspected ilst envelope is available", ErrUnsupported)
	}
	if !resolver.Valid() {
		var err error
		resolver, err = metadata.NewResolver(nil, nil)
		if err != nil {
			return muxMetadataPlan{}, err
		}
	}
	if err := validateIlstRewriteLineage(sourceDocument, inspected.ilst, document); err != nil {
		return muxMetadataPlan{}, err
	}
	payload, reports, err := resolver.Marshal(ctx, IlstCarrier(), inspected.ilst.block, attachment)
	if err != nil {
		return muxMetadataPlan{}, err
	}
	rewrite, err := newIlstRewrite(inspected.ilst, payload)
	if err != nil {
		return muxMetadataPlan{}, err
	}
	result.rewrite = rewrite
	result.reports = reports
	return result, nil
}

func validateIlstRewriteLineage(source metadata.Document, envelope ilstEnvelope, candidate metadata.Document) error {
	root, ok := source.Block(envelope.block)
	if !ok || !root.Source() || root.Carrier() != IlstCarrier() || !root.Payload().Valid() {
		return fmt.Errorf("%w: inspected ilst source block %q is invalid", ErrMalformed, envelope.block)
	}
	retained, ok := candidate.Block(envelope.block)
	if !ok || !sameIlstBlock(root, retained) {
		return fmt.Errorf("%w: MP4 metadata rewrite dropped or changed ilst source block %q", ErrUnsupported, envelope.block)
	}
	for _, block := range source.Blocks() {
		if block.Source() {
			continue
		}
		value, ok := candidate.Block(block.ID())
		if !ok || !sameIlstBlock(block, value) {
			return fmt.Errorf("%w: MP4 metadata rewrite dropped or changed opaque ilst block %q", ErrUnsupported, block.ID())
		}
	}
	return nil
}
