package media

import "testing"

func TestPooledAudioFrameResetsState(t *testing.T) {
	frame := NewAudioFrame(SampleFormatF32P, LayoutStereo2_0, 48000, 4, WithAudioPts(11), WithAudioBitsPerSample(24))
	frame.Release()

	next := NewAudioFrame(SampleFormatF32P, LayoutMono1, 44100, 2)
	defer next.Release()
	if next.SampleRate != 44100 || next.Samples != 2 || next.Layout != LayoutMono1 ||
		next.pts != 0 || next.BitsPerSample != defaultBitsPerSample(SampleFormatF32P) {
		t.Fatalf("pooled audio frame retained state: %+v", next)
	}
	if len(next.Planes()) != 1 || len(next.Planes()[0]) != 2*4 {
		t.Fatalf("pooled audio frame planes = %v, want 1 plane of 8 bytes", next.Planes())
	}
}

func TestPooledAudioFrameHonorsRetain(t *testing.T) {
	frame := NewAudioFrame(SampleFormatF32P, LayoutMono1, 48000, 1)
	frame.Retain()
	frame.Release()
	copy(frame.Planes()[0], []byte{1, 2, 3, 4})
	if got := frame.Planes()[0]; got[0] != 1 || got[3] != 4 {
		t.Fatalf("audio frame data changed while retained: %v", got)
	}
	frame.Release()
}

func TestPooledAudioFrameGrowsPlanesAcrossChannelCounts(t *testing.T) {
	mono := NewAudioFrame(SampleFormatF32P, LayoutMono1, 48000, 4)
	mono.Release()

	stereo := NewAudioFrame(SampleFormatF32P, LayoutStereo2_0, 48000, 4)
	defer stereo.Release()
	if len(stereo.Planes()) != 2 {
		t.Fatalf("stereo audio frame has %d planes, want 2", len(stereo.Planes()))
	}
	for i, plane := range stereo.Planes() {
		if len(plane) != 4*4 {
			t.Fatalf("plane %d has %d bytes, want 16", i, len(plane))
		}
	}
}
