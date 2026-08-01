// Package scan locates MPEG audio frame boundaries within an in-memory
// buffer, resynchronizing past corrupt or non-frame data and resolving
// free-format frame sizes by cross-checking subsequent frame headers.
//
// It is shared by every component that must recover frame boundaries from
// raw bytes rather than a clean, pre-aligned frame stream: the standalone
// MP3 decoder's resync path and the WAV demuxer's embedded-MP3 packetizer.
package scan

import mp3header "github.com/godexture/godec/plugin/mp3/header"

const (
	// maxFreeFormatFrameSize bounds how far ahead a free-format frame's
	// size is searched for by looking for a second matching header.
	maxFreeFormatFrameSize = 2304
	// maxVerifyFrames caps how many consecutive frame headers are checked
	// to confirm a sync candidate before accepting it.
	maxVerifyFrames = 10

	// MaxLookback is the largest number of trailing bytes, beyond a
	// candidate frame's start, that Frame may need to conclusively confirm
	// or reject it: enough to resolve a free-format frame size and confirm
	// one following frame header.
	//
	// A caller that scans a bounded, refillable window rather than a fixed
	// one-shot buffer (e.g. resyncing over a stream) must not discard the
	// last MaxLookback bytes of an inconclusive window before retrying with
	// more data, or it risks dropping a candidate Frame had not yet ruled
	// out.
	MaxLookback = 2*maxFreeFormatFrameSize + 8
)

// Frame scans data for the first valid MPEG audio frame at or after offset 0.
//
// freeFormatBytes carries a frame size previously resolved for a free-format
// stream (0 if not yet known); callers should thread the returned
// newFreeFormatBytes into their next call so a once-resolved free-format
// size is not lost when resyncing past unrelated garbage.
//
// It returns the byte offset where the frame starts, its total size in bytes
// (including padding), the free-format size to carry into the next call, and
// whether a frame was found at all. When no frame is found, offset equals
// len(data) and the other return values are zero.
func Frame(data []byte, freeFormatBytes int) (offset, frameBytes, newFreeFormatBytes int, found bool) {
	for i := 0; i+4 <= len(data); i++ {
		h, err := mp3header.ParseHeader(data[i : i+4])
		if err != nil || !h.IsValid() {
			continue
		}

		candidateFreeFormatBytes := freeFormatBytes
		candidateFrameBytes := h.FrameBytes(candidateFreeFormatBytes)
		frameAndPadding := candidateFrameBytes + h.Padding()

		for step := 4; candidateFrameBytes == 0 && step < maxFreeFormatFrameSize && i+2*step <= len(data)-4; step++ {
			next, err := mp3header.ParseHeader(data[i+step : i+step+4])
			if err != nil || !h.Compare(next) {
				continue
			}

			resolvedFrameBytes := step - h.Padding()
			nextFrameBytes := resolvedFrameBytes + next.Padding()
			if i+step+nextFrameBytes+4 > len(data) {
				continue
			}

			next2, err := mp3header.ParseHeader(data[i+step+nextFrameBytes : i+step+nextFrameBytes+4])
			if err != nil || !h.Compare(next2) {
				continue
			}

			frameAndPadding = step
			candidateFrameBytes = resolvedFrameBytes
			candidateFreeFormatBytes = resolvedFrameBytes
		}

		if candidateFrameBytes == 0 || i+frameAndPadding > len(data) {
			continue
		}

		if verify(data[i:], h, candidateFreeFormatBytes) || i+frameAndPadding == len(data) {
			return i, frameAndPadding, candidateFreeFormatBytes, true
		}
	}

	return len(data), 0, 0, false
}

// verify confirms a sync candidate at the start of data by checking that up
// to maxVerifyFrames consecutive frame headers following it are consistent
// with the first. It returns true once at least one following frame matches,
// or once maxVerifyFrames matches are confirmed.
func verify(data []byte, first mp3header.Header, freeFormatBytes int) bool {
	byteIndex := 0
	current := first

	for matched := 0; matched < maxVerifyFrames; matched++ {
		byteIndex += current.FrameBytes(freeFormatBytes) + current.Padding()
		if byteIndex+4 > len(data) {
			return matched > 0
		}

		next, err := mp3header.ParseHeader(data[byteIndex : byteIndex+4])
		if err != nil || !first.Compare(next) {
			return matched > 0
		}
		current = next
	}

	return true
}
