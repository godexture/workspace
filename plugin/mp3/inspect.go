package mp3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin/mp3/internal/frame"
	"github.com/godexture/godec/resource"
)

var errInspectReadBudget = errors.New("MP3 inspection read budget exhausted")

type tagExtent struct {
	slot      carrier.ID
	offset    int64
	size      int64
	mediaType string
	block     metadata.BlockID
}

// inspection is immutable after Inspect returns. It carries only the source
// ranges and framing invariants needed by the reader and parser.
type inspection struct {
	sourceEnd     int64
	audioStart    int64
	audioEnd      int64
	first         frame.Header
	firstBytes    int
	freeBytes     int
	maxFrameBytes int
	signal        sample.Signal
	metadata      metadata.Attachment
	framing       framing
	duration      timing.Duration
	hasDuration   bool
}

func (i inspection) valid() bool {
	return i.sourceEnd > 0 && i.audioStart >= 0 && i.audioStart < i.audioEnd && i.audioEnd <= i.sourceEnd &&
		i.first.Valid() && i.firstBytes >= 4 && i.maxFrameBytes >= i.firstBytes && i.signal.Valid() && i.metadata.Valid() && i.framing.valid() &&
		(!i.hasDuration || i.duration >= 0)
}

func (i inspection) properties() (property.Set, error) {
	properties, err := i.signal.Properties()
	if err != nil {
		return property.Set{}, err
	}
	properties, err = codec.WithTag(properties, CodecTag())
	if err != nil {
		return property.Set{}, err
	}
	properties, err = withFraming(properties, i.framing)
	if err != nil {
		return property.Set{}, err
	}
	if i.hasDuration {
		properties, err = stream.WithDuration(properties, i.duration)
	}
	return properties, err
}

func inspectMP3(ctx format.InspectContext) (format.Inspection, error) {
	random, ok := access.RandomOf(ctx.Opening())
	if !ok {
		return format.Inspection{}, fmt.Errorf("%w: MP3 Inspect requires random read", ErrUnsupported)
	}
	sizer, ok := access.StableSizeOf(ctx.Opening())
	if !ok {
		return format.Inspection{}, fmt.Errorf("%w: MP3 Inspect requires stable size", ErrUnsupported)
	}
	sourceEnd, err := sizer.Size(ctx.Context())
	if err != nil {
		return format.Inspection{}, err
	}
	if sourceEnd <= 0 {
		return format.Inspection{}, fmt.Errorf("%w: stable source size is %d", ErrMalformed, sourceEnd)
	}

	reader := &inspectReader{reader: random, remaining: uint64(ctx.Limit())}
	extents, audioStart, audioEnd, err := discoverExtents(ctx.Context(), reader, sourceEnd)
	if err != nil {
		return format.Inspection{}, err
	}
	resolver, _ := metadata.ResolverOf(ctx.Prepared())
	if err := requireTagBindings(ctx.Context(), resolver, extents); err != nil {
		return format.Inspection{}, err
	}
	initial, initialHeader, err := readInitialHeader(ctx.Context(), reader, audioStart, audioEnd)
	if err != nil {
		return format.Inspection{}, err
	}
	scanWorkspace := scanWorkspaceFor(initialHeader)
	audioLength := audioEnd - audioStart
	if audioLength < 0 {
		return format.Inspection{}, fmt.Errorf("%w: audio range exceeds runtime address space", ErrUnsupported)
	}
	if int64(scanWorkspace) > audioLength {
		scanWorkspace = int(audioLength)
	}
	metadataBytes, err := extentBytes(extents)
	if err != nil {
		return format.Inspection{}, err
	}
	if err := checkInspectMemory(ctx.MemoryLimit(), metadataBytes, uint64(scanWorkspace)); err != nil {
		return format.Inspection{}, err
	}

	first, firstBytes, freeBytes, firstData, err := scanFirstFrame(ctx.Context(), reader, audioStart, audioEnd, initial, initialHeader, metadataBytes, ctx.MemoryLimit(), scanWorkspace)
	if err != nil {
		return format.Inspection{}, err
	}
	maxFrameBytes, bounded := first.MaximumCompatibleFrameSize(freeBytes)
	if !bounded {
		return format.Inspection{}, fmt.Errorf("%w: compatible frame size is unavailable", ErrUnsupported)
	}
	if maxFrameBytes < firstBytes {
		return format.Inspection{}, fmt.Errorf("%w: first frame exceeds its compatible bound", ErrMalformed)
	}

	signal := sample.Signal{Rate: first.SampleRateHz(), Layout: mp3Layout(first.ChannelMode()), ValidBits: 0}
	if !signal.Valid() {
		return format.Inspection{}, fmt.Errorf("%w: first frame signal is invalid", ErrMalformed)
	}
	framingValue := framing{
		version:       uint8(first.Version()),
		layer:         uint8(first.Layer()),
		rate:          first.SampleRateHz(),
		channels:      signal.Layout.Count(),
		freeBytes:     freeBytes,
		maxFrameBytes: maxFrameBytes,
	}
	attachment, err := parseMetadata(ctx.Context(), reader, resolver, extents)
	if err != nil {
		return format.Inspection{}, err
	}

	// scanFirstFrame stores the bytes only during the scan, so indexes are
	// parsed there and reduced to their frame count below.
	frameCount, err := parseFirstIndexes(firstData, first)
	if err != nil {
		return format.Inspection{}, err
	}

	result := inspection{
		sourceEnd:     sourceEnd,
		audioStart:    audioStart,
		audioEnd:      audioEnd,
		first:         first,
		firstBytes:    firstBytes,
		freeBytes:     freeBytes,
		maxFrameBytes: maxFrameBytes,
		signal:        signal,
		metadata:      attachment,
		framing:       framingValue,
	}
	if frameCount != 0 {
		samples := uint64(frameCount) * uint64(first.SamplesPerFrame())
		if samples > math.MaxInt64 {
			return format.Inspection{}, fmt.Errorf("%w: indexed duration overflows the stream time base", ErrUnsupported)
		}
		result.duration = timing.NewDuration(int64(samples))
		result.hasDuration = true
	}
	return format.NewInspection(MP3(), result), nil
}

