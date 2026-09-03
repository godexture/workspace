package mp4

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/resource"
)

// muxLayout is the byte plan of one remux. A full and a partial track selection
// share it: the mdat payload is always rebuilt from the arriving packets, every
// other box is either copied verbatim or resized in place, and the fields that
// record a byte offset or a derived duration are patched afterwards.
type muxLayout struct {
	pieces []muxPiece
	tracks []muxTrack
	// payload indexes the rebuilt mdat payload within pieces.
	payload int
	size    uint64
	// journal is the total size of the chunk-offset journal, which is the sum
	// of the per-track regions placeJournal hands out.
	journal uint64
	// verbatim marks a layout that must reproduce the source byte for byte. It
	// is set for a movie that records byte offsets outside the sample tables,
	// where the only safe remux is one that moves nothing.
	verbatim bool
	// duration rewrites the mvhd duration when the selection drops a track.
	duration muxPatch
}

// muxTrack is one selected track, in output order.
type muxTrack struct {
	source int
	value  track
	// offsets is the output offset of the first chunk-offset table entry.
	offsets uint64
	// journal is where this track's region of the chunk-offset journal starts.
	// Tracks record their chunks as the interleaved payload reaches them, so
	// each one writes into its own region rather than appending in turn.
	journal uint64
}

// muxPatch rewrites one fixed-width big-endian field in the finished output.
// A zero width means the field keeps its inspected bytes.
type muxPatch struct {
	offset uint64
	value  uint64
	width  uint64
}

type muxPieceKind uint8

const (
	// muxCopy reproduces a source range byte for byte.
	muxCopy muxPieceKind = iota
	// muxHeader writes a box header whose size changed.
	muxHeader
	// muxPayload is the mdat payload, written from the arriving packets.
	muxPayload
)

type muxPiece struct {
	kind   muxPieceKind
	output uint64
	size   uint64
	source uint64
	header [16]byte
}

func (l muxLayout) valid() bool {
	return len(l.tracks) != 0 && l.payload > 0 && l.payload < len(l.pieces) && l.pieces[l.payload].kind == muxPayload
}

func (l muxLayout) prefix() []muxPiece { return l.pieces[:l.payload] }

func (l muxLayout) suffix() []muxPiece { return l.pieces[l.payload+1:] }

func (l muxLayout) payloadOffset() uint64 { return l.pieces[l.payload].output }

func (l muxLayout) payloadSize() uint64 { return l.pieces[l.payload].size }

// journalBytes is the chunk-offset journal this layout needs while it writes.
// Each chunk contributes one 64-bit output offset.
func (l muxLayout) journalBytes() resource.Bytes { return resource.Bytes(l.journal) }

// compileMux resolves the packet inputs against the inspection and plans the
// output bytes. Inputs name inspected tracks in inspection order; a shorter
// input list keeps that subset and rebuilds the movie around it.
func compileMux(inputs []stream.Descriptor, inspected movie) (muxLayout, error) {
	selected, err := selectMuxTracks(inputs, inspected)
	if err != nil {
		return muxLayout{}, err
	}
	return buildMuxLayout(inspected, selected)
}

func selectMuxTracks(inputs []stream.Descriptor, inspected movie) ([]int, error) {
	if len(inputs) == 0 || len(inputs) > len(inspected.tracks) {
		return nil, fmt.Errorf("%w: MP4 mux takes 1 to %d packet inputs, got %d", ErrUnsupported, len(inspected.tracks), len(inputs))
	}
	index := make(map[stream.ID]int, len(inspected.tracks))
	for position, value := range inspected.tracks {
		index[trackStreamID(value.id)] = position
	}
	result := make([]int, 0, len(inputs))
	previous := -1
	for route, input := range inputs {
		position, ok := index[input.ID()]
		if !ok {
			return nil, fmt.Errorf("%w: packet input %d stream %q is absent from the MP4 inspection", ErrUnsupported, route, input.ID())
		}
		if position <= previous {
			return nil, fmt.Errorf("%w: MP4 mux keeps inspected tracks in order and cannot repeat one", ErrUnsupported)
		}
		previous = position
		if err := validateMuxInput(input, inspected.tracks[position], inspected.metadata, route); err != nil {
			return nil, err
		}
		result = append(result, position)
	}
	return result, nil
}

