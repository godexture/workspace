package internal

import (
	"bytes"
	"testing"
)

// TestDemuxerRejectsTruncatedInput is the WAVE/PCM baseline for
// docs/refactor/quality.md's "cancel、invalid input、Finalize/Close
// failure" M0 item: FLAC and MP3 already had truncated/malformed-input
// tests (e.g. codec-flac's TestDecoder_RejectsPacketWithTrailingFrame,
// format-mp3's scan tests), WAVE/PCM had none.
//
// Analyze() only needs the RIFF/fmt/data chunk header, not the sample
// payload declared by the data chunk's size (ReadPacket streams that
// separately, including over non-seekable/live input), so it legitimately
// succeeds before the file is fully present. The baseline this test
// freezes is therefore: (1) no panic at any truncation length in either
// Analyze or ReadPacket, (2) Analyze's success is monotonic in the
// available length -- it never fails once it has already succeeded on a
// shorter prefix, and (3) once Analyze succeeds over a truncated payload,
// ReadPacket streams at most the bytes actually present and terminates
// with an error or EOF instead of fabricating data.
func TestDemuxerRejectsTruncatedInput(t *testing.T) {
	t.Parallel()
	full := buildTestWAV(t, bytes.Repeat([]byte{0x10, 0x00, 0x20, 0x00, 0x30, 0x00, 0x40, 0x00}, 64))

	analyzeSucceededOnce := false
	for length := 0; length < len(full); length++ {
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

			_, _, err = demuxer.Analyze()
			if err != nil {
				if analyzeSucceededOnce {
					t.Fatalf("length=%d: Analyze() failed after a shorter prefix already succeeded: %v", length, err)
				}
				return
			}
			analyzeSucceededOnce = true

			total := 0
			for {
				packet, _, err := demuxer.ReadPacket()
				if err != nil {
					// io.EOF/io.ErrUnexpectedEOF end a healthy stream; any
					// other error is an acceptable rejection of truncated
					// input. Either way, ReadPacket must stop, not panic.
					break
				}
				total += len(packet.Data())
				packet.Release()
				if total > len(truncated) {
					t.Fatalf("length=%d: ReadPacket() returned %d payload bytes from a %d-byte input", length, total, len(truncated))
				}
			}
		}()
	}
	if !analyzeSucceededOnce {
		t.Fatal("Analyze() never succeeded, even for the full untruncated file")
	}
}
