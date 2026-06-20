package id3

import (
	"bytes"
	"log"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

func decodeTextFrame(frameData []byte) string {
	if len(frameData) == 0 {
		return ""
	}
	text := decodeEncodedText(frameData[0], frameData[1:])
	if idx := strings.IndexByte(text, 0); idx >= 0 {
		text = text[:idx]
	}
	return strings.TrimSpace(text)
}

func decodeEncodedTextFrame(frameData []byte) string {
	if len(frameData) < 4 {
		return ""
	}

	encoding := frameData[0]
	payload := frameData[4:]
	textStart := skipEncodedString(payload, encoding)
	if textStart >= len(payload) {
		return ""
	}
	return strings.TrimSpace(decodeEncodedText(encoding, payload[textStart:]))
}

func decodeTrackOrDiskNumberFrame(frameData []byte) (trackNumber int64, totalTracks int64) {
	text := decodeTextFrame(frameData)
	if text == "" {
		return 0, 0
	}

	track, total, _ := strings.Cut(text, "/")

	trackNumber, _ = strconv.ParseInt(track, 10, 64)
	totalTracks, _ = strconv.ParseInt(total, 10, 64)

	return trackNumber, totalTracks
}

func decodeURLFrame(frameData []byte) string {
	return trimString(string(bytes.Trim(frameData, "\x00")))
}

func decodeUserURLValue(frameData []byte) string {
	if len(frameData) < 2 {
		return ""
	}
	encoding := frameData[0]
	payload := frameData[1:]
	valueStart := skipEncodedString(payload, encoding)
	if valueStart >= len(payload) {
		return ""
	}
	return trimString(string(bytes.Trim(payload[valueStart:], "\x00")))
}

func skipEncodedString(payload []byte, encoding byte) int {
	switch encoding {
	case 1, 2:
		for i := 0; i+1 < len(payload); i += 2 {
			if payload[i] == 0 && payload[i+1] == 0 {
				return i + 2
			}
		}
	default:
		for i, b := range payload {
			if b == 0 {
				return i + 1
			}
		}
	}
	return len(payload)
}

func findEncodedTerminator(payload []byte, encoding byte) int {
	switch encoding {
	case 1, 2:
		for i := 0; i+1 < len(payload); i += 2 {
			if payload[i] == 0 && payload[i+1] == 0 {
				return i
			}
		}
	default:
		for i, b := range payload {
			if b == 0 {
				return i
			}
		}
	}
	return -1
}

func terminatorSize(encoding byte) int {
	switch encoding {
	case 1, 2:
		return 2
	default:
		return 1
	}
}

func decodeEncodedText(encoding byte, payload []byte) string {
	switch encoding {
	case 0:
		return trimString(latin1ToUTF8(payload))
	case 1:
		return trimString(decodeUTF16(payload, true))
	case 2:
		return trimString(decodeUTF16(payload, false))
	case 3:
		if utf8.Valid(payload) {
			return trimString(string(payload))
		}
	}
	return trimString(string(bytes.Trim(payload, "\x00")))
}

func decodeUTF16(payload []byte, withBOM bool) string {
	if len(payload) == 0 {
		return ""
	}

	order := utf16BigEndian
	start := 0
	if withBOM && len(payload) >= 2 {
		if payload[0] == 0xFF && payload[1] == 0xFE {
			order = utf16LittleEndian
			start = 2
		} else if payload[0] == 0xFE && payload[1] == 0xFF {
			start = 2
		}
	}

	u16 := make([]uint16, 0, (len(payload)-start)/2)
	for i := start; i+1 < len(payload); i += 2 {
		u16 = append(u16, order(payload[i:i+2]))
	}
	return string(utf16.Decode(u16))
}

func trimString(value string) string {
	return strings.Trim(value, "\x00 \t\r\n")
}

func latin1ToUTF8(payload []byte) string {
	runes := make([]rune, len(payload))
	for i, b := range payload {
		runes[i] = rune(b)
	}
	return string(runes)
}

func removeUnsynchronisation(payload []byte) []byte {
	out := make([]byte, 0, len(payload))
	for i := 0; i < len(payload); i++ {
		if i+1 < len(payload) && payload[i] == 0xFF && payload[i+1] == 0x00 {
			out = append(out, 0xFF)
			i++
			continue
		}
		out = append(out, payload[i])
	}
	return out
}

func utf16BigEndian(payload []byte) uint16 {
	return uint16(payload[0])<<8 | uint16(payload[1])
}

func utf16LittleEndian(payload []byte) uint16 {
	return uint16(payload[1])<<8 | uint16(payload[0])
}

func encodeText(value string, targetEncoding Encoding, version Version) (byte, []byte) {
	if targetEncoding == EncodingDefault {
		if version == Version2v4 {
			targetEncoding = EncodingUTF8
		} else {
			targetEncoding = EncodingUTF16
		}
	}

	if version < Version2v4 {
		if targetEncoding == EncodingUTF8 || targetEncoding == EncodingUTF16BE {
			var encodingName string
			switch targetEncoding {
			case EncodingUTF16BE:
				encodingName = "UTF-16BE"
			case EncodingUTF8:
				encodingName = "UTF-8"
			}
			log.Printf("WARNING: ID3v2.%d does not support encoding %s, processing as requested anyway\n", version, encodingName)
		}
	}

	var tagEncodingByte byte
	switch targetEncoding {
	case EncodingISO88591:
		tagEncodingByte = 0
	case EncodingUTF16:
		tagEncodingByte = 1
	case EncodingUTF16BE:
		tagEncodingByte = 2
	case EncodingUTF8:
		tagEncodingByte = 3
	default:
		tagEncodingByte = 3
	}

	switch targetEncoding {
	case EncodingISO88591:
		var buf []byte
		for _, r := range value {
			if r <= 0xFF {
				buf = append(buf, byte(r))
			} else {
				buf = append(buf, '?')
			}
		}
		return tagEncodingByte, buf

	case EncodingUTF16:
		u16 := utf16.Encode([]rune(value))
		buf := make([]byte, 2+len(u16)*2)
		buf[0] = 0xFF
		buf[1] = 0xFE
		for i, u := range u16 {
			buf[2+i*2] = byte(u & 0xFF)
			buf[2+i*2+1] = byte(u >> 8)
		}
		return tagEncodingByte, buf

	case EncodingUTF16BE:
		u16 := utf16.Encode([]rune(value))
		buf := make([]byte, len(u16)*2)
		for i, u := range u16 {
			buf[i*2] = byte(u >> 8)
			buf[i*2+1] = byte(u & 0xFF)
		}
		return tagEncodingByte, buf

	case EncodingUTF8:
		return tagEncodingByte, []byte(value)
	}

	return 3, []byte(value)
}
