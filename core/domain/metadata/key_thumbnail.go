package metadata

import (
	"bytes"
	"image"
	_ "image/jpeg"
	_ "image/png"
)

type PictureType uint8

const (
	PictureTypeOther      PictureType = 0x00
	PictureTypeFileIcon   PictureType = 0x01
	PictureTypeFrontCover PictureType = 0x03
	PictureTypeBackCover  PictureType = 0x04
)

type Thumbnail struct {
	Data        []byte
	MIMEType    string
	PictureType PictureType

	Description string

	Width         uint32
	Height        uint32
	ColorDepth    uint32
	IndexedColors uint32

	Extensions Extensions
}

func (t *Thumbnail) FillImageProperties() (*Thumbnail, error) {
	if len(t.Data) == 0 {
		return t, nil
	}

	config, format, err := image.DecodeConfig(bytes.NewReader(t.Data))
	if err != nil {
		return t, err
	}

	t.Width = uint32(config.Width)
	t.Height = uint32(config.Height)

	if t.MIMEType == "" {
		t.MIMEType = "image/" + format
	}

	return t, nil
}

// DetectImageMIME reports the MIME type of image data by decoding it, or
// "application/octet-stream" if no registered image decoder recognizes it.
func DetectImageMIME(data []byte) string {
	_, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "application/octet-stream"
	}
	return "image/" + format
}
