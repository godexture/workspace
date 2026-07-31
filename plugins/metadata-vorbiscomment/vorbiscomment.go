package vorbiscomment

import (
	"encoding/binary"
	"errors"
	"strconv"
	"strings"

	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/sdk/date"
)

const metadataFieldKey = "flac.vorbiscomment.field"

const defaultVendor = "godexture/format-flac"

func Parse(payload []byte, bundle *metadata.Bundle) error {
	if bundle == nil {
		return errors.New("vorbis comment bundle is nil")
	}

	offset := 0
	vendor, err := readString(payload, &offset)
	if err != nil {
		return err
	}
	bundle.SetNonZeroIfEmpty(metadata.KeyEncoder(vendor))

	count, err := readUint32(payload, &offset)
	if err != nil {
		return err
	}
	for range count {
		comment, err := readString(payload, &offset)
		if err != nil {
			return err
		}
		applyComment(bundle, comment)
	}
	if offset != len(payload) {
		return errors.New("vorbis comment has trailing data")
	}
	return nil
}

func Marshal(bundle metadata.Bundle) []byte {
	vendor := string(metadata.Get[metadata.KeyEncoder](&bundle))
	if vendor == "" {
		vendor = defaultVendor
	}

	comments := make([]string, 0, 16)
	add := func(name, value string) {
		if value != "" {
			comments = append(comments, name+"="+value)
		}
	}
	add("TITLE", string(metadata.Get[metadata.KeyTitle](&bundle)))
	add("ALBUM", string(metadata.Get[metadata.KeyAlbum](&bundle)))
	for _, artist := range metadata.Enumerate[metadata.KeyArtist](&bundle) {
		add("ARTIST", string(artist))
	}
	add("COMPOSER", string(metadata.Get[metadata.KeyComposer](&bundle)))
	add("GENRE", string(metadata.Get[metadata.KeyGenre](&bundle)))
	if value := date.Partial(metadata.Get[metadata.KeyDate](&bundle)); value.HasValue() {
		add("DATE", value.ToISOString())
	}
	add("COMMENT", string(metadata.Get[metadata.KeyComment](&bundle)))
	add("COPYRIGHT", string(metadata.Get[metadata.KeyCopyright](&bundle)))
	add("LICENSE", string(metadata.Get[metadata.KeyLicense](&bundle)))
	for _, website := range metadata.Enumerate[metadata.KeyWebsite](&bundle) {
		add("CONTACT", string(website))
	}
	addNumberComment(&comments, "TRACKNUMBER", "TRACKTOTAL", int64(metadata.Get[metadata.KeyTrackNumber](&bundle)), int64(metadata.Get[metadata.KeyTotalTracks](&bundle)))
	addNumberComment(&comments, "DISCNUMBER", "DISCTOTAL", int64(metadata.Get[metadata.KeyDiscNumber](&bundle)), int64(metadata.Get[metadata.KeyTotalDiscs](&bundle)))
	if raw, ok := bundle.GetRaw(metadataFieldKey); ok {
		for _, field := range raw {
			comments = append(comments, string(field))
		}
	}

	data := make([]byte, 0, len(vendor)+8+len(comments)*16)
	data = appendString(data, vendor)
	data = binary.LittleEndian.AppendUint32(data, uint32(len(comments)))
	for _, comment := range comments {
		data = appendString(data, comment)
	}
	return data
}

