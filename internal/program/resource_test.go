package program

import (
	"math"
	"testing"

	"github.com/godexture/godec/resource"
)

func TestPayloadMemoryScalingRejectsOverflow(t *testing.T) {
	if _, err := scaleMemory(resource.Bytes(math.MaxUint64), 2); err == nil {
		t.Fatal("payload memory overflow was accepted")
	}
}
