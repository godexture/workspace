package id3v1

import (
	"fmt"
	"strings"

	"github.com/godexture/godec/core/domain/metadata"
	"github.com/godexture/godec/sdk/date"
)

func Marshal(bundle metadata.Bundle) ([]byte, error) {
	tag := make([]byte, TagSize)
	copy(tag[0:3], tagHeader)

	title := metadata.Get[metadata.KeyTitle](&bundle)
	copy(tag[3:33], encodeString(string(title), 30))

	artists := metadata.Enumerate[metadata.KeyArtist](&bundle)
	var artist string
	if len(artists) > 0 {
		artist = string(artists[0])
	}
	copy(tag[33:63], encodeString(string(artist), 30))

	album := metadata.Get[metadata.KeyAlbum](&bundle)
	copy(tag[63:93], encodeString(string(album), 30))

	dateVal := date.Partial(metadata.Get[metadata.KeyDate](&bundle))
	if dateVal.HasValue() && dateVal.Year().Exists() {
		yearStr := fmt.Sprintf("%04d", dateVal.Year().Unwrap())
		copy(tag[93:97], encodeString(yearStr, 4))
	}

	comment := metadata.Get[metadata.KeyComment](&bundle)
	trackNum := metadata.Get[metadata.KeyTrackNumber](&bundle)

	if trackNum > 0 && trackNum <= 255 {
		copy(tag[97:125], encodeString(string(comment), 28))
		tag[125] = 0 // ID3v1.1 indicator
		tag[126] = byte(trackNum)
	} else {
		copy(tag[97:127], encodeString(string(comment), 30))
	}

	genreStr := string(metadata.Get[metadata.KeyGenre](&bundle))
	tag[127] = encodeGenre(genreStr)

	isEmpty := len(title) == 0 && len(artist) == 0 && len(album) == 0 &&
		(!dateVal.HasValue() || !dateVal.Year().Exists()) &&
		len(comment) == 0 && trackNum == 0 && tag[127] == 255

	if isEmpty {
		return nil, nil
	}

	return tag, nil
}

func encodeString(s string, size int) []byte {
	b := make([]byte, 0, size)
	for _, r := range s {
		if len(b) >= size {
			break
		}
		if r <= 0xFF {
			b = append(b, byte(r))
		} else {
			b = append(b, '?')
		}
	}
	return b
}

func encodeGenre(genre string) byte {
	if genre == "" {
		return 255
	}
	for i, g := range genres {
		if strings.EqualFold(g, genre) {
			return byte(i)
		}
	}
	return 255
}