// inspectReader charges parser reads before calling the borrowed source.
type inspectReader struct {
	reader    access.Random
	remaining uint64
}

func (r *inspectReader) ReadAt(ctx context.Context, destination []byte, offset int64) (int, error) {
	if err := context.Cause(ctx); err != nil {
		return 0, err
	}
	if uint64(len(destination)) > r.remaining {
		return 0, fmt.Errorf("%w: %w (needs %d bytes with %d remaining)", ErrUnsupported, errInspectReadBudget, len(destination), r.remaining)
	}
	r.remaining -= uint64(len(destination))
	if offset < 0 || len(destination) != 0 && offset > math.MaxInt64-int64(len(destination)-1) {
		return 0, access.ErrInvalidRead
	}
	return r.reader.ReadAt(ctx, destination, offset)
}

func wrapInspectRead(kind error, label string, err error) error {
	switch {
	case errors.Is(err, errInspectReadBudget):
		return fmt.Errorf("%w: %s: %w", ErrUnsupported, label, err)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%s: %w", label, err)
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return fmt.Errorf("%w: %s: %w", kind, label, err)
	default:
		return fmt.Errorf("%s: %w", label, err)
	}
}

func discoverExtents(ctx context.Context, reader access.Random, sourceEnd int64) ([]tagExtent, int64, int64, error) {
	var extents []tagExtent
	audioStart := int64(0)
	for audioStart < sourceEnd {
		if sourceEnd-audioStart < 3 {
			break
		}
		var signature [3]byte
		if err := access.ReadFullAt(ctx, reader, signature[:], audioStart); err != nil {
			return nil, 0, 0, wrapInspectRead(ErrMalformed, "leading tag signature", err)
		}
		if !bytes.Equal(signature[:], []byte("ID3")) {
			break
		}
		if sourceEnd-audioStart < 10 {
			return nil, 0, 0, fmt.Errorf("%w: truncated ID3v2 header", ErrMalformed)
		}
		var header [10]byte
		if err := access.ReadFullAt(ctx, reader, header[:], audioStart); err != nil {
			return nil, 0, 0, wrapInspectRead(ErrMalformed, "ID3v2 header", err)
		}
		size, footer, err := parseV2Header(header[:])
		if err != nil {
			return nil, 0, 0, fmt.Errorf("%w: ID3v2 header at %d: %w", ErrMalformed, audioStart, err)
		}
		total := int64(10) + int64(size)
		if footer {
			total += 10
		}
		if total < 10 || total > sourceEnd-audioStart {
			return nil, 0, 0, fmt.Errorf("%w: ID3v2 extent at %d exceeds source", ErrMalformed, audioStart)
		}
		if footer {
			var footerBytes [10]byte
			if err := access.ReadFullAt(ctx, reader, footerBytes[:], audioStart+total-10); err != nil {
				return nil, 0, 0, wrapInspectRead(ErrMalformed, "ID3v2 footer", err)
			}
			if !bytes.Equal(footerBytes[:3], []byte("3DI")) || !bytes.Equal(footerBytes[3:], header[3:]) {
				return nil, 0, 0, fmt.Errorf("%w: ID3v2 footer does not match its header", ErrMalformed)
			}
		}
		extents = append(extents, tagExtent{slot: LeadingID3v2(), offset: audioStart, size: total, mediaType: "application/x-id3v2", block: metadata.BlockID(fmt.Sprintf("id3v2@%d", audioStart))})
		audioStart += total
	}
	audioEnd := sourceEnd
	if sourceEnd >= 128 {
		var signature [3]byte
		if err := access.ReadFullAt(ctx, reader, signature[:], sourceEnd-128); err != nil {
			return nil, 0, 0, wrapInspectRead(ErrMalformed, "trailing tag signature", err)
		}
		if bytes.Equal(signature[:], []byte("TAG")) {
			audioEnd = sourceEnd - 128
			extents = append(extents, tagExtent{slot: TrailingID3v1(), offset: audioEnd, size: 128, mediaType: "application/x-id3v1", block: metadata.BlockID(fmt.Sprintf("id3v1@%d", audioEnd))})
		}
	}
	if audioStart >= audioEnd {
		return nil, 0, 0, fmt.Errorf("%w: tag areas leave no audio frames", ErrMalformed)
	}
	return extents, audioStart, audioEnd, nil
}

