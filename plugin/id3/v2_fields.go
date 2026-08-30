package id3

import (
	"bytes"
	"encoding/binary"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/plugin"
)

func v2ApplyFrame(builder *metadata.Builder, slot carrier.ID, encoding plugin.Identity, root metadata.BlockID, version byte, frameID string, data []byte) (int, bool) {
	origin := metadata.Origin{Carrier: slot, Encoding: encoding, Block: root, Native: frameID}
	if declaration, ok := v2StringKey(frameID); ok {
		values, ok := v2DecodeText(version, data)
		if !ok {
			return 0, false
		}
		for _, value := range values {
			metadata.Add(builder, declaration, value, origin)
		}
		return len(values), true
	}
	switch frameID {
	case "WXXX", "WXX":
		value, ok := v2DecodeUserURL(version, data)
		if !ok {
			return 0, false
		}
		metadata.Add(builder, tag.Website(), value, origin)
		return 1, true
	case "TDRC":
		if version != 4 {
			return 0, false
		}
		values, ok := v2DecodeText(version, data)
		if !ok {
			return 0, false
		}
		dates := make([]tag.PartialDate, len(values))
		for index, value := range values {
			parsed, ok := v2ParseTDRC(value)
			if !ok {
				return 0, false
			}
			dates[index] = parsed
		}
		for _, value := range dates {
			metadata.Add(builder, tag.Date(), value, origin)
		}
		return len(dates), true
	case "TRCK", "TRK":
		return v2AddOrdinalPairs(builder, tag.TrackNumber(), tag.TotalTracks(), origin, version, data)
	case "TPOS", "TPA":
		return v2AddOrdinalPairs(builder, tag.DiscNumber(), tag.TotalDiscs(), origin, version, data)
	case "COMM", "COM":
		value, ok := v2DecodeQualifiedText(version, data)
		if !ok {
			return 0, false
		}
		metadata.Add(builder, tag.Comment(), value, origin)
		return 1, true
	case "USLT", "ULT":
		value, ok := v2DecodeQualifiedText(version, data)
		if !ok {
			return 0, false
		}
		metadata.Add(builder, tag.Lyrics(), value, origin)
		return 1, true
	}
	return 0, false
}

func v2StringKey(frameID string) (key.Key[string], bool) {
	switch frameID {
	case "TIT2", "TT2":
		return tag.Title(), true
	case "TPE1", "TP1":
		return tag.Artist(), true
	case "TALB", "TAL":
		return tag.Album(), true
	case "TCON", "TCO":
		return tag.Genre(), true
	case "TCOM", "TCM":
		return tag.Composer(), true
	case "TCOP", "TCR":
		return tag.Copyright(), true
	case "TENC", "TEN":
		return tag.Encoder(), true
	}
	return key.Key[string]{}, false
}

func v2ApplyPicture(builder *metadata.Builder, slot carrier.ID, encoding plugin.Identity, root metadata.BlockID, version byte, frameID string, data metadata.Blob) bool {
	var (
		picture tag.Artwork
		ok      bool
	)
	switch frameID {
	case "APIC":
		if version < 3 {
			return false
		}
		picture, ok = v2DecodeAPIC(version, data)
	case "PIC":
		if version != 2 {
			return false
		}
		picture, ok = v2DecodePIC(data)
	default:
		return false
	}
	if !ok {
		return false
	}
	metadata.Add(builder, tag.Picture(), picture, metadata.Origin{Carrier: slot, Encoding: encoding, Block: root, Native: frameID})
	return true
}

func v2DecodeAPIC(version byte, data metadata.Blob) (tag.Artwork, bool) {
	header, ok := v2Read(data, 0, 2)
	if !ok {
		return tag.Artwork{}, false
	}
	encoding := header[0]
	mimeEnd, ok := v2BlobByte(data, 1, 0)
	if !ok || mimeEnd == 1 || mimeEnd+1 >= data.Len() {
		return tag.Artwork{}, false
	}
	mime, ok := v2Read(data, 1, mimeEnd-1)
	if !ok || !tag.IsImageMediaType(string(mime)) {
		return tag.Artwork{}, false
	}
	pictureType, ok := v2Read(data, mimeEnd+1, 1)
	if !ok || pictureType[0] > 0x14 {
		return tag.Artwork{}, false
	}
	description, imageStart, ok := v2PictureDescription(version, encoding, data, mimeEnd+2)
	if !ok || imageStart == data.Len() {
		return tag.Artwork{}, false
	}
	image, ok := data.Slice(string(mime), imageStart, data.Len())
	if !ok {
		return tag.Artwork{}, false
	}
	return tag.Artwork{Data: image, MediaType: string(mime), Type: tag.ArtworkType(pictureType[0]), Description: description}, true
}

