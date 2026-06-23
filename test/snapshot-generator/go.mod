module github.com/godexture/snapshot-generator

go 1.26.4

require (
	github.com/godexture/codec-pcm v0.0.0
	github.com/godexture/core v0.0.0
	github.com/godexture/format-wav v0.0.0
	github.com/godexture/sdk v0.0.0
)

replace (
	github.com/godexture/codec-pcm => ../../plugins/codec-pcm
	github.com/godexture/core => ../../core
	github.com/godexture/format-wav => ../../plugins/format-wav
	github.com/godexture/sdk => ../../pkg
)