func parseV2Header(header []byte) (int, bool, error) {
	if len(header) < 10 || !bytes.Equal(header[:3], []byte("ID3")) {
		return 0, false, errors.New("invalid ID3v2 header")
	}
	version, revision, flags := header[3], header[4], header[5]
	if version < 2 || version > 4 || revision == 0xff {
		return 0, false, errors.New("unsupported ID3v2 version")
	}
	allowed := byte(0xe0)
	if version == 2 {
		allowed = 0xc0
	}
	if version == 4 {
		allowed = 0xf0
	}
	if flags&^allowed != 0 {
		return 0, false, errors.New("invalid ID3v2 flags")
	}
	for _, value := range header[6:10] {
		if value&0x80 != 0 {
			return 0, false, errors.New("invalid ID3v2 syncsafe size")
		}
	}
	size := int(header[6])<<21 | int(header[7])<<14 | int(header[8])<<7 | int(header[9])
	return size, version == 4 && flags&0x10 != 0, nil
}

func readInitialHeader(ctx context.Context, reader access.Random, start, end int64) ([]byte, frame.Header, error) {
	if end-start < 4 {
		return nil, frame.Header{}, fmt.Errorf("%w: audio area is shorter than an MPEG header", ErrMalformed)
	}
	headerBytes := make([]byte, 4)
	if err := access.ReadFullAt(ctx, reader, headerBytes, start); err != nil {
		return nil, frame.Header{}, wrapInspectRead(ErrMalformed, "first MPEG header", err)
	}
	header, _ := frame.Parse(headerBytes)
	return headerBytes, header, nil
}

func scanWorkspaceFor(header frame.Header) int {
	if header.Valid() && !header.FreeFormat() {
		maximum, ok := header.MaximumFrameSize()
		if !ok || maximum > (math.MaxInt-3)/2 {
			return math.MaxInt
		}
		return 2*maximum + 3
	}
	return 4
}

