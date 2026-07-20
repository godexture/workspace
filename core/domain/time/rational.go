package time

import "math/big"

type Rational big.Rat

func NewRational(numerator, denominator int64) Rational {
	return Rational(*big.NewRat(numerator, denominator))
}
