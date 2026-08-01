package mp3

import "testing"

// TestDecodeFrame_PreservesFreeFormatBytesAcrossResync guards against
// Decoder.Init() (called on the resync fallback path) wiping out a
// free-format frame size resolved by an earlier call before it reaches
// scan.Frame. Without the fix, a lone free-format frame that resyncs after
// alignment is lost (no second frame available to rediscover the size from)
// is misread as "not found" instead of decoded.
func TestDecodeFrame_PreservesFreeFormatBytesAcrossResync(t *testing.T) {
	t.Parallel()

	const freeFrameSize = 300
	freeFormatHeader := []byte{0xFF, 0xFB, 0x00, 0x00} // MPEG1 Layer3, free-format, no padding
	buildFrame := func() []byte {
		frame := make([]byte, freeFrameSize)
		copy(frame, freeFormatHeader)
		return frame
	}

	// Three repeated frames give scan.Frame enough data to resolve the
	// free-format size on its own, with no prior hint available yet.
	warmup := append(append(buildFrame(), buildFrame()...), buildFrame()...)

	var d Decoder
	if _, _, err := d.DecodeFrame(warmup, nil); err != nil {
		t.Fatalf("warm-up DecodeFrame: %v", err)
	}
	if d.FreeFormatBytes != freeFrameSize {
		t.Fatalf("FreeFormatBytes after warm-up = %d, want %d", d.FreeFormatBytes, freeFrameSize)
	}

	// Simulate losing fast-path alignment (e.g. after a seek), forcing the
	// resync fallback again -- but this time with only one frame's worth of
	// data, too little for scan.Frame to rediscover the size on its own.
	d.Header = Header{}
	lone := buildFrame()
	samples, info, err := d.DecodeFrame(lone, nil)
	if err != nil {
		t.Fatalf("DecodeFrame after resync: %v", err)
	}
	if samples != SamplesPerFrameLayer23 {
		t.Fatalf("samples = %d, want %d (frame went unrecognized -- FreeFormatBytes hint was lost across Init())", samples, SamplesPerFrameLayer23)
	}
	if info.FrameOffset != 0 || info.FrameBytes != freeFrameSize {
		t.Fatalf("frameInfo = %+v, want offset=0 bytes=%d", info, freeFrameSize)
	}
	if d.FreeFormatBytes != freeFrameSize {
		t.Fatalf("FreeFormatBytes after resync = %d, want %d", d.FreeFormatBytes, freeFrameSize)
	}
}