func validateMuxInput(input stream.Descriptor, value track, expected metadata.Document, route int) error {
	if !input.Valid() || !input.SchemaDescriptor().Equal(codec.Packets().Descriptor()) || input.ID() != trackStreamID(value.id) || input.TimeBase() != timing.MustBase(1, int64(value.timeScale)) {
		return fmt.Errorf("%w: packet input %d does not match inspected track %d", ErrUnsupported, route, value.id)
	}
	tag, ok := codec.TagOf(input.Properties())
	if !ok || tag != SampleEntryTag(string(value.codec[:])) {
		return fmt.Errorf("%w: packet input %d changes track %d sample entry", ErrUnsupported, route, value.id)
	}
	if !sameIlstMuxDocument(input.Metadata(), expected) {
		return fmt.Errorf("%w: packet input %d changes MP4 metadata", ErrUnsupported, route)
	}
	return nil
}

func buildMuxLayout(value movie, selected []int) (muxLayout, error) {
	if err := validateMuxMovie(value); err != nil {
		return muxLayout{}, err
	}
	if err := validateMuxRanges(value); err != nil {
		return muxLayout{}, err
	}
	builder, err := newMuxLayoutBuilder(value, selected)
	if err != nil {
		return muxLayout{}, err
	}
	return builder.build()
}

// validateMuxRanges checks the source geometry this layout relies on: mvhd and
// every trak sit inside moov in ascending, non-overlapping order, and each
// chunk-offset table sits inside its own trak.
func validateMuxRanges(value movie) error {
	moovEnd, moovOK := payloadEnd(value.moov)
	headEnd, headOK := checkedBoxAdd(value.header.box.offset, value.header.box.size)
	if !moovOK || !headOK || value.header.box.offset < value.moov.payloadOffset || headEnd > moovEnd {
		return fmt.Errorf("%w: MP4 mvhd lies outside moov", ErrMalformed)
	}
	cursor := value.moov.payloadOffset
	for _, current := range value.tracks {
		end, ok := checkedBoxAdd(current.trak.offset, current.trak.size)
		if !ok || current.trak.offset < cursor || end > moovEnd {
			return fmt.Errorf("%w: MP4 track %d lies outside moov or overlaps another track", ErrMalformed, current.id)
		}
		table := current.tables.offsets
		tableEnd, tableOK := payloadEnd(table)
		entries, entriesOK := chunkTableBytes(current)
		if !tableOK || !entriesOK || table.offset < current.trak.offset || tableEnd > end || table.payloadSize < 8 || entries > table.payloadSize-8 {
			return fmt.Errorf("%w: MP4 track %d chunk-offset table lies outside its trak", ErrMalformed, current.id)
		}
		cursor = end
	}
	return nil
}

// chunkTableBytes is the encoded size of one track's chunk-offset entries.
func chunkTableBytes(value track) (uint64, bool) {
	width := uint64(4)
	if value.tables.largeOffsets {
		width = 8
	}
	count := uint64(value.chunkCount)
	if count > math.MaxUint64/width {
		return 0, false
	}
	return count * width, true
}

type muxLayoutBuilder struct {
	movie movie
	// order lists the selected track indexes in output order, and kept answers
	// the same question by inspection index.
	order []int
	kept  []bool

	moovSize    uint64
	moovHeader  uint64
	mediaSize   uint64
	mediaHeader uint64
	// payloadBytes is the rebuilt mdat payload: the selected sample bytes.
	payloadBytes uint64
	duration     muxPatch

	pieces  []muxPiece
	tracks  []muxTrack
	cursor  uint64
	payload int
}

func newMuxLayoutBuilder(value movie, selected []int) (*muxLayoutBuilder, error) {
	result := &muxLayoutBuilder{movie: value, order: selected, kept: make([]bool, len(value.tracks)), payload: -1}
	for _, index := range selected {
		if index < 0 || index >= len(value.tracks) || result.kept[index] {
			return nil, fmt.Errorf("%w: MP4 track selection %v is invalid", ErrMalformed, selected)
		}
		result.kept[index] = true
	}
	var removed uint64
	for index, current := range value.tracks {
		var ok bool
		if !result.kept[index] {
			if removed, ok = checkedBoxAdd(removed, current.trak.size); !ok {
				return nil, fmt.Errorf("%w: MP4 dropped track bytes overflow", ErrUnsupported)
			}
			continue
		}
		if result.payloadBytes, ok = checkedBoxAdd(result.payloadBytes, current.sampleBytes); !ok {
			return nil, fmt.Errorf("%w: MP4 selected sample bytes overflow", ErrUnsupported)
		}
	}
	if removed > value.moov.payloadSize {
		return nil, fmt.Errorf("%w: MP4 dropped track bytes exceed moov", ErrMalformed)
	}
	if err := result.planSelection(); err != nil {
		return nil, err
	}
	if err := result.planSizes(value.moov.payloadSize - removed); err != nil {
		return nil, err
	}
	return result, nil
}

