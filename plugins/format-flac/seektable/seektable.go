package seektable

import (
	"encoding/binary"
	"errors"
)

const (
	MetadataType            = 3
	PointSize               = 18
	PlaceholderSampleNumber = ^uint64(0)
)

type Point struct {
	SampleNumber uint64
	StreamOffset uint64
	FrameSamples uint16
}

func Parse(data []byte) ([]Point, error) {
	if len(data)%PointSize != 0 {
		return nil, errors.New("invalid FLAC SEEKTABLE length")
	}

	points := make([]Point, 0, len(data)/PointSize)
	for len(data) > 0 {
		point := Point{
			SampleNumber: binary.BigEndian.Uint64(data),
			StreamOffset: binary.BigEndian.Uint64(data[8:]),
			FrameSamples: binary.BigEndian.Uint16(data[16:]),
		}
		if point.SampleNumber != PlaceholderSampleNumber {
			points = append(points, point)
		}
		data = data[PointSize:]
	}
	return points, nil
}
