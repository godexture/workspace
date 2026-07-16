package flac

import "testing"

func TestTukeyAndSubdivideTukey(t *testing.T) {
	t.Parallel()
	window := make([]float64, 9)
	Tukey(0.5)(window)
	if window[0] != 0 || window[4] != 1 || window[8] != 0 {
		t.Fatalf("unexpected Tukey window: %v", window)
	}
	if windows := SubdivideTukey(3, 0.5); len(windows) != 6 {
		t.Fatalf("got %d windows", len(windows))
	}
	Tukey(-1)(window)
	for _, value := range window {
		if value != 1 {
			t.Fatal("negative taper was not clamped")
		}
	}
}
