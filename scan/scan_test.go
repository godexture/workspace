package scan

import "testing"

// cbrHeader is a valid MPEG1 Layer3, 128kbps, 44100Hz, stereo header. Its
// frame size (417 bytes, no padding) is fixed regardless of freeFormatBytes.
var cbrHeader = []byte{0xFF, 0xFB, 0x90, 0x00}

const cbrFrameBytes = 417

// freeFormatHeader is a valid MPEG1 Layer3, free-format (bitrate index 0),
// 44100Hz, no-padding header. Its frame size is not derivable from the
// header alone; Frame must either be given it via freeFormatBytes or
// resolve it by finding a second matching header at a fixed offset.
var freeFormatHeader = []byte{0xFF, 0xFB, 0x00, 0x00}

func cbrFrame() []byte {
	frame := make([]byte, cbrFrameBytes)
	copy(frame, cbrHeader)
	return frame
}

func freeFormatFrame(size int) []byte {
	frame := make([]byte, size)
	copy(frame, freeFormatHeader)
	return frame
}

func TestFrame_EndOfBufferShortCircuit(t *testing.T) {
	t.Parallel()
	data := cbrFrame()

	offset, frameBytes, newFreeFormatBytes, found := Frame(data, 0)
	if !found {
		t.Fatal("Frame() found = false, want true")
	}
	if offset != 0 || frameBytes != cbrFrameBytes {
		t.Fatalf("offset=%d frameBytes=%d, want 0/%d", offset, frameBytes, cbrFrameBytes)
	}
	if newFreeFormatBytes != 0 {
		t.Fatalf("newFreeFormatBytes = %d, want 0 (not a free-format frame)", newFreeFormatBytes)
	}
}

func TestFrame_VerifiesAgainstFollowingFrame(t *testing.T) {
	t.Parallel()
	data := append(cbrFrame(), cbrFrame()...)

	offset, frameBytes, _, found := Frame(data, 0)
	if !found {
		t.Fatal("Frame() found = false, want true")
	}
	if offset != 0 || frameBytes != cbrFrameBytes {
		t.Fatalf("offset=%d frameBytes=%d, want 0/%d", offset, frameBytes, cbrFrameBytes)
	}
}

func TestFrame_ResynchronizesPastLeadingGarbage(t *testing.T) {
	t.Parallel()
	garbage := []byte{0x00, 0x12, 0x34, 0x56, 0x78}
	data := append(append(garbage, cbrFrame()...), cbrFrame()...)

	offset, frameBytes, _, found := Frame(data, 0)
	if !found {
		t.Fatal("Frame() found = false, want true")
	}
	if offset != len(garbage) || frameBytes != cbrFrameBytes {
		t.Fatalf("offset=%d frameBytes=%d, want %d/%d", offset, frameBytes, len(garbage), cbrFrameBytes)
	}
}

func TestFrame_NoValidHeaderNotFound(t *testing.T) {
	t.Parallel()
	data := make([]byte, 64)
	for i := range data {
		data[i] = byte(i)
	}

	offset, frameBytes, newFreeFormatBytes, found := Frame(data, 0)
	if found {
		t.Fatal("Frame() found = true, want false")
	}
	if offset != len(data) || frameBytes != 0 || newFreeFormatBytes != 0 {
		t.Fatalf("offset=%d frameBytes=%d newFreeFormatBytes=%d, want %d/0/0", offset, frameBytes, newFreeFormatBytes, len(data))
	}
}

func TestFrame_TruncatedFrameAtBufferEndNotFound(t *testing.T) {
	t.Parallel()
	// Just the header, nowhere near the 417 bytes the frame needs.
	data := append([]byte{}, cbrHeader...)

	_, _, _, found := Frame(data, 0)
	if found {
		t.Fatal("Frame() found = true, want false (frame extends past the buffer)")
	}
}

func TestFrame_ResolvesFreeFormatSizeFromRepeatedHeaders(t *testing.T) {
	t.Parallel()
	const size = 300
	data := append(append(freeFormatFrame(size), freeFormatFrame(size)...), freeFormatFrame(size)...)

	offset, frameBytes, newFreeFormatBytes, found := Frame(data, 0)
	if !found {
		t.Fatal("Frame() found = false, want true")
	}
	if offset != 0 || frameBytes != size {
		t.Fatalf("offset=%d frameBytes=%d, want 0/%d", offset, frameBytes, size)
	}
	if newFreeFormatBytes != size {
		t.Fatalf("newFreeFormatBytes = %d, want %d", newFreeFormatBytes, size)
	}
}

func TestFrame_UsesFreeFormatHintDirectly(t *testing.T) {
	t.Parallel()
	const size = 300
	// Only one frame's worth of data: not enough for Frame to rediscover
	// the free-format size on its own, so the hint must carry it.
	data := freeFormatFrame(size)

	offset, frameBytes, newFreeFormatBytes, found := Frame(data, size)
	if !found {
		t.Fatal("Frame() found = false, want true (should have trusted the freeFormatBytes hint)")
	}
	if offset != 0 || frameBytes != size || newFreeFormatBytes != size {
		t.Fatalf("offset=%d frameBytes=%d newFreeFormatBytes=%d, want 0/%d/%d", offset, frameBytes, newFreeFormatBytes, size, size)
	}
}

func TestFrame_WithoutHintCannotResolveSingleFreeFormatFrame(t *testing.T) {
	t.Parallel()
	const size = 300
	// Same lone frame as TestFrame_UsesFreeFormatHintDirectly, but with no
	// prior hint: there's no second header anywhere in the buffer for the
	// free-format size search to confirm against, so it must not be found.
	data := freeFormatFrame(size)

	_, _, _, found := Frame(data, 0)
	if found {
		t.Fatal("Frame() found = true, want false (no hint and no second frame to resolve the size from)")
	}
}

func TestFrame_EmptyInputNotFound(t *testing.T) {
	t.Parallel()
	offset, frameBytes, newFreeFormatBytes, found := Frame(nil, 0)
	if found || offset != 0 || frameBytes != 0 || newFreeFormatBytes != 0 {
		t.Fatalf("Frame(nil) = (%d, %d, %d, %v), want (0, 0, 0, false)", offset, frameBytes, newFreeFormatBytes, found)
	}
}
