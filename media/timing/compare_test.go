package timing

import (
	"errors"
	"math"
	"testing"
)

func TestCompareMixedBases(t *testing.T) {
	tests := []struct {
		name      string
		left      int64
		leftBase  Base
		right     int64
		rightBase Base
		want      int
	}{
		{
			name:      "equal",
			left:      1,
			leftBase:  MustBase(1, 2),
			right:     2,
			rightBase: MustBase(1, 4),
			want:      0,
		},
		{
			name:      "near below",
			left:      10_000_000_001,
			leftBase:  MustBase(1, 10_000_000_000),
			right:     1,
			rightBase: MustBase(1, 1),
			want:      1,
		},
		{
			name:      "near above",
			left:      9_999_999_999,
			leftBase:  MustBase(1, 10_000_000_000),
			right:     1,
			rightBase: MustBase(1, 1),
			want:      -1,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := Compare(testCase.left, testCase.leftBase, testCase.right, testCase.rightBase)
			if err != nil || got != testCase.want {
				t.Fatalf("Compare() = %d, %v; want %d", got, err, testCase.want)
			}
		})
	}
}

func TestCompareSignsAndZero(t *testing.T) {
	base := MustBase(1, 1)
	tests := []struct {
		left, right int64
		want        int
	}{
		{left: 0, right: 0, want: 0},
		{left: 0, right: 1, want: -1},
		{left: 0, right: -1, want: 1},
		{left: 1, right: 0, want: 1},
		{left: -1, right: 0, want: -1},
		{left: -1, right: -1, want: 0},
		{left: -2, right: -1, want: -1},
		{left: -1, right: -2, want: 1},
		{left: -1, right: 1, want: -1},
		{left: 1, right: -1, want: 1},
		{left: math.MinInt64, right: math.MinInt64, want: 0},
	}
	for _, testCase := range tests {
		got, err := Compare(testCase.left, base, testCase.right, base)
		if err != nil || got != testCase.want {
			t.Fatalf("Compare(%d, %d) = %d, %v; want %d", testCase.left, testCase.right, got, err, testCase.want)
		}
	}
}

func TestCompareInvalidBase(t *testing.T) {
	valid := MustBase(1, 1)
	invalid := []Base{{}, {Numerator: 0, Denominator: 1}, {Numerator: 1, Denominator: 0}, {Numerator: -1, Denominator: 1}, {Numerator: 1, Denominator: -1}}
	for _, base := range invalid {
		if _, err := Compare(0, base, 0, valid); !errors.Is(err, ErrInvalidBase) {
			t.Fatalf("invalid left base %v: %v", base, err)
		}
		if _, err := Compare(0, valid, 0, base); !errors.Is(err, ErrInvalidBase) {
			t.Fatalf("invalid right base %v: %v", base, err)
		}
	}
}

func TestCompareOverflow(t *testing.T) {
	base := MustBase(1<<62, 1)
	other := MustBase(1, 1<<62)
	if _, err := Compare(1<<62, base, 1, other); !errors.Is(err, ErrOverflow) {
		t.Fatalf("overflow = %v", err)
	}
}

func TestCompareAntisymmetric(t *testing.T) {
	base := MustBase(1, 48_000)
	other := MustBase(1001, 44_100)
	tests := [][2]int64{{0, 0}, {1, 2}, {-1, 2}, {2, -1}, {math.MinInt64, math.MaxInt64}}
	for _, values := range tests {
		left, err := Compare(values[0], base, values[1], other)
		if err != nil {
			t.Fatalf("Compare(%d, %d) error: %v", values[0], values[1], err)
		}
		right, err := Compare(values[1], other, values[0], base)
		if err != nil {
			t.Fatalf("reverse Compare(%d, %d) error: %v", values[1], values[0], err)
		}
		if left != -right {
			t.Fatalf("Compare(%d, %d) = %d, reverse = %d", values[0], values[1], left, right)
		}
	}
}

func TestCompareAllocatesZero(t *testing.T) {
	leftBase := MustBase(1, 48_000)
	rightBase := MustBase(1, 1_000)
	allocations := testing.AllocsPerRun(1000, func() {
		value, err := Compare(48_024, leftBase, 1000, rightBase)
		if err != nil || value != 1 {
			panic("unexpected timestamp comparison")
		}
	})
	if allocations != 0 {
		t.Fatalf("timestamp comparison allocations = %v", allocations)
	}
}

func FuzzCompareDoesNotPanic(f *testing.F) {
	f.Add(int64(1), int64(1), int64(1), int64(1000), int64(1), int64(1))
	f.Add(int64(-9223372036854775807), int64(1000000), int64(1), int64(3), int64(2), int64(2))
	f.Fuzz(func(t *testing.T, left, leftNumerator, leftDenominator, right, rightNumerator, rightDenominator int64) {
		if leftNumerator <= 0 || leftDenominator <= 0 || rightNumerator <= 0 || rightDenominator <= 0 {
			return
		}
		leftBase := Base{Numerator: leftNumerator, Denominator: leftDenominator}
		rightBase := Base{Numerator: rightNumerator, Denominator: rightDenominator}
		comparison, err := Compare(left, leftBase, right, rightBase)
		reverse, reverseErr := Compare(right, rightBase, left, leftBase)
		if err != nil || reverseErr != nil {
			if !errors.Is(err, reverseErr) || !errors.Is(reverseErr, err) {
				t.Fatalf("asymmetric errors: %v, %v", err, reverseErr)
			}
			return
		}
		if comparison != -reverse {
			t.Fatalf("asymmetric comparison: %d, %d", comparison, reverse)
		}
	})
}
