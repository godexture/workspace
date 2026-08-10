package linear

import mediaformat "github.com/godexture/godec/media/format"

func probeRaw(mediaformat.ProbeContext) (mediaformat.ProbeResult, error) {
	return mediaformat.Fallback(), nil
}
