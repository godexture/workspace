package vorbiscomment

import (
	"encoding/base64"
	"reflect"
	"testing"

	"github.com/godexture/godec/core/domain/metadata"
)

func TestParsePicture(t *testing.T) {
	t.Parallel()
	want := metadata.Thumbnail{
		Data:          []byte{1, 2, 3},
		MIMEType:      "image/png",
		PictureType:   metadata.PictureTypeFrontCover,
		Description:   "cover",
		Width:         100,
		Height:        200,
		ColorDepth:    24,
		IndexedColors: 0,
	}
	got, err := ParsePicture(MarshalPicture(want))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("picture = %#v, want %#v", got, want)
	}
}

func TestParsePictureFillsImageProperties(t *testing.T) {
	t.Parallel()
	data, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLxLwAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatal(err)
	}
	thumbnail, err := ParsePicture(MarshalPicture(metadata.Thumbnail{Data: data, MIMEType: "image/png"}))
	if err != nil {
		t.Fatal(err)
	}
	if thumbnail.Width != 1 || thumbnail.Height != 1 {
		t.Fatalf("dimensions = %dx%d", thumbnail.Width, thumbnail.Height)
	}
}

func TestParsePictureRejectsTruncatedPayload(t *testing.T) {
	t.Parallel()
	if _, err := ParsePicture([]byte{0, 0, 0}); err == nil {
		t.Fatal("ParsePicture() error = nil")
	}
}
