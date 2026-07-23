module github.com/godexture/codec-flac

go 1.26.4

require (
	github.com/godexture/core v0.0.0
	github.com/godexture/format-flac v0.0.0
	github.com/godexture/sdk v0.0.0
)

require (
	github.com/godexture/metadata-id3 v0.0.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
)

replace github.com/godexture/codec-pcm => ../codec-pcm

replace github.com/godexture/core => ../../core

replace github.com/godexture/format-flac => ../format-flac

replace github.com/godexture/format-wav => ../format-wav

replace github.com/godexture/format-mp3 => ../format-mp3

replace github.com/godexture/metadata-id3 => ../metadata-id3

replace github.com/godexture/codec-mp3 => ../codec-mp3

replace github.com/godexture/sdk => ../../pkg