// planSelection checks what a partial selection cannot preserve and derives the
// shortened movie duration. A full selection keeps the inspected mvhd bytes.
func (b *muxLayoutBuilder) planSelection() error {
	if len(b.order) == len(b.movie.tracks) {
		return nil
	}
	var longest uint64
	for _, index := range b.order {
		current := b.movie.tracks[index]
		if current.references {
			return fmt.Errorf("%w: MP4 track %d declares track references a subset cannot verify", ErrUnsupported, current.id)
		}
		if current.movieDuration.unknown() {
			return fmt.Errorf("%w: MP4 track %d has an unknown duration and cannot shorten the movie", ErrUnsupported, current.id)
		}
		longest = max(longest, current.movieDuration.value)
	}
	if longest == b.movie.header.duration.value {
		return nil
	}
	if !b.movie.header.duration.fits(longest) {
		return fmt.Errorf("%w: MP4 movie duration %d does not fit the inspected mvhd field", ErrUnsupported, longest)
	}
	b.duration = muxPatch{value: longest, width: b.movie.header.duration.width()}
	return nil
}

func (b *muxLayoutBuilder) planSizes(moovPayload uint64) error {
	var ok bool
	if b.moovSize, b.moovHeader, ok = boxSize(b.movie.moov, moovPayload); !ok {
		return fmt.Errorf("%w: MP4 output moov size overflows", ErrUnsupported)
	}
	if b.mediaSize, b.mediaHeader, ok = boxSize(b.movie.media, b.payloadBytes); !ok {
		return fmt.Errorf("%w: MP4 output mdat size overflows", ErrUnsupported)
	}
	return nil
}

// boxSize keeps the inspected header when the payload is unchanged, so a full
// selection stays byte exact, and compacts the header otherwise.
func boxSize(value box, payload uint64) (size, headerSize uint64, ok bool) {
	if payload == value.payloadSize {
		return value.size, value.headerSize, true
	}
	if headerSize, ok = compactBoxHeader(payload); !ok {
		return 0, 0, false
	}
	size, ok = checkedBoxAdd(headerSize, payload)
	return size, headerSize, ok
}

func compactBoxHeader(payload uint64) (uint64, bool) {
	if payload <= math.MaxUint32-8 {
		return 8, true
	}
	if payload <= math.MaxUint64-16 {
		return 16, true
	}
	return 0, false
}

func (b *muxLayoutBuilder) build() (muxLayout, error) {
	first, second := b.movie.moov, b.movie.media
	if b.movie.media.offset < b.movie.moov.offset {
		first, second = b.movie.media, b.movie.moov
	}
	firstEnd, firstOK := checkedBoxAdd(first.offset, first.size)
	secondEnd, secondOK := checkedBoxAdd(second.offset, second.size)
	if !firstOK || !secondOK || firstEnd > second.offset || secondEnd > b.movie.sourceEnd {
		return muxLayout{}, fmt.Errorf("%w: MP4 moov and mdat ranges overlap", ErrMalformed)
	}
	if err := b.copy(0, first.offset); err != nil {
		return muxLayout{}, err
	}
	if err := b.writeBox(first); err != nil {
		return muxLayout{}, err
	}
	if err := b.copy(firstEnd, second.offset); err != nil {
		return muxLayout{}, err
	}
	if err := b.writeBox(second); err != nil {
		return muxLayout{}, err
	}
	if err := b.copy(secondEnd, b.movie.sourceEnd); err != nil {
		return muxLayout{}, err
	}
	result := muxLayout{pieces: b.pieces, tracks: b.tracks, payload: b.payload, size: b.cursor}
	if !result.valid() || len(result.tracks) != len(b.order) {
		return muxLayout{}, fmt.Errorf("%w: MP4 output layout is incomplete", ErrMalformed)
	}
	if err := result.placeJournal(); err != nil {
		return muxLayout{}, err
	}
	if err := result.checkOffsetWidth(); err != nil {
		return muxLayout{}, err
	}
	if err := b.locateDuration(&result); err != nil {
		return muxLayout{}, err
	}
	if b.movie.offsetIndex && !result.reproduces(b.movie) {
		return muxLayout{}, fmt.Errorf("%w: MP4 records byte offsets outside the sample tables, and this selection does not reproduce the source", ErrUnsupported)
	}
	result.verbatim = b.movie.offsetIndex
	return result, nil
}

