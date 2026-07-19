module github.com/godexture/format-flac

go 1.26.4

require (
	github.com/godexture/core v0.0.0
	github.com/godexture/metadata-id3 v0.0.0
	github.com/godexture/metadata-vorbiscomment v0.0.0
	github.com/godexture/sdk v0.0.0
)

require golang.org/x/sync v0.20.0 // indirect

replace github.com/godexture/core => ../../core

replace github.com/godexture/metadata-id3 => ../metadata-id3

replace github.com/godexture/metadata-vorbiscomment => ../metadata-vorbiscomment

replace github.com/godexture/sdk => ../../pkg
