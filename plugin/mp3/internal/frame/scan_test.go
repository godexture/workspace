package frame

import (
	"bytes"
	"testing"
)

func TestScanFindsConsecutiveCBRFrames(t *testing.T) {
	header := testHeader(3, 1, 9, 0, false, 0)
	first := testFrame(header, 417)
	second := testFrame(header, 417)
	data := append(first, second...)

	got, ok := Scan(data, 0, false)
	if !ok {
		t.Fatal("Scan() did not find consecutive CBR frames")
	}
	if got.Offset != 0 || got.FrameBytes != 417 || got.FreeFormatBytes != 0 {
		t.Fatalf("Scan() = %+v, want offset 0, frame size 417", got)
	}
	if got.Header.Layer() != LayerIII || got.Header.BitrateKbps() != 128 {
		t.Fatalf("Scan() header = %+v", got.Header)
	}
}

func TestScanAcceptsConsecutiveFramesWithVaryingBitrate(t *testing.T) {
	firstHeader := testHeader(3, 1, 9, 0, false, 0)
	secondHeader := testHeader(3, 1, 10, 0, true, 0)
	data := testFrame(firstHeader, 417)
	data = append(data, testFrame(secondHeader, 523)...)

	got, ok := Scan(data, 0, false)
	if !ok || got.Offset != 0 || got.FrameBytes != 417 {
		t.Fatalf("Scan() = %+v, %v, want first varying-bitrate frame", got, ok)
	}
}

func TestScanAcceptsOneCompleteFrameAtEnd(t *testing.T) {
	header := testHeader(3, 1, 9, 0, false, 0)
	data := testFrame(header, 417)

	got, ok := Scan(data, 0, true)
	if !ok || got.Offset != 0 || got.FrameBytes != len(data) {
		t.Fatalf("Scan() = %+v, %v, want one complete frame", got, ok)
	}
}

func TestScanWaitsForFollowingCBRFrameBeforeEOF(t *testing.T) {
	header := testHeader(3, 1, 9, 0, false, 0)
	data := testFrame(header, 417)

	got, ok := Scan(data, 0, false)
	if ok || !got.NeedMore || got.Offset != 0 {
		t.Fatalf("Scan() = %+v, %v, want incomplete result at candidate offset", got, ok)
	}
}

func TestScanRetainsPartialHeaderSuffix(t *testing.T) {
	data := []byte{0x01, 0x02, 0xff, 0xfb}

	got, ok := Scan(data, 0, false)
	if ok || !got.NeedMore || got.Offset != 1 {
		t.Fatalf("Scan() = %+v, %v, want final three bytes retained", got, ok)
	}
}

func TestScanReportsTruncatedCandidateOnlyUntilEOF(t *testing.T) {
	header := testHeader(3, 1, 9, 0, false, 0)
	data := testFrame(header, 100)

	waiting, ok := Scan(data, 0, false)
	if ok || !waiting.NeedMore || waiting.Offset != 0 {
		t.Fatalf("Scan(non-final) = %+v, %v, want more data", waiting, ok)
	}
	final, ok := Scan(data, 0, true)
	if ok || final.NeedMore || final.Offset != len(data) {
		t.Fatalf("Scan(final) = %+v, %v, want rejected truncation", final, ok)
	}
}

func TestScanStopsAtEarliestIncompleteCBRCandidate(t *testing.T) {
	// The first header describes a large MPEG-2 Layer III frame. Two smaller,
	// compatible frames later in this chunk are complete, but cannot displace
	// the earlier candidate while the chunk is non-final.
	largeHeader := testHeader(2, 1, 14, 0, false, 0)
	smallHeader := testHeader(2, 1, 1, 0, false, 0)
	data := make([]byte, 56)
	copy(data, largeHeader[:])
	copy(data[4:], testFrame(smallHeader, 26))
	copy(data[30:], testFrame(smallHeader, 26))

	got, ok := Scan(data, 0, false)
	if ok || !got.NeedMore || got.Offset != 0 {
		t.Fatalf("Scan() = %+v, %v, want earliest candidate retained", got, ok)
	}
}

func TestScanResynchronizesPastLeadingGarbage(t *testing.T) {
	header := testHeader(3, 1, 9, 0, false, 0)
	data := append([]byte{0x01, 0x02, 0x03, 0x04, 0x05}, testFrame(header, 417)...)
	data = append(data, testFrame(header, 417)...)

	got, ok := Scan(data, 0, false)
	if !ok || got.Offset != 5 {
		t.Fatalf("Scan() = %+v, %v, want offset 5", got, ok)
	}
}

func TestScanSkipsFalseSync(t *testing.T) {
	falseHeader := testHeader(3, 1, 9, 0, false, 0)
	validHeader := testHeader(3, 1, 9, 0, false, 0)
	data := testFrame(falseHeader, 120)
	data = append(data, testFrame(validHeader, 417)...)
	data = append(data, testFrame(validHeader, 417)...)

	got, ok := Scan(data, 0, false)
	if !ok || got.Offset != 120 {
		t.Fatalf("Scan() = %+v, %v, want false sync skipped", got, ok)
	}
}

