package decoder

import (
	"strings"
	"testing"
)

func TestRestoreFixedMode(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		checked := []int64{3, 5, 1, 1, 1}
		unchecked := append([]int64(nil), checked...)

		if err := restoreFixedChecked(checked, 1, 16); err != nil {
			t.Fatal(err)
		}
		restoreFixedUnchecked(unchecked, 1)
		if !equalInt64s(checked, unchecked) {
			t.Fatalf("checked = %v, unchecked = %v", checked, unchecked)
		}
	})

	t.Run("range", func(t *testing.T) {
		checked := []int64{32767, 1, 5}
		err := restoreFixedChecked(checked, 1, 16)
		if err == nil || err.Error() != "invalid FLAC fixed prediction: FLAC subframe sample 32768 outside 16-bit range" {
			t.Fatalf("error = %v", err)
		}
		if !equalInt64s(checked, []int64{32767, 1, 5}) {
			t.Fatalf("strict modified samples after validation failure: %v", checked)
		}

		unchecked := []int64{32767, 1, 5}
		restoreFixedUnchecked(unchecked, 1)
		if unchecked[1] != 32768 {
			t.Fatalf("sample = %d, want 32768", unchecked[1])
		}
	})
}

func TestRestoreLPCMode(t *testing.T) {
	coefficients := []int64{1}

	t.Run("valid", func(t *testing.T) {
		checked := []int64{3, 2, 2, -1}
		unchecked := append([]int64(nil), checked...)
		if err := restoreLPC(checked, coefficients, 1, 0, 16, true); err != nil {
			t.Fatal(err)
		}
		if err := restoreLPC(unchecked, coefficients, 1, 0, 16, false); err != nil {
			t.Fatal(err)
		}
		if !equalInt64s(checked, unchecked) {
			t.Fatalf("strict = %v, non-strict = %v", checked, unchecked)
		}
	})

	t.Run("range", func(t *testing.T) {
		checked := []int64{32767, 1, 5}
		err := restoreLPC(checked, coefficients, 1, 0, 16, true)
		if err == nil || !strings.Contains(err.Error(), "FLAC subframe sample 32768 outside 16-bit range") {
			t.Fatalf("strict error = %v", err)
		}
		if !equalInt64s(checked, []int64{32767, 1, 5}) {
			t.Fatalf("strict modified samples after validation failure: %v", checked)
		}

		unchecked := []int64{32767, 1, 5}
		if err := restoreLPC(unchecked, coefficients, 1, 0, 16, false); err != nil {
			t.Fatalf("non-strict error = %v", err)
		}
		if unchecked[1] != 32768 {
			t.Fatalf("sample = %d, want 32768", unchecked[1])
		}
	})
}

func equalInt64s(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
