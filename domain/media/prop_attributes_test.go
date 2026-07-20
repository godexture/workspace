package media

import "testing"

func TestAudioAttributesEffectiveBitsPerSample(t *testing.T) {
	t.Parallel()
	if got, want := (AudioAttributes{Format: SampleFormatS24}).EffectiveBitsPerSample(), 24; got != want {
		t.Fatalf("default effective bits = %d, want %d", got, want)
	}
	if got, want := (AudioAttributes{Format: SampleFormatS32, BitsPerSample: 20}).EffectiveBitsPerSample(), 20; got != want {
		t.Fatalf("explicit effective bits = %d, want %d", got, want)
	}
}

func TestEffectiveBitsPerSample(t *testing.T) {
	t.Parallel()
	if got, want := EffectiveBitsPerSample(SampleFormatS16, 0), 16; got != want {
		t.Fatalf("default effective bits = %d, want %d", got, want)
	}
	if got, want := EffectiveBitsPerSample(SampleFormatS32, 24), 24; got != want {
		t.Fatalf("explicit effective bits = %d, want %d", got, want)
	}
}
