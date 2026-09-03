package mp4

import (
	"fmt"
	"math"
	"sort"

	"github.com/godexture/godec/media/metadata"
)

// muxBoxRewrite is the output geometry for one box whose payload changed.
// Keeping the original header width is important for large-size source boxes:
// shrinking one must not silently change the source representation.
type muxBoxRewrite struct {
	size       uint64
	headerSize uint64
}

type muxIlstRewrite struct {
	active   bool
	envelope ilstEnvelope
	payload  metadata.Blob
	ilst     muxBoxRewrite
	meta     muxBoxRewrite
	udta     muxBoxRewrite
}

func newIlstRewrite(envelope ilstEnvelope, payload metadata.Blob) (muxIlstRewrite, error) {
	if !envelope.valid() || !payload.Valid() {
		return muxIlstRewrite{}, fmt.Errorf("%w: MP4 ilst rewrite has no valid envelope or payload", ErrMalformed)
	}
	if err := validateIlstRewriteEnvelope(envelope); err != nil {
		return muxIlstRewrite{}, err
	}
	var ok bool
	result := muxIlstRewrite{active: true, envelope: envelope, payload: payload}
	if result.ilst.size, result.ilst.headerSize, ok = resizedBox(envelope.ilst, uint64(payload.Len())); !ok {
		return muxIlstRewrite{}, fmt.Errorf("%w: MP4 rewritten ilst size overflows", ErrUnsupported)
	}
	metaPayload, ok := replaceBoxChildSize(envelope.meta.payloadSize, envelope.ilst.size, result.ilst.size)
	if !ok {
		return muxIlstRewrite{}, fmt.Errorf("%w: MP4 ilst does not fit in meta", ErrMalformed)
	}
	if result.meta.size, result.meta.headerSize, ok = resizedBox(envelope.meta, metaPayload); !ok {
		return muxIlstRewrite{}, fmt.Errorf("%w: MP4 rewritten meta size overflows", ErrUnsupported)
	}
	udtaPayload, ok := replaceBoxChildSize(envelope.udta.payloadSize, envelope.meta.size, result.meta.size)
	if !ok {
		return muxIlstRewrite{}, fmt.Errorf("%w: MP4 meta does not fit in udta", ErrMalformed)
	}
	if result.udta.size, result.udta.headerSize, ok = resizedBox(envelope.udta, udtaPayload); !ok {
		return muxIlstRewrite{}, fmt.Errorf("%w: rewritten udta size overflows", ErrUnsupported)
	}
	return result, nil
}

func validateIlstRewriteEnvelope(value ilstEnvelope) error {
	if !value.valid() {
		return fmt.Errorf("%w: MP4 ilst rewrite envelope is incomplete", ErrMalformed)
	}
	for _, current := range []box{value.udta, value.meta, value.ilst} {
		if current.openEnded {
			return fmt.Errorf("%w: MP4 ilst rewrite cannot resize an open-ended box", ErrUnsupported)
		}
		if current.headerSize != 8 && current.headerSize != 16 {
			return fmt.Errorf("%w: MP4 ilst rewrite has an unsupported box header", ErrMalformed)
		}
		end, ok := checkedBoxAdd(current.offset, current.size)
		expectedSize, sizeOK := checkedBoxAdd(current.headerSize, current.payloadSize)
		payloadEndValue, payloadOK := payloadEnd(current)
		if !ok || !sizeOK || !payloadOK || current.size != expectedSize || end != payloadEndValue {
			return fmt.Errorf("%w: MP4 ilst rewrite box range is invalid", ErrMalformed)
		}
	}
	udtaEnd, udtaOK := checkedBoxAdd(value.udta.offset, value.udta.size)
	metaEnd, metaOK := checkedBoxAdd(value.meta.offset, value.meta.size)
	ilstEnd, ilstOK := checkedBoxAdd(value.ilst.offset, value.ilst.size)
	if !udtaOK || !metaOK || !ilstOK || value.meta.offset < value.udta.payloadOffset || metaEnd > udtaEnd || value.ilst.offset < value.meta.payloadOffset || ilstEnd > metaEnd {
		return fmt.Errorf("%w: MP4 ilst rewrite envelope is not nested", ErrMalformed)
	}
	return nil
}

func replaceBoxChildSize(parent, oldChild, newChild uint64) (uint64, bool) {
	if oldChild > parent {
		return 0, false
	}
	return checkedBoxAdd(parent-oldChild, newChild)
}

// resizedBox keeps a source large-size header even if the rewritten payload
// becomes smaller. A normal header expands to a large-size header only when
// the new total cannot fit in its original 32-bit size field.
func resizedBox(value box, payload uint64) (size, headerSize uint64, ok bool) {
	if value.headerSize != 8 && value.headerSize != 16 {
		return 0, 0, false
	}
	if payload == value.payloadSize {
		return value.size, value.headerSize, true
	}
	headerSize = value.headerSize
	if headerSize == 8 && payload > math.MaxUint32-8 {
		headerSize = 16
	}
	size, ok = checkedBoxAdd(headerSize, payload)
	return size, headerSize, ok
}