func v2DecodePIC(data metadata.Blob) (tag.Artwork, bool) {
	header, ok := v2Read(data, 0, 5)
	if !ok {
		return tag.Artwork{}, false
	}
	mediaType := ""
	switch string(header[1:4]) {
	case "PNG":
		mediaType = "image/png"
	case "JPG":
		mediaType = "image/jpeg"
	default:
		return tag.Artwork{}, false
	}
	if header[4] > 0x14 {
		return tag.Artwork{}, false
	}
	description, imageStart, ok := v2PictureDescription(2, header[0], data, 5)
	if !ok || imageStart == data.Len() {
		return tag.Artwork{}, false
	}
	image, ok := data.Slice(mediaType, imageStart, data.Len())
	if !ok {
		return tag.Artwork{}, false
	}
	return tag.Artwork{Data: image, MediaType: mediaType, Type: tag.ArtworkType(header[4]), Description: description}, true
}

func v2PictureDescription(version, encoding byte, data metadata.Blob, start int) (string, int, bool) {
	width := 1
	if encoding == 1 || encoding == 2 {
		width = 2
	}
	end, ok := v2EncodedTerminator(data, start, width)
	if !ok {
		return "", 0, false
	}
	value, ok := v2Read(data, start, end-start)
	if !ok {
		return "", 0, false
	}
	description, ok := v2DecodeEncodedText(version, encoding, value)
	if !ok {
		return "", 0, false
	}
	return description, end + width, true
}

func v2BlobByte(data metadata.Blob, start int, want byte) (int, bool) {
	for offset := start; offset < data.Len(); {
		size := data.Len() - offset
		if size > 256 {
			size = 256
		}
		value, ok := v2Read(data, offset, size)
		if !ok {
			return 0, false
		}
		if index := bytes.IndexByte(value, want); index >= 0 {
			return offset + index, true
		}
		offset += size
	}
	return 0, false
}

func v2EncodedTerminator(data metadata.Blob, start, width int) (int, bool) {
	if width == 1 {
		return v2BlobByte(data, start, 0)
	}
	for offset := start; offset+1 < data.Len(); offset += 2 {
		value, ok := v2Read(data, offset, 2)
		if !ok {
			return 0, false
		}
		if value[0] == 0 && value[1] == 0 {
			return offset, true
		}
	}
	return 0, false
}

type v2OrdinalPair struct {
	number    int64
	hasNumber bool
	total     int64
	hasTotal  bool
}

func v2AddOrdinalPairs(builder *metadata.Builder, number key.Key[int64], total key.Key[int64], origin metadata.Origin, version byte, data []byte) (int, bool) {
	values, ok := v2DecodeText(version, data)
	if !ok {
		return 0, false
	}
	pairs := make([]v2OrdinalPair, len(values))
	for index, value := range values {
		pair, ok := v2ParseOrdinalPair(value)
		if !ok {
			return 0, false
		}
		pairs[index] = pair
	}
	units := 0
	for _, pair := range pairs {
		if pair.hasNumber {
			metadata.Add(builder, number, pair.number, origin)
			units++
		}
		if pair.hasTotal {
			metadata.Add(builder, total, pair.total, origin)
			units++
		}
	}
	return units, units != 0
}

func v2ParseOrdinalPair(value string) (v2OrdinalPair, bool) {
	first, second, hasSeparator := strings.Cut(value, "/")
	if hasSeparator && strings.Contains(second, "/") {
		return v2OrdinalPair{}, false
	}
	result := v2OrdinalPair{}
	if first != "" {
		parsed, ok := v2ParseOrdinal(first)
		if !ok {
			return v2OrdinalPair{}, false
		}
		result.number, result.hasNumber = parsed, true
	}
	if hasSeparator && second != "" {
		parsed, ok := v2ParseOrdinal(second)
		if !ok {
			return v2OrdinalPair{}, false
		}
		result.total, result.hasTotal = parsed, true
	}
	return result, result.hasNumber || result.hasTotal
}

func v2ParseOrdinal(value string) (int64, bool) {
	if !v2Digits(value) {
		return 0, false
	}
	parsed, err := strconv.ParseUint(value, 10, 63)
	return int64(parsed), err == nil
}

func v2DecodeText(version byte, value []byte) ([]string, bool) {
	if len(value) < 2 {
		return nil, false
	}
	text, ok := v2DecodeEncodedText(version, value[0], value[1:])
	if !ok {
		return nil, false
	}
	parts := strings.Split(text, "\x00")
	if len(parts) == 0 {
		return nil, false
	}
	for _, part := range parts {
		if part == "" {
			return nil, false
		}
	}
	if version < 4 && len(parts) != 1 {
		return nil, false
	}
	return parts, true
}

