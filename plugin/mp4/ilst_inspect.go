package mp4

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/media/metadata"
)

type ilstEnvelope struct {
	udta  box
	meta  box
	ilst  box
	block metadata.BlockID
}

type ilstMetadataInspection struct {
	metadata    metadata.Attachment
	envelope    ilstEnvelope
	offsetIndex bool
}

type ilstEnvelopeScan struct {
	envelope    ilstEnvelope
	found       bool
	ambiguous   bool
	unsafe      bool
	offsetIndex bool
}

func (e ilstEnvelope) valid() bool {
	return e.udta.typeID == typeUDTA && e.meta.typeID == typeMETA && e.ilst.typeID == typeILST && e.block != ""
}

func ilstSourceBlockID(value box) metadata.BlockID {
	return metadata.BlockID(fmt.Sprintf("mp4/ilst/%016x", value.offset))
}

// inspectIlstMetadata classifies the supported moov/udta/meta iTunes region.
// Structure and encoding-content failures become Unavailable, while source
// execution failures remain inspection errors.
func inspectIlstMetadata(ctx context.Context, reader access.Random, sourceEnd uint64, moov box, resolver metadata.Resolver, budget *movieBudget) (ilstMetadataInspection, error) {
	result := ilstMetadataInspection{metadata: metadata.Absent()}
	if budget == nil {
		return result, nil
	}
	scan, err := findIlstEnvelope(ctx, reader, sourceEnd, moov)
	result.offsetIndex = scan.offsetIndex
	if err != nil {
		if ilstRequiredMetadataFailure(err) {
			return result, err
		}
		result.metadata = metadata.MustUnavailable(metadata.AssetScope)
		return result, nil
	}
	if scan.unsafe || scan.ambiguous {
		result.metadata = metadata.MustUnavailable(metadata.AssetScope)
		return result, nil
	}
	if !scan.found {
		return result, nil
	}
	if !resolver.Valid() {
		resolver, err = metadata.NewResolver(nil, nil)
		if err != nil {
			return result, err
		}
	}
	envelope := scan.envelope
	if !resolver.HasBinding(IlstCarrier()) {
		_, err := resolver.Parse(ctx, IlstCarrier(), envelope.block, metadata.AssetScope, metadata.NewBlob(ilstMediaType, nil))
		if err == nil {
			return result, fmt.Errorf("%w: ilst resolver binding is missing", errUnsupportedMovie)
		}
		return result, err
	}
	if envelope.ilst.payloadSize > uint64(math.MaxInt) {
		return result, fmt.Errorf("%w: iTunes ilst payload %d exceeds runtime memory", errUnsupportedMovie, envelope.ilst.payloadSize)
	}
	if err := budget.reserve(envelope.ilst.payloadSize, "iTunes ilst payload"); err != nil {
		return result, err
	}

	raw := make([]byte, int(envelope.ilst.payloadSize))
	if err := readMovieAt(ctx, reader, raw, envelope.ilst.payloadOffset, "iTunes ilst payload"); err != nil {
		return result, err
	}
	payload := metadata.NewBlob(ilstMediaType, raw)
	parsed, err := resolver.Parse(ctx, IlstCarrier(), envelope.block, metadata.AssetScope, payload)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errInspectReadBudget) {
			return result, err
		}
		result.metadata = metadata.MustUnavailable(metadata.AssetScope)
		return result, nil
	}
	if err := context.Cause(ctx); err != nil {
		return result, err
	}
	if !parsed.Valid() || parsed.Scope() != metadata.AssetScope {
		result.metadata = metadata.MustUnavailable(metadata.AssetScope)
		return result, nil
	}
	result.metadata = metadata.MustAvailable(parsed)
	result.envelope = envelope
	return result, nil
}