func extentBytes(extents []tagExtent) (uint64, error) {
	var total uint64
	for _, extent := range extents {
		if extent.size < 0 || uint64(extent.size) > math.MaxInt64 {
			return 0, fmt.Errorf("%w: metadata extent size overflows the address space", ErrUnsupported)
		}
		if total > math.MaxUint64-uint64(extent.size) {
			return 0, fmt.Errorf("%w: metadata extent total overflows", ErrUnsupported)
		}
		total += uint64(extent.size)
	}
	return total, nil
}

func checkInspectMemory(limit resource.Bytes, metadataBytes, scanBytes uint64) error {
	if metadataBytes > math.MaxUint64-scanBytes || metadataBytes+scanBytes > uint64(limit) {
		return fmt.Errorf("%w: MP3 inspection needs %d bytes for metadata and scan workspace, limit is %d", ErrUnsupported, metadataBytes+scanBytes, limit)
	}
	return nil
}

func requireTagBindings(ctx context.Context, resolver metadata.Resolver, extents []tagExtent) error {
	for _, extent := range extents {
		if cause := context.Cause(ctx); cause != nil {
			return cause
		}
		if resolver.Valid() && resolver.HasBinding(extent.slot) {
			continue
		}
		probe := resolver
		if !probe.Valid() {
			probe, _ = metadata.NewResolver(nil, nil)
		}
		_, err := probe.Parse(ctx, extent.slot, extent.block, metadata.AssetScope, metadata.NewBlob(extent.mediaType, nil))
		if cause := context.Cause(ctx); cause != nil {
			return cause
		}
		if err == nil {
			return diagnostic.NewError(diagnostic.NewItem("mp3.metadata-binding", diagnostic.ErrorSeverity, diagnostic.Path{Descriptor: metadata.BindingKey(extent.slot).String()}, "MP3 metadata carrier binding is missing", nil))
		}
		return err
	}
	return nil
}

func parseMetadata(ctx context.Context, reader access.Random, resolver metadata.Resolver, extents []tagExtent) (metadata.Attachment, error) {
	if len(extents) == 0 {
		return metadata.Absent(), nil
	}
	attachments := make([]metadata.Attachment, 0, len(extents))
	unavailable := false
	for _, extent := range extents {
		if extent.size > math.MaxInt {
			return metadata.Attachment{}, fmt.Errorf("%w: metadata extent is too large", ErrUnsupported)
		}
		payload := make([]byte, int(extent.size))
		if err := access.ReadFullAt(ctx, reader, payload, extent.offset); err != nil {
			return metadata.Attachment{}, wrapInspectRead(ErrMalformed, fmt.Sprintf("metadata extent at %d", extent.offset), err)
		}
		document, err := resolver.Parse(ctx, extent.slot, extent.block, metadata.AssetScope, metadata.NewBlob(extent.mediaType, payload))
		if cause := context.Cause(ctx); cause != nil {
			return metadata.Attachment{}, cause
		}
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return metadata.Attachment{}, err
			}
			unavailable = true
			continue
		}
		attachment, err := metadata.NewAvailable(document)
		if err != nil {
			return metadata.Attachment{}, err
		}
		attachments = append(attachments, attachment)
	}
	if unavailable {
		return metadata.NewUnavailable(metadata.AssetScope)
	}
	return metadata.Merge(metadata.AssetScope, attachments...)
}

