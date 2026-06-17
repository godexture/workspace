module github.com/godexture/codec-mp3

go 1.26.1

require (
	github.com/godexture/core v0.0.0
	github.com/godexture/format-mp3 v0.0.0
	github.com/godexture/sdk v0.0.0
)

require golang.org/x/sync v0.20.0 // indirect

replace github.com/godexture/core => ../../core

replace github.com/godexture/sdk => ../../pkg

replace github.com/godexture/format-mp3 => ../format-mp3
