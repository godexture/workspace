package frame

// Result describes the first complete frame found in a byte buffer.
//
// Offset is the byte position of Header. FrameBytes includes this frame's
// padding. FreeFormatBytes is non-zero only for free-format frames and does
// not include padding. For a false result, Offset is the safe discard prefix;
// it is len(data) at EOF and retains an incomplete candidate or the final
// three bytes for a non-final chunk. NeedMore reports that more bytes can
// change the result.
type Result struct {
	Offset          int
	FrameBytes      int
	FreeFormatBytes int
	Header          Header
	NeedMore        bool
}

// Scan finds the first frame that is complete and has enough following
// structure to distinguish it from a false sync. A positive hint supplies the
// unpadded size of a free-format frame; zero asks Scan to resolve that size
// from repeated compatible headers. When final is false, a complete frame at
// the current buffer end remains inconclusive until another chunk arrives.
// Scan does not limit retained data; callers must bound it by their resource
// policy.
func Scan(data []byte, freeFormatHint int, final bool) (Result, bool) {
	if freeFormatHint < 0 {
		freeFormatHint = 0
	}

	for offset := 0; offset+4 <= len(data); offset++ {
		header, err := Parse(data[offset:])
		if err != nil {
			continue
		}

		if header.FreeFormat() {
			frameBytes, freeBytes, state := resolveFreeFormat(data, offset, header, freeFormatHint, final)
			switch state {
			case candidateFound:
				return Result{
					Offset:          offset,
					FrameBytes:      frameBytes,
					FreeFormatBytes: freeBytes,
					Header:          header,
				}, true
			case candidateIncomplete:
				return Result{Offset: offset, NeedMore: true}, false
			}
			continue
		}

		frameBytes, err := header.FrameSize(0)
		if err != nil {
			continue
		}
		if !fits(data, offset, frameBytes) {
			if !final {
				return Result{Offset: offset, NeedMore: true}, false
			}
			continue
		}
		if state := verifyCBR(data, offset, header, frameBytes, final); state == candidateFound {
			return Result{Offset: offset, FrameBytes: frameBytes, Header: header}, true
		} else if state == candidateIncomplete {
			return Result{Offset: offset, NeedMore: true}, false
		}
	}

	if final {
		return Result{Offset: len(data)}, false
	}
	keep := len(data) - 3
	if keep < 0 {
		keep = 0
	}
	return Result{Offset: keep, NeedMore: true}, false
}

type candidateState uint8

const (
	candidateRejected candidateState = iota
	candidateIncomplete
	candidateFound
)

func verifyCBR(data []byte, offset int, header Header, frameBytes int, final bool) candidateState {
	next := offset + frameBytes
	if next == len(data) {
		if final {
			return candidateFound
		}
		return candidateIncomplete
	}
	if next > len(data) || len(data)-next < 4 {
		if !final {
			return candidateIncomplete
		}
		return candidateRejected
	}

	following, err := Parse(data[next:])
	if err != nil || !header.Compatible(following) || following.FreeFormat() {
		return candidateRejected
	}
	followingBytes, err := following.FrameSize(0)
	if err != nil || !fits(data, next, followingBytes) {
		if !final {
			return candidateIncomplete
		}
		return candidateRejected
	}
	return candidateFound
}

func resolveFreeFormat(data []byte, offset int, header Header, hint int, final bool) (int, int, candidateState) {
	if hint > 0 {
		frameBytes, err := header.FrameSize(hint)
		if err != nil {
			return 0, 0, candidateRejected
		}
		if !fits(data, offset, frameBytes) {
			if !final {
				return 0, 0, candidateIncomplete
			}
			return 0, 0, candidateRejected
		}

		next := offset + frameBytes
		if next == len(data) {
			if final {
				return frameBytes, hint, candidateFound
			}
			return 0, 0, candidateIncomplete
		}
		if len(data)-next < 4 {
			if !final {
				return 0, 0, candidateIncomplete
			}
			return 0, 0, candidateRejected
		}
		following, err := Parse(data[next:])
		if err != nil || !header.Compatible(following) || !following.FreeFormat() {
			return 0, 0, candidateRejected
		}
		followingBytes, err := following.FrameSize(hint)
		if err != nil {
			return 0, 0, candidateRejected
		}
		if !fits(data, next, followingBytes) {
			if !final {
				return 0, 0, candidateIncomplete
			}
			return 0, 0, candidateRejected
		}
		return frameBytes, hint, candidateFound
	}

	// With no hint, two boundaries determine a free-format size and a third
	// header confirms that the second boundary was not an accidental sync.
	for next := offset + 4; next+4 <= len(data); next++ {
		following, err := Parse(data[next:])
		if err != nil || !header.Compatible(following) || !following.FreeFormat() {
			continue
		}

		freeBytes := next - offset - header.PaddingBytes()
		if freeBytes < 4 {
			continue
		}
		frameBytes := freeBytes + header.PaddingBytes()
		if !fits(data, offset, frameBytes) {
			continue
		}

		followingBytes, err := following.FrameSize(freeBytes)
		if err != nil || !fits(data, next, followingBytes) {
			if !final {
				return 0, 0, candidateIncomplete
			}
			continue
		}
		thirdOffset := next + followingBytes
		if thirdOffset == len(data) && final {
			return frameBytes, freeBytes, candidateFound
		}
		if len(data)-thirdOffset < 4 {
			if !final {
				return 0, 0, candidateIncomplete
			}
			continue
		}
		third, err := Parse(data[thirdOffset:])
		if err != nil || !following.Compatible(third) || !third.FreeFormat() {
			continue
		}
		thirdBytes, err := third.FrameSize(freeBytes)
		if err != nil || !fits(data, thirdOffset, thirdBytes) {
			if !final {
				return 0, 0, candidateIncomplete
			}
			continue
		}
		return frameBytes, freeBytes, candidateFound
	}

	if !final {
		return 0, 0, candidateIncomplete
	}
	return 0, 0, candidateRejected
}

func fits(data []byte, offset, size int) bool {
	return offset >= 0 && size >= 0 && offset <= len(data) && size <= len(data)-offset
}