// reproduces reports whether this layout puts every byte of the output where
// the source had it. A movie that records byte offsets outside the sample
// tables -- sidx, iloc, tfra -- stays consistent only under such a layout, so
// it is the one selection worth accepting rather than rejecting the movie
// outright. The muxer still checks each sample lands where it was read from,
// because arrival order is what decides that and this only decides position.
func (l muxLayout) reproduces(value movie) bool {
	return len(l.tracks) == len(value.tracks) &&
		l.duration.width == 0 &&
		l.size == value.sourceEnd &&
		l.payloadOffset() == value.media.payloadOffset &&
		l.payloadSize() == value.media.payloadSize
}

// placeJournal gives each track a region of the chunk-offset journal, in track
// order, so patching can read one track's entries as a run.
func (l *muxLayout) placeJournal() error {
	l.journal = 0
	for index := range l.tracks {
		l.tracks[index].journal = l.journal
		chunks := uint64(l.tracks[index].value.chunkCount)
		if chunks > (uint64(math.MaxInt64)-l.journal)/8 {
			return fmt.Errorf("%w: MP4 chunk-offset journal exceeds runtime range", ErrUnsupported)
		}
		l.journal += chunks * 8
	}
	return nil
}

// checkOffsetWidth rejects a movie whose rebuilt payload could push a 32-bit
// stco entry past its field, while the layout is still the only thing that has
// been decided. Every chunk lands inside the payload, so the payload end is the
// bound. Deciding it per entry would mean rewriting the whole mdat first and
// failing on the patch.
func (l muxLayout) checkOffsetWidth() error {
	end, ok := checkedBoxAdd(l.payloadOffset(), l.payloadSize())
	if !ok {
		return fmt.Errorf("%w: MP4 output payload range overflows", ErrUnsupported)
	}
	for _, selected := range l.tracks {
		if selected.value.tables.largeOffsets || selected.value.chunkCount == 0 {
			continue
		}
		if end > math.MaxUint32 {
			return fmt.Errorf("%w: MP4 track %d keeps 32-bit chunk offsets but the output payload ends at %d", ErrUnsupported, selected.value.id, end)
		}
	}
	return nil
}

func (b *muxLayoutBuilder) writeBox(value box) error {
	if value.typeID == typeMOOV {
		return b.writeMoov()
	}
	return b.writeMedia()
}

func (b *muxLayoutBuilder) writeMoov() error {
	source := b.movie.moov
	moovEnd, ok := payloadEnd(source)
	boxEnd, boxOK := checkedBoxAdd(source.offset, source.size)
	if !ok || !boxOK {
		return fmt.Errorf("%w: MP4 moov range overflows", ErrMalformed)
	}
	if b.moovSize == source.size {
		if err := b.copy(source.offset, boxEnd); err != nil {
			return err
		}
		return b.recordTracks(b.last())
	}
	if err := b.boxHeader(typeMOOV, b.moovSize, b.moovHeader); err != nil {
		return err
	}
	cursor := source.payloadOffset
	for index, current := range b.movie.tracks {
		trackEnd, ok := checkedBoxAdd(current.trak.offset, current.trak.size)
		if !ok {
			return fmt.Errorf("%w: MP4 track %d range overflows", ErrMalformed, current.id)
		}
		if err := b.copy(cursor, current.trak.offset); err != nil {
			return err
		}
		cursor = trackEnd
		if !b.kept[index] {
			continue
		}
		if err := b.copy(current.trak.offset, trackEnd); err != nil {
			return err
		}
		if err := b.recordTrack(index, b.last()); err != nil {
			return err
		}
	}
	return b.copy(cursor, moovEnd)
}

