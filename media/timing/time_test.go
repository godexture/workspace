package timing

import (
	"errors"
	"math"
	"testing"
)

func TestRescaleRequiresExplicitRounding(t *testing.T) {
	from := MustBase(1, 1)
	to := MustBase(1, 2)
	if got, err := from.Rescale(3, to, RoundTowardZero); err != nil || got != 6 {
		t.Fatalf("exact rescale = %d, %v", got, err)
	}
	if got, err := MustBase(1, 2).Rescale(1, MustBase(1, 1), RoundTowardZero); err != nil || got != 0 {
		t.Fatalf("toward zero = %d, %v", got, err)
	}
	if got, err := MustBase(1, 2).Rescale(1, MustBase(1, 1), RoundCeil); err != nil || got != 1 {
		t.Fatalf("ceil = %d, %v", got, err)
	}
	if got, err := MustBase(1, 2).Rescale(-1, MustBase(1, 1), RoundFloor); err != nil || got != -1 {
		t.Fatalf("floor = %d, %v", got, err)
	}
}

func TestRescaleNearestTies(t *testing.T) {
	base := MustBase(1, 1)
	to := MustBase(2, 1)
	if got, err := base.Rescale(1, to, RoundNearestEven); err != nil || got != 0 {
		t.Fatalf("1/2 nearest even = %d, %v", got, err)
	}
	if got, err := base.Rescale(1, to, RoundNearestAway); err != nil || got != 1 {
		t.Fatalf("1/2 nearest away = %d, %v", got, err)
	}
	if got, err := base.Rescale(3, MustBase(4, 1), RoundNearestEven); err != nil || got != 1 {
		t.Fatalf("3/4 nearest even = %d, %v", got, err)
	}
}

func TestRescaleOverflowAndNegativeLimits(t *testing.T) {
	if _, err := MustBase(1, 1).Rescale(math.MaxInt64, MustBase(1, 2), RoundAwayFromZero); !errors.Is(err, ErrOverflow) {
		t.Fatalf("positive overflow = %v", err)
	}
	if got, err := MustBase(1, 1).Rescale(math.MinInt64, MustBase(2, 1), RoundTowardZero); err != nil || got != math.MinInt64/2 {
		t.Fatalf("negative limit = %d, %v", got, err)
	}
}

func TestOptionalTimestampDistinguishesZero(t *testing.T) {
	unknown := UnknownPTS()
	if unknown.Valid() {
		t.Fatal("unknown timestamp is valid")
	}
	zero := SomePTS(NewPTS(0))
	if !zero.Valid() || zero.Value() != 0 {
		t.Fatalf("zero timestamp = %#v", zero)
	}
	rescaled, err := unknown.Rescale(MustBase(1, 1), MustBase(1, 1000), RoundTowardZero)
	if err != nil || rescaled.Valid() {
		t.Fatalf("unknown rescale = %#v, %v", rescaled, err)
	}
}

func FuzzRescaleDoesNotPanic(f *testing.F) {
	f.Add(int64(1), int64(1), int64(1000), int64(1), int64(1))
	f.Add(int64(-9223372036854775807), int64(1000000), int64(1), int64(3), int64(2))
	f.Fuzz(func(t *testing.T, value, numerator, denominator, targetNumerator, targetDenominator int64) {
		if numerator <= 0 || denominator <= 0 || targetNumerator <= 0 || targetDenominator <= 0 {
			return
		}
		from := Base{Numerator: numerator, Denominator: denominator}
		to := Base{Numerator: targetNumerator, Denominator: targetDenominator}
		_, _ = from.Rescale(value, to, RoundNearestEven)
	})
}
