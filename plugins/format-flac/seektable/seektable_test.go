package seektable

import (
	"encoding/binary"
	"testing"
)

func TestParseSkipsPlaceholders(t *testing.T) {
	t.Parallel()
	data := make([]byte, 3*PointSize)
	binary.BigEndian.PutUint64(data[0:], 10)
	binary.BigEndian.PutUint64(data[8:], 20)
	binary.BigEndian.PutUint16(data[16:], 30)
	binary.BigEndian.PutUint64(data[PointSize:], PlaceholderSampleNumber)
	binary.BigEndian.PutUint64(data[2*PointSize:], 40)
	binary.BigEndian.PutUint64(data[2*PointSize+8:], 50)
	binary.BigEndian.PutUint16(data[2*PointSize+16:], 60)

	points, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 2 || points[0].SampleNumber != 10 || points[1].StreamOffset != 50 {
		t.Fatalf("Parse() = %#v", points)
	}
}

func TestParseRejectsPartialPoint(t *testing.T) {
	t.Parallel()
	if _, err := Parse(make([]byte, PointSize-1)); err == nil {
		t.Fatal("Parse() accepted a partial seek point")
	}
}