func TestScanRejectsTruncatedFrame(t *testing.T) {
	header := testHeader(3, 1, 9, 0, false, 0)
	data := testFrame(header, 4)

	got, ok := Scan(data, 0, true)
	if ok || got.Offset != len(data) {
		t.Fatalf("Scan() = %+v, %v, want truncated frame rejected", got, ok)
	}
}

func TestScanResolvesFreeFormatFromRepeatedHeaders(t *testing.T) {
	header := testHeader(3, 1, 0, 0, false, 0)
	data := testFrame(header, 300)
	data = append(data, testFrame(header, 300)...)
	data = append(data, testFrame(header, 300)...)

	got, ok := Scan(data, 0, false)
	if !ok || got.Offset != 0 || got.FrameBytes != 300 || got.FreeFormatBytes != 300 {
		t.Fatalf("Scan() = %+v, %v, want free-format size 300", got, ok)
	}
}

func TestScanResolvesFreeFormatBeyondLegacySmallBounds(t *testing.T) {
	const freeBytes = 4096
	header := testHeader(3, 1, 0, 0, false, 0)
	data := testFrame(header, freeBytes)
	data = append(data, testFrame(header, freeBytes)...)
	data = append(data, testFrame(header, freeBytes)...)

	got, ok := Scan(data, 0, false)
	if !ok || got.FrameBytes != freeBytes || got.FreeFormatBytes != freeBytes {
		t.Fatalf("Scan() = %+v, %v, want free-format size %d", got, ok, freeBytes)
	}
}

func TestScanUsesFreeFormatHint(t *testing.T) {
	header := testHeader(3, 1, 0, 0, false, 0)
	data := testFrame(header, 300)

	got, ok := Scan(data, 300, true)
	if !ok || got.Offset != 0 || got.FrameBytes != 300 || got.FreeFormatBytes != 300 {
		t.Fatalf("Scan() = %+v, %v, want hinted free-format size 300", got, ok)
	}
}

func TestScanWaitsForFollowingFreeFormatFrame(t *testing.T) {
	header := testHeader(3, 1, 0, 0, false, 0)
	data := testFrame(header, 300)

	got, ok := Scan(data, 300, false)
	if ok || !got.NeedMore || got.Offset != 0 {
		t.Fatalf("Scan() = %+v, %v, want more data for hinted frame", got, ok)
	}
}

func TestScanUsesPaddingWithFreeFormatHint(t *testing.T) {
	header := testHeader(3, 1, 0, 0, true, 0)
	data := testFrame(header, 301)

	got, ok := Scan(data, 300, true)
	if !ok || got.Offset != 0 || got.FrameBytes != 301 || got.FreeFormatBytes != 300 {
		t.Fatalf("Scan() = %+v, %v, want padded hinted free-format size", got, ok)
	}
}

func TestScanWithoutHintNeedsRepeatedFreeHeaders(t *testing.T) {
	header := testHeader(3, 1, 0, 0, false, 0)
	data := testFrame(header, 300)

	got, ok := Scan(data, 0, true)
	if ok || got.Offset != len(data) {
		t.Fatalf("Scan() = %+v, %v, want unresolved free-format frame rejected", got, ok)
	}
}

func TestScanAcceptsTwoCompleteFreeFramesAtEOF(t *testing.T) {
	header := testHeader(3, 1, 0, 0, false, 0)
	data := testFrame(header, 300)
	data = append(data, testFrame(header, 300)...)

	got, ok := Scan(data, 0, true)
	if !ok || got.Offset != 0 || got.FrameBytes != 300 || got.FreeFormatBytes != 300 {
		t.Fatalf("Scan() = %+v, %v, want two-frame EOF resolution", got, ok)
	}
}

func TestScanStopsAtEarliestUnresolvedFreeCandidate(t *testing.T) {
	firstHeader := testHeader(3, 1, 0, 0, false, 0)
	laterHeader := testHeader(3, 1, 0, 0, false, 0)
	data := make([]byte, 930)
	copy(data, firstHeader[:])
	copy(data[30:], testFrame(laterHeader, 300))
	copy(data[330:], testFrame(laterHeader, 300))
	copy(data[630:], testFrame(laterHeader, 300))

	got, ok := Scan(data, 0, false)
	if ok || !got.NeedMore || got.Offset != 0 {
		t.Fatalf("Scan() = %+v, %v, want unresolved first free candidate retained", got, ok)
	}
}

func TestScanIsDeterministic(t *testing.T) {
	header := testHeader(3, 1, 0, 0, false, 0)
	data := append([]byte{0x99, 0x88}, testFrame(header, 300)...)
	data = append(data, testFrame(header, 300)...)
	data = append(data, testFrame(header, 300)...)

	first, firstOK := Scan(data, 0, false)
	second, secondOK := Scan(data, 0, false)
	if firstOK != secondOK || first.Offset != second.Offset || first.FrameBytes != second.FrameBytes || first.FreeFormatBytes != second.FreeFormatBytes {
		t.Fatalf("Scan() changed between runs: first=%+v,%v second=%+v,%v", first, firstOK, second, secondOK)
	}
}

func testFrame(header [4]byte, size int) []byte {
	frame := bytes.Repeat([]byte{0}, size)
	copy(frame, header[:])
	return frame
}
