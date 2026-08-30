package vorbiscomment

import (
	"strconv"
	"strings"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/plugin"
)

func vcFieldSyntax(value []byte) bool {
	name, _, found := strings.Cut(string(value), "=")
	if !found || name == "" {
		return false
	}
	for _, byteValue := range []byte(name) {
		if byteValue < 0x20 || byteValue > 0x7e || byteValue == '=' {
			return false
		}
	}
	return true
}

func applyField(builder *metadata.Builder, slot carrier.ID, encoding plugin.Identity, root metadata.BlockID, field string) (int, bool) {
	name, value, _ := strings.Cut(field, "=")
	origin := metadata.Origin{Carrier: slot, Encoding: encoding, Block: root, Native: name}
	switch strings.ToUpper(name) {
	case "TITLE":
		metadata.Add(builder, tag.Title(), value, origin)
	case "ARTIST":
		metadata.Add(builder, tag.Artist(), value, origin)
	case "ALBUM":
		metadata.Add(builder, tag.Album(), value, origin)
	case "COMPOSER":
		metadata.Add(builder, tag.Composer(), value, origin)
	case "GENRE":
		metadata.Add(builder, tag.Genre(), value, origin)
	case "COMMENT":
		metadata.Add(builder, tag.Comment(), value, origin)
	case "COPYRIGHT":
		metadata.Add(builder, tag.Copyright(), value, origin)
	case "LICENSE":
		metadata.Add(builder, tag.License(), value, origin)
	case "ENCODER":
		metadata.Add(builder, tag.Encoder(), value, origin)
	case "LYRICS":
		metadata.Add(builder, tag.Lyrics(), value, origin)
	case "DATE":
		parsed, err := tag.ParseDate(value)
		if err != nil || parsed.ToISOString() != value {
			return 0, false
		}
		metadata.Add(builder, tag.Date(), parsed, origin)
	case "TRACKNUMBER":
		number, total, hasTotal, ok := vcNumberPair(value)
		if !ok {
			return 0, false
		}
		metadata.Add(builder, tag.TrackNumber(), number, origin)
		if hasTotal {
			metadata.Add(builder, tag.TotalTracks(), total, origin)
			return 2, true
		}
	case "TRACKTOTAL", "TOTALTRACKS":
		total, ok := vcUnsigned(value)
		if !ok {
			return 0, false
		}
		metadata.Add(builder, tag.TotalTracks(), total, origin)
	case "DISCNUMBER":
		number, total, hasTotal, ok := vcNumberPair(value)
		if !ok {
			return 0, false
		}
		metadata.Add(builder, tag.DiscNumber(), number, origin)
		if hasTotal {
			metadata.Add(builder, tag.TotalDiscs(), total, origin)
			return 2, true
		}
	case "DISCTOTAL", "TOTALDISCS":
		total, ok := vcUnsigned(value)
		if !ok {
			return 0, false
		}
		metadata.Add(builder, tag.TotalDiscs(), total, origin)
	case "METADATA_BLOCK_PICTURE":
		picture, ok := vcDecodePicture(value)
		if !ok {
			return 0, false
		}
		metadata.Add(builder, tag.Picture(), picture, origin)
	default:
		return 0, false
	}
	return 1, true
}

func vcKnownField(field string) bool {
	name, value, found := strings.Cut(field, "=")
	if !found {
		return false
	}
	switch strings.ToUpper(name) {
	case "TITLE", "ARTIST", "ALBUM", "COMPOSER", "GENRE", "COMMENT", "COPYRIGHT", "LICENSE", "ENCODER", "LYRICS":
		return true
	case "DATE":
		parsed, err := tag.ParseDate(value)
		return err == nil && parsed.ToISOString() == value
	case "TRACKNUMBER", "DISCNUMBER":
		_, _, _, ok := vcNumberPair(value)
		return ok
	case "TRACKTOTAL", "TOTALTRACKS", "DISCTOTAL", "TOTALDISCS":
		_, ok := vcUnsigned(value)
		return ok
	case "METADATA_BLOCK_PICTURE":
		_, ok := vcDecodePicture(value)
		return ok
	}
	return false
}

func vcNumberPair(value string) (int64, int64, bool, bool) {
	numberText, totalText, hasTotal := strings.Cut(value, "/")
	if hasTotal && strings.Contains(totalText, "/") {
		return 0, 0, false, false
	}
	number, ok := vcUnsigned(numberText)
	if !ok {
		return 0, 0, false, false
	}
	if !hasTotal {
		return number, 0, false, true
	}
	total, ok := vcUnsigned(totalText)
	return number, total, true, ok
}

func vcUnsigned(value string) (int64, bool) {
	if value == "" {
		return 0, false
	}
	for _, rune := range value {
		if rune < '0' || rune > '9' {
			return 0, false
		}
	}
	parsed, err := strconv.ParseUint(value, 10, 63)
	return int64(parsed), err == nil
}

func vcTextField(entry metadata.Entry) (string, string, bool) {
	value, ok := entry.Value().(string)
	if !ok {
		return "", "", false
	}
	switch entry.Key() {
	case tag.Title().ID():
		return "TITLE", value, true
	case tag.Artist().ID():
		return "ARTIST", value, true
	case tag.Album().ID():
		return "ALBUM", value, true
	case tag.Composer().ID():
		return "COMPOSER", value, true
	case tag.Genre().ID():
		return "GENRE", value, true
	case tag.Comment().ID():
		return "COMMENT", value, true
	case tag.Copyright().ID():
		return "COPYRIGHT", value, true
	case tag.License().ID():
		return "LICENSE", value, true
	case tag.Encoder().ID():
		return "ENCODER", value, true
	case tag.Lyrics().ID():
		return "LYRICS", value, true
	}
	return "", "", false
}