func ilstRequiredMetadataFailure(err error) bool {
	if err == nil {
		return false
	}
	switch value := err.(type) {
	case interface{ Unwrap() []error }:
		for _, item := range value.Unwrap() {
			if ilstRequiredMetadataFailure(item) {
				return true
			}
		}
		return false
	case interface{ Unwrap() error }:
		return ilstRequiredMetadataFailure(value.Unwrap())
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errTruncatedMovie) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	if errors.Is(err, errInspectReadBudget) {
		return true
	}
	if errors.Is(err, errRuntimeRange) {
		return true
	}
	return !errors.Is(err, errMalformedMovie) && !errors.Is(err, errMalformedBox) && !errors.Is(err, errUnsupportedMovie)
}
func findIlstEnvelope(ctx context.Context, reader access.Random, sourceEnd uint64, moov box) (ilstEnvelopeScan, error) {
	var result ilstEnvelopeScan
	err := scanChildBoxes(ctx, reader, sourceEnd, moov, func(child box) error {
		if child.typeID != typeUDTA {
			return nil
		}
		err := scanChildBoxes(ctx, reader, sourceEnd, child, func(meta box) error {
			if meta.typeID != typeMETA {
				return nil
			}
			candidate, present, err := findIlstInMeta(ctx, reader, sourceEnd, meta, &result)
			if err != nil {
				if !ilstRequiredMetadataFailure(err) {
					result.unsafe = true
					if ilstOptionalTraversalFailure(err) {
						result.offsetIndex = true
					}
					return nil
				}
				return err
			}
			if !present {
				return nil
			}
			if result.found {
				result.ambiguous = true
				return nil
			}
			candidate.udta = child
			candidate.block = ilstSourceBlockID(candidate.ilst)
			result.envelope, result.found = candidate, true
			return nil
		})
		if err != nil {
			if !ilstRequiredMetadataFailure(err) {
				result.unsafe = true
				if ilstOptionalTraversalFailure(err) {
					result.offsetIndex = true
				}
				return nil
			}
			return err
		}
		return nil
	})
	if err != nil {
		if ilstOptionalTraversalFailure(err) {
			result.unsafe = true
			result.offsetIndex = true
		}
		return result, err
	}
	return result, nil
}

// ilstOptionalTraversalFailure reports a box walk that stopped before it
// could establish the absence of offset records. Malformed/unsupported
// structure is classified as content; read-budget, source I/O, cancellation,
// and truncation remain required failures and are propagated.
func ilstOptionalTraversalFailure(err error) bool {
	return !ilstRequiredMetadataFailure(err)
}

func findIlstInMeta(ctx context.Context, reader access.Random, sourceEnd uint64, value box, result *ilstEnvelopeScan) (ilstEnvelope, bool, error) {
	var prefix [4]byte
	if err := readBoxPrefix(ctx, reader, value, prefix[:], "meta"); err != nil {
		if !ilstRequiredMetadataFailure(err) {
			result.unsafe = true
		}
		return ilstEnvelope{}, false, err
	}
	if prefix != [4]byte{} {
		result.unsafe = true
		result.offsetIndex = true
		return ilstEnvelope{}, false, nil
	}
	start, ok := checkedBoxAdd(value.payloadOffset, uint64(len(prefix)))
	end, endOK := payloadEnd(value)
	if !ok || !endOK || start > end {
		result.unsafe = true
		return ilstEnvelope{}, false, fmt.Errorf("%w: meta payload range", errMalformedMovie)
	}
	var handler box
	var ilst box
	hasIloc := false
	var handlerCount, ilstCount int
	err := scanBoxes(ctx, reader, boxScope{sourceEnd: sourceEnd, start: start, end: end}, func(child box) error {
		switch child.typeID {
		case typeHDLR:
			handlerCount++
			handler = child
		case typeILST:
			ilstCount++
			ilst = child
		case typeILOC:
			hasIloc = true
			result.offsetIndex = true
		}
		return nil
	})
	if err != nil {
		result.unsafe = true
		// The meta is proven ISO FullBox but its child list cannot prove that no
		// direct iloc records stale output offsets.
		result.offsetIndex = true
		return ilstEnvelope{}, false, err
	}
	if handlerCount > 1 {
		result.unsafe = true
		result.ambiguous = true
		return ilstEnvelope{}, false, nil
	}
	if handlerCount == 0 {
		if ilstCount > 0 || hasIloc {
			result.unsafe = true
		}
		return ilstEnvelope{}, false, nil
	}
	mdir, err := isIlstHandler(ctx, reader, handler)
	if err != nil {
		if !ilstRequiredMetadataFailure(err) {
			result.unsafe = true
			return ilstEnvelope{}, false, nil
		}
		return ilstEnvelope{}, false, err
	}
	if !mdir {
		result.unsafe = true
		return ilstEnvelope{}, false, nil
	}
	if ilstCount != 1 {
		if ilstCount > 1 || hasIloc {
			result.unsafe = true
		}
		if ilstCount > 1 {
			result.ambiguous = true
			return ilstEnvelope{}, false, nil
		}
		return ilstEnvelope{}, false, nil
	}
	return ilstEnvelope{meta: value, ilst: ilst}, true, nil
}

func isIlstHandler(ctx context.Context, reader access.Random, value box) (bool, error) {
	var prefix [24]byte
	if err := readBoxPrefix(ctx, reader, value, prefix[:], "meta hdlr"); err != nil {
		return false, err
	}
	if prefix[0] != 0 || prefix[1] != 0 || prefix[2] != 0 || prefix[3] != 0 {
		return false, nil
	}
	return boxType(prefix[8:12]) == typeMDIR, nil
}