func v2DecodeQualifiedText(version byte, value []byte) (string, bool) {
	if len(value) < 5 || !bytes.Equal(value[1:4], []byte("XXX")) {
		return "", false
	}
	encoding := value[0]
	description, text, ok := v2SplitQualifiedText(version, encoding, value[4:])
	if !ok || description != "" || text == "" {
		return "", false
	}
	return text, true
}

func v2DecodeUserURL(version byte, value []byte) (string, bool) {
	if len(value) < 2 {
		return "", false
	}
	description, url, ok := v2SplitUserURL(version, value[0], value[1:])
	if !ok || description != "" || len(url) == 0 || bytes.IndexByte(url, 0) >= 0 {
		return "", false
	}
	runes := make([]rune, len(url))
	for index, byteValue := range url {
		runes[index] = rune(byteValue)
	}
	return string(runes), true
}

func v2SplitUserURL(version, encoding byte, value []byte) (string, []byte, bool) {
	step := 1
	if encoding == 1 || encoding == 2 {
		step = 2
	}
	for index := 0; index+step <= len(value); index += step {
		if step == 1 {
			if value[index] != 0 {
				continue
			}
			description, valid := v2DecodeEncodedText(version, encoding, value[:index])
			return description, append([]byte(nil), value[index+1:]...), valid
		}
		if value[index] != 0 || value[index+1] != 0 {
			continue
		}
		description, valid := v2DecodeEncodedText(version, encoding, value[:index])
		return description, append([]byte(nil), value[index+2:]...), valid
	}
	return "", nil, false
}

func v2SplitQualifiedText(version, encoding byte, value []byte) (string, string, bool) {
	step := 1
	if encoding == 1 || encoding == 2 {
		step = 2
	}
	for index := 0; index+step <= len(value); index += step {
		if step == 1 {
			if value[index] != 0 {
				continue
			}
			description, validDescription := v2DecodeEncodedText(version, encoding, value[:index])
			text, validText := v2DecodeEncodedText(version, encoding, value[index+1:])
			return description, text, validDescription && validText
		}
		if value[index] != 0 || value[index+1] != 0 {
			continue
		}
		description, validDescription := v2DecodeEncodedText(version, encoding, value[:index])
		text, validText := v2DecodeEncodedText(version, encoding, value[index+2:])
		return description, text, validDescription && validText
	}
	return "", "", false
}

func v2DecodeEncodedText(version, encoding byte, value []byte) (string, bool) {
	if version < 4 && encoding > 1 {
		return "", false
	}
	switch encoding {
	case 0:
		runes := make([]rune, len(value))
		for index, byteValue := range value {
			runes[index] = rune(byteValue)
		}
		return string(runes), true
	case 1:
		if len(value) < 2 {
			return "", false
		}
		little := false
		switch {
		case value[0] == 0xff && value[1] == 0xfe:
			little = true
		case value[0] == 0xfe && value[1] == 0xff:
		default:
			return "", false
		}
		return v2DecodeUTF16(value[2:], little)
	case 2:
		return v2DecodeUTF16(value, false)
	case 3:
		return string(value), utf8.Valid(value)
	}
	return "", false
}

func v2DecodeUTF16(value []byte, little bool) (string, bool) {
	if len(value)%2 != 0 {
		return "", false
	}
	units := make([]uint16, len(value)/2)
	for index := range units {
		if little {
			units[index] = binary.LittleEndian.Uint16(value[index*2:])
		} else {
			units[index] = binary.BigEndian.Uint16(value[index*2:])
		}
	}
	var result []rune
	for index := 0; index < len(units); index++ {
		unit := units[index]
		if unit >= 0xd800 && unit <= 0xdbff {
			if index+1 == len(units) || units[index+1] < 0xdc00 || units[index+1] > 0xdfff {
				return "", false
			}
			result = append(result, utf16.DecodeRune(rune(unit), rune(units[index+1])))
			index++
			continue
		}
		if unit >= 0xdc00 && unit <= 0xdfff {
			return "", false
		}
		result = append(result, rune(unit))
	}
	return string(result), true
}

func v2CanonicalText(value string) (string, bool) {
	result := strings.ToValidUTF8(value, "�")
	if strings.ContainsRune(result, 0) {
		result = strings.ReplaceAll(result, "\x00", "�")
	}
	return result, result != value
}

func v2EncodeLatin1(value string) ([]byte, bool) {
	result := make([]byte, 0, len(value))
	substituted := false
	for _, rune := range value {
		if rune == 0 || rune > 0xff {
			result = append(result, '?')
			substituted = true
			continue
		}
		result = append(result, byte(rune))
	}
	return result, substituted
}
