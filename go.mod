module github.com/godexture/cli

go 1.26.1

require (
	github.com/godexture/codec-flac v0.0.0
	github.com/godexture/codec-mp3 v0.0.0
	github.com/godexture/codec-pcm v0.0.0
	github.com/godexture/core v0.0.0
	github.com/godexture/filter-audio v0.0.0
	github.com/godexture/format-flac v0.0.0
	github.com/godexture/format-mp3 v0.0.0
	github.com/godexture/format-wav v0.0.0
	github.com/godexture/sdk v0.0.0
	github.com/spf13/cobra v1.10.2
)

replace github.com/godexture/codec-flac => ../plugins/codec-flac
replace github.com/godexture/codec-mp3 => ../plugins/codec-mp3
replace github.com/godexture/codec-pcm => ../plugins/codec-pcm
replace github.com/godexture/core => ../core
replace github.com/godexture/filter-audio => ../plugins/filter-audio
replace github.com/godexture/format-flac => ../plugins/format-flac
replace github.com/godexture/format-mp3 => ../plugins/format-mp3
replace github.com/godexture/format-wav => ../plugins/format-wav
replace github.com/godexture/sdk => ../pkg
replace github.com/godexture/metadata-id3 => ../plugins/metadata-id3
replace github.com/godexture/metadata-vorbiscomment => ../plugins/metadata-vorbiscomment
