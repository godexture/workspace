package mp4

import (
	"context"
	"fmt"

	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/media/stream"
)

// muxMetadataPlan is settled at compile time, while the source is still an
// immutable inspection. A rewrite owns the encoded ilst payload and the
// envelope geometry needed by the layout writer.
type muxMetadataPlan struct {
	document metadata.Document
	rewrite  muxIlstRewrite
	reports  []loss.Report
}

func compileMuxWithResolver(ctx context.Context, resolver metadata.Resolver, inputs []stream.Descriptor, inspected movie) (muxLayout, error) {
	selected, err := selectMuxTracks(inputs, inspected)
	if err != nil {
		return muxLayout{}, err
	}
	document, err := muxMetadataConsensus(inputs)
	if err != nil {
		return muxLayout{}, err
	}
	metadataPlan, err := planMuxMetadata(ctx, resolver, inspected, document)
	if err != nil {
		return muxLayout{}, err
	}
	return buildMuxLayout(inspected, selected, metadataPlan)
}

func muxMetadataConsensus(inputs []stream.Descriptor) (metadata.Document, error) {
	if len(inputs) == 0 {
		return metadata.Document{}, fmt.Errorf("%w: MP4 mux has no metadata-bearing tracks", ErrUnsupported)
	}
	result := inputs[0].Metadata()
	if !muxAssetDocument(result) {
		return metadata.Document{}, fmt.Errorf("%w: MP4 mux input metadata is not an asset document", ErrUnsupported)
	}
	for index := 1; index < len(inputs); index++ {
		value := inputs[index].Metadata()
		if !muxAssetDocument(value) || !sameIlstMuxDocument(result, value) {
			return metadata.Document{}, fmt.Errorf("%w: MP4 selected tracks disagree on metadata", ErrUnsupported)
		}
	}
	return result, nil
}

func muxAssetDocument(value metadata.Document) bool {
	if value.Scope() == 0 && value.Len() == 0 && len(value.Blocks()) == 0 {
		return true
	}
	return value.Scope() == metadata.AssetScope
}

func planMuxMetadata(ctx context.Context, resolver metadata.Resolver, inspected movie, document metadata.Document) (muxMetadataPlan, error) {
	result := muxMetadataPlan{document: document}
	if sameIlstMuxDocument(document, inspected.metadata) {
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
	if err := validateIlstRewriteLineage(inspected.metadata, inspected.ilst, document); err != nil {
		return muxMetadataPlan{}, err
	}
	payload, reports, err := resolver.Marshal(ctx, IlstCarrier(), inspected.ilst.block, document)
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