func (b *muxLayoutBuilder) writeMedia() error {
	source := b.movie.media
	if b.mediaSize == source.size {
		if err := b.copy(source.offset, source.payloadOffset); err != nil {
			return err
		}
	} else if err := b.boxHeader(typeMDAT, b.mediaSize, b.mediaHeader); err != nil {
		return err
	}
	if b.payload >= 0 {
		return fmt.Errorf("%w: MP4 output layout has two payloads", ErrMalformed)
	}
	b.payload = len(b.pieces)
	return b.add(muxPiece{kind: muxPayload, size: b.payloadBytes})
}

func (b *muxLayoutBuilder) recordTracks(piece muxPiece) error {
	for _, index := range b.order {
		if err := b.recordTrack(index, piece); err != nil {
			return err
		}
	}
	return nil
}

// recordTrack notes where the copied trak lands, so the chunk-offset table can
// be patched once the payload offsets are known.
func (b *muxLayoutBuilder) recordTrack(index int, piece muxPiece) error {
	current := b.movie.tracks[index]
	table, ok := checkedBoxAdd(current.tables.offsets.payloadOffset, 8)
	entries, entriesOK := chunkTableBytes(current)
	if !ok || !entriesOK {
		return fmt.Errorf("%w: MP4 track %d chunk-offset table overflows", ErrMalformed, current.id)
	}
	offsets, ok := locateIn(piece, table, entries)
	if !ok {
		return fmt.Errorf("%w: MP4 track %d chunk-offset table is not preserved", ErrMalformed, current.id)
	}
	b.tracks = append(b.tracks, muxTrack{source: index, value: current, offsets: offsets})
	return nil
}

// locateDuration maps the mvhd duration field onto the output. The field always
// lands in a copied span: a resized moov reproduces every box except the traks
// this selection drops.
func (b *muxLayoutBuilder) locateDuration(result *muxLayout) error {
	if b.duration.width == 0 {
		return nil
	}
	field := b.movie.header.duration
	for _, piece := range result.pieces {
		offset, ok := locateIn(piece, field.offset, field.width())
		if !ok {
			continue
		}
		result.duration = muxPatch{offset: offset, value: b.duration.value, width: b.duration.width}
		return nil
	}
	return fmt.Errorf("%w: MP4 mvhd duration is not preserved", ErrMalformed)
}

func (b *muxLayoutBuilder) copy(start, end uint64) error {
	if start > end || end > b.movie.sourceEnd {
		return fmt.Errorf("%w: MP4 preserved range %d..%d is invalid", ErrMalformed, start, end)
	}
	if start == end {
		return nil
	}
	return b.add(muxPiece{kind: muxCopy, size: end - start, source: start})
}

func (b *muxLayoutBuilder) boxHeader(typeID boxType, size, headerSize uint64) error {
	piece := muxPiece{kind: muxHeader, size: headerSize}
	switch {
	case headerSize == 8 && size <= math.MaxUint32:
		binary.BigEndian.PutUint32(piece.header[:4], uint32(size))
		copy(piece.header[4:8], typeID[:])
	case headerSize == 16:
		binary.BigEndian.PutUint32(piece.header[:4], 1)
		copy(piece.header[4:8], typeID[:])
		binary.BigEndian.PutUint64(piece.header[8:16], size)
	default:
		return fmt.Errorf("%w: MP4 %s size %d does not fit a %d byte header", ErrMalformed, string(typeID[:]), size, headerSize)
	}
	return b.add(piece)
}

func (b *muxLayoutBuilder) add(piece muxPiece) error {
	next, ok := checkedBoxAdd(b.cursor, piece.size)
	if !ok || next > math.MaxInt64 {
		return fmt.Errorf("%w: MP4 output exceeds runtime offsets", ErrUnsupported)
	}
	piece.output = b.cursor
	b.cursor = next
	b.pieces = append(b.pieces, piece)
	return nil
}

func (b *muxLayoutBuilder) last() muxPiece { return b.pieces[len(b.pieces)-1] }

// locateIn maps a source range onto the output within one copied piece. An
// empty range at the very end of the piece still resolves: a table with no
// entries has nothing to patch but a valid location.
func locateIn(piece muxPiece, source, size uint64) (uint64, bool) {
	if piece.kind != muxCopy || source < piece.source {
		return 0, false
	}
	offset := source - piece.source
	if end, ok := checkedBoxAdd(offset, size); !ok || end > piece.size {
		return 0, false
	}
	return piece.output + offset, true
}
