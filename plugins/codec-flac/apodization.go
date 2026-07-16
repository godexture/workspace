package flac

import "github.com/godexture/codec-flac/internal/flac"

type Apodization = flac.Apodization
type StereoMode = flac.StereoMode
type BlockingStrategy = flac.BlockingStrategy

func Tukey(p float64) Apodization                       { return flac.Tukey(p) }
func SubdivideTukey(parts int, p float64) []Apodization { return flac.SubdivideTukey(parts, p) }
