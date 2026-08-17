package mp4

import "github.com/godexture/godec/media/format"

type formatID struct{}

// MP4 identifies ISO Base Media File Format streams carried as MP4 files.
func MP4() format.Format {
	value, err := format.DefinePacketized[formatID](nil, format.WithExtensions("mp4"))
	if err != nil {
		panic(err)
	}
	return value
}
