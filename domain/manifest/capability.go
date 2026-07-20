package manifest

import (
	"fmt"
	"slices"

	"github.com/godexture/core/domain/media"
)

type Capability interface {
	Match(p media.StreamInfo) bool
	Diagnose(p media.StreamInfo) error
}

type IntConstraint struct {
	Values []int
	Min    int
	Max    int
}

func (c IntConstraint) Match(value int) bool {
	if len(c.Values) > 0 && !slices.Contains(c.Values, value) {
		return false
	}
	if c.Min != 0 && value < c.Min {
		return false
	}
	return c.Max == 0 || value <= c.Max
}

func (c IntConstraint) String() string {
	if len(c.Values) > 0 {
		return fmt.Sprintf("%v", c.Values)
	}
	if c.Min != 0 && c.Max != 0 {
		return fmt.Sprintf("%d..%d", c.Min, c.Max)
	}
	if c.Min != 0 {
		return fmt.Sprintf(">=%d", c.Min)
	}
	if c.Max != 0 {
		return fmt.Sprintf("<=%d", c.Max)
	}
	return "any"
}

func (c IntConstraint) Candidates(current int) []int {
	if len(c.Values) > 0 {
		return append([]int(nil), c.Values...)
	}
	if c.Match(current) {
		return []int{current}
	}
	if c.Min != 0 {
		return []int{c.Min}
	}
	if c.Max != 0 {
		return []int{c.Max}
	}
	return nil
}

func (c IntConstraint) Preferred(current int) int {
	for _, value := range c.Candidates(current) {
		return value
	}
	return current
}

type SampleFormatConstraint struct {
	Format        media.SampleFormat
	BitsPerSample IntConstraint
}
