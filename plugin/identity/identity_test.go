// Package identity is the M1 baseline for docs/refactor/checkpoint.md
// M1#1's "後からfamily directoryへ go.mod を置いてもimport pathとmarker
// identityが変わらない" completion condition. Real marker-type identity
// (docs/refactor/plugins.md's plugin.Define[markerType]) doesn't exist
// until M2, so this snapshots the one identity signal that does exist
// today: each family's Go import path, via reflection on a stable
// exported symbol already public in that package. If a future move
// changes any of these paths, this test is the thing that catches it.
package identity_test

import (
	"reflect"
	"runtime"
	"testing"

	filteraudio "github.com/godexture/godec/plugin/audio"
	flac "github.com/godexture/godec/plugin/flac"
	id3 "github.com/godexture/godec/plugin/id3"
	mp3 "github.com/godexture/godec/plugin/mp3"
	pcm "github.com/godexture/godec/plugin/pcm"
	vorbiscomment "github.com/godexture/godec/plugin/vorbiscomment"
	wave "github.com/godexture/godec/plugin/wave"
)

func funcPkgPath(fn any) string {
	name := runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name()
	// name is "pkgpath.FuncName" (or "pkgpath.(*Receiver).Method"); the
	// package path itself never contains a dot-separated final segment
	// boundary ambiguity here because Go import paths don't end in a
	// capitalized identifier segment the way the trailing func name does,
	// so trimming at the last '.' before the identifier is reliable for
	// the plain top-level functions used below.
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '/' {
			break
		}
	}
	lastDot := -1
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '.' {
			lastDot = i
			break
		}
	}
	return name[:lastDot]
}

func TestFamilyPackageIdentitySnapshot(t *testing.T) {
	cases := []struct {
		family string
		want   string
		pkg    string
	}{
		{"flac", "github.com/godexture/godec/plugin/flac", funcPkgPath(flac.MustNewEncoderConfig)},
		{"mp3", "github.com/godexture/godec/plugin/mp3", funcPkgPath(mp3.MustNewDecoderConfig)},
		{"pcm", "github.com/godexture/godec/plugin/pcm", funcPkgPath(pcm.MustNewEncoderConfig)},
		{"wave", "github.com/godexture/godec/plugin/wave", funcPkgPath(wave.MustNewDemuxerConfig)},
		{"audio", "github.com/godexture/godec/plugin/audio", funcPkgPath(filteraudio.MustNewFormatConfig)},
		{"id3", "github.com/godexture/godec/plugin/id3", funcPkgPath(id3.ParseReader)},
		{"vorbiscomment", "github.com/godexture/godec/plugin/vorbiscomment", funcPkgPath(vorbiscomment.Marshal)},
	}
	for _, c := range cases {
		t.Run(c.family, func(t *testing.T) {
			if c.pkg != c.want {
				t.Fatalf("package path = %q, want %q", c.pkg, c.want)
			}
		})
	}
}