func applyComment(bundle *metadata.Bundle, comment string) {
	field, value, found := strings.Cut(comment, "=")
	if !found {
		bundle.AddRaw(metadataFieldKey, []byte(comment))
		return
	}

	switch strings.ToUpper(field) {
	case "TITLE":
		bundle.SetNonZero(metadata.KeyTitle(value))
	case "ALBUM":
		bundle.SetNonZero(metadata.KeyAlbum(value))
	case "ARTIST":
		bundle.PushBack(metadata.KeyArtist(value))
	case "COMPOSER":
		bundle.SetNonZero(metadata.KeyComposer(value))
	case "GENRE":
		bundle.SetNonZero(metadata.KeyGenre(value))
	case "DATE":
		parsed, err := date.NewPartial(value)
		if err != nil {
			bundle.AddRaw(metadataFieldKey, []byte(comment))
			return
		}
		bundle.SetNonZero(metadata.KeyDate(parsed))
	case "COMMENT":
		bundle.SetNonZero(metadata.KeyComment(value))
	case "COPYRIGHT":
		bundle.SetNonZero(metadata.KeyCopyright(value))
	case "LICENSE":
		bundle.SetNonZero(metadata.KeyLicense(value))
	case "CONTACT":
		bundle.PushBack(metadata.KeyWebsite(value))
	case "TRACKNUMBER":
		applyNumberComment(bundle, comment, value, true)
	case "TRACKTOTAL", "TOTALTRACKS":
		applyTotalComment(bundle, comment, value, true)
	case "DISCNUMBER":
		applyNumberComment(bundle, comment, value, false)
	case "DISCTOTAL", "TOTALDISCS":
		applyTotalComment(bundle, comment, value, false)
	default:
		bundle.AddRaw(metadataFieldKey, []byte(comment))
	}
}

func applyNumberComment(bundle *metadata.Bundle, comment, value string, track bool) {
	number, total, ok := splitNumberTotal(value)
	if !ok {
		bundle.AddRaw(metadataFieldKey, []byte(comment))
		return
	}
	if track {
		bundle.SetNonZero(metadata.KeyTrackNumber(number))
		bundle.SetNonZero(metadata.KeyTotalTracks(total))
		return
	}
	bundle.SetNonZero(metadata.KeyDiscNumber(number))
	bundle.SetNonZero(metadata.KeyTotalDiscs(total))
}

func applyTotalComment(bundle *metadata.Bundle, comment, value string, track bool) {
	total, err := strconv.ParseInt(value, 10, 64)
	if err != nil || total <= 0 {
		bundle.AddRaw(metadataFieldKey, []byte(comment))
		return
	}
	if track {
		if metadata.Get[metadata.KeyTotalTracks](bundle) == 0 {
			bundle.Set(metadata.KeyTotalTracks(total))
		}
		return
	}
	if metadata.Get[metadata.KeyTotalDiscs](bundle) == 0 {
		bundle.Set(metadata.KeyTotalDiscs(total))
	}
}

func splitNumberTotal(value string) (number, total int64, ok bool) {
	numberText, totalText, hasTotal := strings.Cut(value, "/")
	var err error
	number, err = strconv.ParseInt(numberText, 10, 64)
	if err != nil || number <= 0 {
		return 0, 0, false
	}
	if !hasTotal || totalText == "" {
		return number, 0, true
	}
	total, err = strconv.ParseInt(totalText, 10, 64)
	if err != nil || total <= 0 {
		return 0, 0, false
	}
	return number, total, true
}

func addNumberComment(comments *[]string, name, totalName string, number, total int64) {
	if number <= 0 {
		if total > 0 {
			*comments = append(*comments, totalName+"="+strconv.FormatInt(total, 10))
		}
		return
	}
	value := strconv.FormatInt(number, 10)
	if total > 0 {
		value += "/" + strconv.FormatInt(total, 10)
	}
	*comments = append(*comments, name+"="+value)
}

func readUint32(data []byte, offset *int) (uint32, error) {
	if len(data)-*offset < 4 {
		return 0, errors.New("truncated vorbis comment length")
	}
	value := binary.LittleEndian.Uint32(data[*offset : *offset+4])
	*offset += 4
	return value, nil
}

func readString(data []byte, offset *int) (string, error) {
	length, err := readUint32(data, offset)
	if err != nil {
		return "", err
	}
	if uint64(length) > uint64(len(data)-*offset) {
		return "", errors.New("truncated vorbis comment string")
	}
	end := *offset + int(length)
	value := string(data[*offset:end])
	*offset = end
	return value, nil
}

func appendString(data []byte, value string) []byte {
	data = binary.LittleEndian.AppendUint32(data, uint32(len(value)))
	return append(data, value...)
}
