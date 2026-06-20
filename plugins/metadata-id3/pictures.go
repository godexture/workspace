package id3

import (
	"bytes"
	"strings"

	"github.com/godexture/core/domain/metadata"
)

func decodeAPICFrame(frameData []byte) metadata.Thumbnail {
	if len(frameData) < 4 {
		return metadata.Thumbnail{}
	}

	encoding := frameData[0]
	mimeEnd := bytes.IndexByte(frameData[1:], 0)
	if mimeEnd < 0 {
		return metadata.Thumbnail{}
	}
	mimeStart := 1
	mimeEnd += mimeStart
	if mimeEnd+2 > len(frameData) {
		return metadata.Thumbnail{}
	}

	pictureType := frameData[mimeEnd+1]
	descriptionStart := mimeEnd + 2
	descriptionEnd := findEncodedTerminator(frameData[descriptionStart:], encoding)
	if descriptionEnd < 0 {
		return metadata.Thumbnail{}
	}
	descriptionBytesEnd := descriptionStart + descriptionEnd
	dataStart := descriptionBytesEnd + terminatorSize(encoding)
	if dataStart > len(frameData) {
		return metadata.Thumbnail{}
	}

	return metadata.Thumbnail{
		Data:        append([]byte(nil), frameData[dataStart:]...),
		MIMEType:    trimString(string(frameData[mimeStart:mimeEnd])),
		PictureType: metadata.PictureType(pictureType),
		Description: decodeEncodedText(encoding, frameData[descriptionStart:descriptionBytesEnd]),
	}
}

func decodePICFrame(frameData []byte) metadata.Thumbnail {
	if len(frameData) < 6 {
		return metadata.Thumbnail{}
	}

	encoding := frameData[0]
	mimeType := picFormatToMIME(frameData[1:4])
	pictureType := frameData[4]
	descriptionStart := 5
	descriptionEnd := findEncodedTerminator(frameData[descriptionStart:], encoding)
	if descriptionEnd < 0 {
		return metadata.Thumbnail{}
	}
	descriptionBytesEnd := descriptionStart + descriptionEnd
	dataStart := descriptionBytesEnd + terminatorSize(encoding)
	if dataStart > len(frameData) {
		return metadata.Thumbnail{}
	}

	return metadata.Thumbnail{
		Data:        append([]byte(nil), frameData[dataStart:]...),
		MIMEType:    mimeType,
		PictureType: metadata.PictureType(pictureType),
		Description: decodeEncodedText(encoding, frameData[descriptionStart:descriptionBytesEnd]),
	}
}

func addThumbnail(bundle *metadata.Bundle, thumbnail metadata.Thumbnail) {
	if len(thumbnail.Data) == 0 {
		return
	}

	thumbnails := metadata.Get[metadata.KeyThumbnail](bundle)
	thumbnail.Data = append([]byte(nil), thumbnail.Data...)
	thumbnails = append(thumbnails, thumbnail)
	bundle.Set(metadata.KeyThumbnail(thumbnails))
}

func picFormatToMIME(format []byte) string {
	switch strings.ToUpper(string(format)) {
	case "PNG":
		return "image/png"
	case "JPG":
		return "image/jpeg"
	default:
		return trimString(string(format))
	}
}
