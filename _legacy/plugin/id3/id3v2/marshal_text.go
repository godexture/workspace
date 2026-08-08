package id3v2

import (
	"strings"
)

func (e *encoder) addTextFrame(frameID string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	encByte, valBytes := encodeText(value, e.opts.Encoding, e.opts.Version)
	payload := append([]byte{encByte}, valBytes...)
	mappedID := e.mapFrameID(frameID)
	e.frames = append(e.frames, e.buildFrame(mappedID, payload))
}

func (e *encoder) addURLFrame(frameID string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	mappedID := e.mapFrameID(frameID)
	e.frames = append(e.frames, e.buildFrame(mappedID, []byte(value)))
}

func (e *encoder) addUserURLFrame(description string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	encByte, descBytes := encodeText(description, e.opts.Encoding, e.opts.Version)
	payload := []byte{encByte}
	payload = append(payload, descBytes...)
	payload = append(payload, e.getTerminator(encByte)...)
	payload = append(payload, []byte(value)...)
	mappedID := e.mapFrameID("WXXX")
	e.frames = append(e.frames, e.buildFrame(mappedID, payload))
}

func (e *encoder) addCommentFrame(value string) {
	e.addLanguageTextFrame("COMM", value)
}

func (e *encoder) addLyricsFrame(value string) {
	e.addLanguageTextFrame("USLT", value)
}

func (e *encoder) addLanguageTextFrame(frameID, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	encByte, valBytes := encodeText(value, e.opts.Encoding, e.opts.Version)
	payload := []byte{encByte}
	payload = append(payload, []byte(commentLanguage)...)
	payload = append(payload, e.getTerminator(encByte)...)
	payload = append(payload, valBytes...)
	mappedID := e.mapFrameID(frameID)
	e.frames = append(e.frames, e.buildFrame(mappedID, payload))
}
