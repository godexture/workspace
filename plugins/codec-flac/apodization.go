package flac

import (
	"github.com/godexture/codec-flac/internal/flac"
)

type Apodization = flac.Apodization

func Tukey(p float64) Apodization                       { return flac.Tukey(p) }
func SubdivideTukey(parts int, p float64) []Apodization { return flac.SubdivideTukey(parts, p) }
