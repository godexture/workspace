package id3v2

import (
	"strings"

	"github.com/godexture/godec/core/domain/metadata"
)

func (e *encoder) addAttachedPictureFrames(thumbnails []metadata.Thumbnail) {
	for _, thumbnail := range thumbnails {
		e.addAttachedPictureFrame(thumbnail)
	}
}

func (e *encoder) addAttachedPictureFrame(thumbnail metadata.Thumbnail) {
	if len(thumbnail.Data) == 0 {
		return
	}

	mimeType := strings.TrimSpace(thumbnail.MIMEType)
	if mimeType == "" {
		mimeType = metadata.DetectImageMIME(thumbnail.Data)
	}

	pictureType := byte(thumbnail.PictureType)
	description := strings.TrimSpace(thumbnail.Description)

	encByte, descBytes := encodeText(description, e.opts.Encoding, e.opts.Version)

	var payload []byte
	var mappedID string

	if e.opts.Version == Version2 {
		format := mimeToFormat(mimeType)
		payload = []byte{encByte}
		payload = append(payload, []byte(format)...)
		payload = append(payload, pictureType)
		payload = append(payload, descBytes...)
		payload = append(payload, e.getTerminator(encByte)...)
		payload = append(payload, thumbnail.Data...)
		mappedID = "PIC"
	} else {
		payload = []byte{encByte}
		payload = append(payload, []byte(mimeType)...)
		payload = append(payload, 0x00, pictureType)
		payload = append(payload, descBytes...)
		payload = append(payload, e.getTerminator(encByte)...)
		payload = append(payload, thumbnail.Data...)
		mappedID = "APIC"
	}

	e.frames = append(e.frames, e.buildFrame(mappedID, payload))
}

func (e *encoder) addRawAttachedPictureFrame(payload []byte) {
	if len(payload) == 0 {
		return
	}
	if e.opts.Version == Version2 {
		thumb := decodeAPICFrame(payload)
		if len(thumb.Data) > 0 {
			e.addAttachedPictureFrame(thumb)
			return
		}
		// An APIC payload cannot be emitted under PIC without converting its
		// MIME and description fields to the ID3v2.2 layout.
		return
	}
	mappedID := e.mapFrameID("APIC")
	e.frames = append(e.frames, e.buildFrame(mappedID, payload))
}

func mimeToFormat(mime string) string {
	mime = strings.ToLower(mime)
	if strings.Contains(mime, "png") {
		return "PNG"
	}
	if strings.Contains(mime, "jpeg") || strings.Contains(mime, "jpg") {
		return "JPG"
	}
	parts := strings.Split(mime, "/")
	if len(parts) > 1 {
		format := strings.ToUpper(parts[1])
		if len(format) > 3 {
			format = format[:3]
		}
		if len(format) == 3 {
			return format
		}
	}
	return "   "
}
