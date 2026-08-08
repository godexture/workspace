package frame

import "github.com/godexture/godec/plugin/flac/internal/streaminfo"

func StartSample(header Header, info streaminfo.StreamInfo) uint64 {
	if header.BlockingStrategy {
		return header.Number
	}
	return header.Number * uint64(info.MaxBlockSize)
}