func (b *muxLayoutBuilder) writeMoovWithIlst() error {
	source := b.movie.moov
	rewrite := b.metadataPlan.rewrite
	moovEnd, ok := payloadEnd(source)
	if !ok {
		return fmt.Errorf("%w: MP4 moov payload range overflows", ErrMalformed)
	}
	udtaEnd, udtaOK := checkedBoxAdd(rewrite.envelope.udta.offset, rewrite.envelope.udta.size)
	if !udtaOK || rewrite.envelope.udta.offset < source.payloadOffset || udtaEnd > moovEnd {
		return fmt.Errorf("%w: MP4 ilst udta lies outside moov", ErrMalformed)
	}
	if source.openEnded && b.moovSize == source.size && b.moovHeader == source.headerSize {
		if err := b.openEndedHeader(typeMOOV); err != nil {
			return err
		}
	} else if err := b.boxHeader(typeMOOV, b.moovSize, b.moovHeader); err != nil {
		return err
	}

	type event struct {
		start uint64
		end   uint64
		track int
		udta  bool
	}
	events := make([]event, 0, len(b.movie.tracks)+1)
	events = append(events, event{start: rewrite.envelope.udta.offset, end: udtaEnd, track: -1, udta: true})
	for index, current := range b.movie.tracks {
		end, ok := checkedBoxAdd(current.trak.offset, current.trak.size)
		if !ok {
			return fmt.Errorf("%w: MP4 track %d range overflows", ErrMalformed, current.id)
		}
		events = append(events, event{start: current.trak.offset, end: end, track: index})
	}
	sort.Slice(events, func(left, right int) bool {
		if events[left].start != events[right].start {
			return events[left].start < events[right].start
		}
		return events[left].end < events[right].end
	})
	cursor := source.payloadOffset
	for _, current := range events {
		if current.start < cursor || current.end > moovEnd {
			return fmt.Errorf("%w: MP4 ilst rewrite overlaps moov children", ErrMalformed)
		}
		if err := b.copy(cursor, current.start); err != nil {
			return err
		}
		if current.udta {
			if err := b.writeIlstUdta(); err != nil {
				return err
			}
		} else if b.kept[current.track] {
			if err := b.copy(current.start, current.end); err != nil {
				return err
			}
			if err := b.recordTrack(current.track, b.last()); err != nil {
				return err
			}
		}
		cursor = current.end
	}
	return b.copy(cursor, moovEnd)
}

func (b *muxLayoutBuilder) writeIlstUdta() error {
	rewrite := b.metadataPlan.rewrite
	udtaEnd, udtaOK := checkedBoxAdd(rewrite.envelope.udta.offset, rewrite.envelope.udta.size)
	metaEnd, metaOK := checkedBoxAdd(rewrite.envelope.meta.offset, rewrite.envelope.meta.size)
	if !udtaOK || !metaOK || rewrite.envelope.meta.offset < rewrite.envelope.udta.payloadOffset || metaEnd > udtaEnd {
		return fmt.Errorf("%w: MP4 meta lies outside rewritten udta", ErrMalformed)
	}
	if err := b.boxHeader(typeUDTA, rewrite.udta.size, rewrite.udta.headerSize); err != nil {
		return err
	}
	if err := b.copy(rewrite.envelope.udta.payloadOffset, rewrite.envelope.meta.offset); err != nil {
		return err
	}
	if err := b.writeIlstMeta(); err != nil {
		return err
	}
	return b.copy(metaEnd, udtaEnd)
}

func (b *muxLayoutBuilder) writeIlstMeta() error {
	rewrite := b.metadataPlan.rewrite
	metaEnd, metaOK := checkedBoxAdd(rewrite.envelope.meta.offset, rewrite.envelope.meta.size)
	ilstEnd, ilstOK := checkedBoxAdd(rewrite.envelope.ilst.offset, rewrite.envelope.ilst.size)
	if !metaOK || !ilstOK || rewrite.envelope.ilst.offset < rewrite.envelope.meta.payloadOffset || ilstEnd > metaEnd {
		return fmt.Errorf("%w: MP4 ilst lies outside rewritten meta", ErrMalformed)
	}
	if err := b.boxHeader(typeMETA, rewrite.meta.size, rewrite.meta.headerSize); err != nil {
		return err
	}
	if err := b.copy(rewrite.envelope.meta.payloadOffset, rewrite.envelope.ilst.offset); err != nil {
		return err
	}
	if err := b.writeIlstBox(); err != nil {
		return err
	}
	return b.copy(ilstEnd, metaEnd)
}

func (b *muxLayoutBuilder) writeIlstBox() error {
	rewrite := b.metadataPlan.rewrite
	if err := b.boxHeader(typeILST, rewrite.ilst.size, rewrite.ilst.headerSize); err != nil {
		return err
	}
	return b.add(muxPiece{kind: muxBlob, size: uint64(rewrite.payload.Len()), blob: rewrite.payload})
}
