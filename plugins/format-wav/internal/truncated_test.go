package internal

import (
	"bytes"
	"io"
	"testing"
)

// TestDemuxerRejectsTruncatedInput is the WAVE/PCM baseline for
// docs/refactor/quality.md's "cancel、invalid input、Finalize/Close
// failure" M0 item. It pins the demuxer's exact current contract across
// every truncation length of a valid file, not just "no panic":
//
//   - Analyze() only needs the RIFF/fmt (and, once present, data) chunk
//     headers, not the sample payload, so header-complete prefixes must
//     succeed even before the payload -- or even the data chunk header --
//     is present. Once it declares a dataOffset/dataSize, every longer
//     prefix must report the exact same dataOffset/dataSize: truncation
//     may only remove payload bytes, never change the declared shape.
//   - ReadPacket must return exactly min(declared dataSize, bytes actually
//     present after dataOffset) total payload bytes, terminating in
//     io.EOF -- never more (padding header/missing bytes into the count,
//     or fabricating declared size, would change this number), never an
//     infinite loop of zero-length packets, and never a panic.
//   - The complete, untruncated file must itself succeed end to end.
func TestDemuxerRejectsTruncatedInput(t *testing.T) {
	t.Parallel()
	payload := bytes.Repeat([]byte{0x10, 0x00, 0x20, 0x00, 0x30, 0x00, 0x40, 0x00}, 64)
	full := buildTestWAV(t, payload)

	// Ground truth independent of the demuxer's own parsing: the "data"
	// FourCC is immediately followed by a 4-byte size field, then the
	// payload we asked buildTestWAV to write. A mutant that fabricates or
	// miscalculates dataOffset/dataSize inside parseHeader must diverge
	// from these, not just be self-consistent across truncation lengths.
	tagIndex := bytes.Index(full, []byte("data"))
	if tagIndex < 0 {
		t.Fatal("test fixture invariant broken: no data chunk tag in the built WAV bytes")
	}
	wantDataOffset := int64(tagIndex + 8)
	wantDataSize := uint64(len(payload))

	analyzeSucceededOnce := false
	for length := 0; length <= len(full); length++ {
		truncated := full[:length]
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("length=%d: demuxer panicked: %v", length, r)
				}
			}()

			demuxer, err := NewDemuxer(bytes.NewReader(truncated), DemuxerConfig{})
			if err != nil {
				if analyzeSucceededOnce {
					t.Fatalf("length=%d: NewDemuxer() failed after a shorter prefix already succeeded: %v", length, err)
				}
				return
			}

			if _, _, err := demuxer.Analyze(); err != nil {
				if analyzeSucceededOnce {
					t.Fatalf("length=%d: Analyze() failed after a shorter prefix already succeeded: %v", length, err)
				}
				return
			}
			analyzeSucceededOnce = true

			wantPayload := 0
			if demuxer.header.dataOffset != 0 {
				if demuxer.header.dataOffset != wantDataOffset {
					t.Fatalf("length=%d: dataOffset = %d, want %d (declared shape must not change under truncation)",
						length, demuxer.header.dataOffset, wantDataOffset)
				}
				if demuxer.header.dataSize != wantDataSize {
					t.Fatalf("length=%d: dataSize = %d, want %d (declared shape must not change under truncation)",
						length, demuxer.header.dataSize, wantDataSize)
				}
				available := int64(length) - demuxer.header.dataOffset
				if available < 0 {
					available = 0
				}
				wantPayload = int(min(uint64(available), demuxer.header.dataSize))
			}

			total := 0
			const maxIterations = 10_000
			for iterations := 0; ; iterations++ {
				if iterations >= maxIterations {
					t.Fatalf("length=%d: ReadPacket() did not terminate within %d calls", length, maxIterations)
				}
				packet, _, err := demuxer.ReadPacket()
				if err != nil {
					if err != io.EOF {
						t.Fatalf("length=%d: ReadPacket() terminal error = %v, want io.EOF", length, err)
					}
					break
				}
				if len(packet.Data()) == 0 {
					packet.Release()
					t.Fatalf("length=%d: ReadPacket() returned a zero-length packet without an error", length)
				}
				total += len(packet.Data())
				packet.Release()
			}

			if total != wantPayload {
				t.Fatalf("length=%d: ReadPacket() returned %d payload bytes, want exactly %d (dataOffset=%d dataSize=%d)",
					length, total, wantPayload, demuxer.header.dataOffset, demuxer.header.dataSize)
			}
		}()
	}
	if !analyzeSucceededOnce {
		t.Fatal("Analyze() never succeeded, even for the full untruncated file")
	}
}