func scanFirstFrame(ctx context.Context, reader access.Random, start, end int64, initial []byte, initialHeader frame.Header, metadataBytes uint64, memoryLimit resource.Bytes, workspace int) (frame.Header, int, int, []byte, error) {
	sourceLength64 := end - start
	if start < 0 || end <= start || sourceLength64 < 0 {
		return frame.Header{}, 0, 0, nil, fmt.Errorf("%w: audio range is invalid", ErrMalformed)
	}
	capacity := len(initial)
	if initialHeader.Valid() && !initialHeader.FreeFormat() {
		capacity = workspace
		if int64(capacity) > sourceLength64 {
			capacity = int(sourceLength64)
		}
	} else if capacity < 4 {
		capacity = 4
	}
	if capacity < len(initial) {
		capacity = len(initial)
	}
	data := make([]byte, len(initial), capacity)
	copy(data, initial)
	for {
		if cause := context.Cause(ctx); cause != nil {
			return frame.Header{}, 0, 0, nil, cause
		}
		final := int64(len(data)) == sourceLength64
		result, found := frame.Scan(data, 0, final)
		if cause := context.Cause(ctx); cause != nil {
			return frame.Header{}, 0, 0, nil, cause
		}
		if found {
			if result.Offset != 0 {
				return frame.Header{}, 0, 0, nil, fmt.Errorf("%w: bytes precede the first MPEG frame", ErrMalformed)
			}
			return result.Header, result.FrameBytes, result.FreeFormatBytes, data[:result.FrameBytes], nil
		}
		if result.Offset > 0 {
			return frame.Header{}, 0, 0, nil, fmt.Errorf("%w: bytes precede the first MPEG frame", ErrMalformed)
		}
		if final {
			return frame.Header{}, 0, 0, nil, fmt.Errorf("%w: no complete MPEG frame found", ErrMalformed)
		}
		next := nextScanCapacity(len(data), cap(data), sourceLength64, initialHeader)
		if next <= len(data) {
			return frame.Header{}, 0, 0, nil, fmt.Errorf("%w: first frame scan cannot grow", ErrUnsupported)
		}
		limit := uint64(memoryLimit)
		nextBytes := uint64(next)
		if nextBytes > limit || metadataBytes > limit-nextBytes {
			return frame.Header{}, 0, 0, nil, fmt.Errorf("%w: MP3 frame scan exceeds InspectMemory", ErrUnsupported)
		}
		oldLength := len(data)
		if next > cap(data) {
			currentBytes := uint64(cap(data))
			if currentBytes > limit-nextBytes-metadataBytes {
				return frame.Header{}, 0, 0, nil, fmt.Errorf("%w: MP3 frame scan growth exceeds InspectMemory", ErrUnsupported)
			}
			grown := make([]byte, next)
			copy(grown, data)
			data = grown
		} else {
			data = data[:next]
		}
		if start > math.MaxInt64-int64(oldLength) {
			return frame.Header{}, 0, 0, nil, fmt.Errorf("%w: frame scan offset overflows", ErrUnsupported)
		}
		if err := access.ReadFullAt(ctx, reader, data[oldLength:], start+int64(oldLength)); err != nil {
			return frame.Header{}, 0, 0, nil, wrapInspectRead(ErrMalformed, "frame scan", err)
		}
	}
}

func nextScanCapacity(length, capacity int, sourceLength int64, initial frame.Header) int {
	if initial.Valid() && !initial.FreeFormat() {
		if sourceLength < int64(capacity) {
			return int(sourceLength)
		}
		return capacity
	}
	next := capacity
	if capacity > (math.MaxInt-4)/2 {
		next = math.MaxInt
	} else {
		next = capacity * 2
	}
	if next != math.MaxInt && next < capacity+4 {
		next = capacity + 4
	}
	if int64(next) > sourceLength {
		if sourceLength <= int64(math.MaxInt) {
			next = int(sourceLength)
		}
	}
	return next
}

func parseFirstIndexes(data []byte, header frame.Header) (uint32, error) {
	xing, xingFound, err := frame.ParseXing(header, data)
	if err != nil {
		return 0, fmt.Errorf("%w: Xing index: %w", ErrMalformed, err)
	}
	vbri, vbriFound, err := frame.ParseVBRI(header, data)
	if err != nil {
		return 0, fmt.Errorf("%w: VBRI index: %w", ErrMalformed, err)
	}
	var count uint32
	if xingFound && xing.HasFrames() && xing.Frames() != 0 {
		count = xing.Frames()
	}
	if vbriFound && vbri.Frames() != 0 {
		if count != 0 && count != vbri.Frames() {
			return 0, fmt.Errorf("%w: Xing and VBRI frame counts disagree", ErrMalformed)
		}
		count = vbri.Frames()
	}
	return count, nil
}

func mp3Layout(mode frame.ChannelMode) sample.Layout {
	if mode == frame.Mono {
		return sample.Mono()
	}
	return sample.Channels(2)
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
