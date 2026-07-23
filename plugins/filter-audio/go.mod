module github.com/godexture/filter-audio

go 1.26.1

require (
	github.com/godexture/core v0.0.0
	github.com/godexture/sdk v0.0.0
)

require golang.org/x/sync v0.21.0 // indirect

replace github.com/godexture/core => ../../core

replace github.com/godexture/sdk => ../../pkg
