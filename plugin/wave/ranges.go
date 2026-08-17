package wave

import "math"

// sourceRange identifies bytes that belong to the inspected source. It is a
// descriptor only: the source opening is borrowed by the operator at Open and
// never crosses the Compile/Inspection boundary.
type sourceRange struct {
	offset uint64
	length uint64
}

func (r sourceRange) valid() bool { return r.length != 0 }

func (r sourceRange) end() (uint64, bool) {
	if r.offset > math.MaxUint64-r.length {
		return 0, false
	}
	return r.offset + r.length, true
}

// sourceRanges deliberately has one slot per WAVE layout anchor. Unknown
// chunks in one anchor are recorded as one contiguous source range instead of
// one value per chunk. The source scan only produces ranges for bytes that are
// actually preserved; known fmt/data/ds64 records are not included.
type sourceRanges struct {
	reservation  sourceRange
	beforeFormat sourceRange
	beforeData   sourceRange
	afterData    sourceRange
	trailer      sourceRange
	info         sourceRange
	infoCount    uint8
}

func (r sourceRanges) any() bool {
	return r.reservation.valid() || r.beforeFormat.valid() || r.beforeData.valid() ||
		r.afterData.valid() || r.trailer.valid()
}

func (r *sourceRanges) add(anchor chunkAnchor, value sourceRange) error {
	if !value.valid() {
		return nil
	}
	var target *sourceRange
	switch anchor {
	case chunkReservation:
		target = &r.reservation
	case chunkBeforeFormat:
		target = &r.beforeFormat
	case chunkBeforeData:
		target = &r.beforeData
	case chunkAfterData:
		target = &r.afterData
	case chunkAfterRIFF:
		target = &r.trailer
	default:
		return ErrMalformed
	}
	if target.valid() {
		end, ok := target.end()
		if !ok || end != value.offset || target.length > math.MaxUint64-value.length {
			return ErrUnsupported
		}
		target.length += value.length
		return nil
	}
	*target = value
	return nil
}

func (r sourceRanges) rangeFor(anchor chunkAnchor) sourceRange {
	switch anchor {
	case chunkReservation:
		return r.reservation
	case chunkBeforeFormat:
		return r.beforeFormat
	case chunkBeforeData:
		return r.beforeData
	case chunkAfterData:
		return r.afterData
	case chunkAfterRIFF:
		return r.trailer
	default:
		return sourceRange{}
	}
}
