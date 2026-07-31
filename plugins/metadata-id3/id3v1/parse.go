package id3v1

import (
	"bytes"

	"github.com/godexture/godec/core/domain/metadata"
	"github.com/godexture/godec/plugins/metadata-id3/internal/id3text"
	"github.com/godexture/godec/sdk/date"
)

const (
	TagSize   = 128
	tagHeader = "TAG"
)

func HasTag(buffer []byte) bool {
	return len(buffer) >= TagSize && bytes.Equal(buffer[len(buffer)-TagSize:len(buffer)-TagSize+3], []byte(tagHeader))
}

func Parse(buffer []byte, bundle *metadata.Bundle) {
	if !HasTag(buffer) {
		return
	}

	tag := buffer[len(buffer)-TagSize:]
	bundle.SetNonZeroIfEmpty(metadata.KeyTitle(decodeString(tag[3:33])))
	artist := decodeString(tag[33:63])
	if artist != "" {
		bundle.PushBack(metadata.KeyArtist(artist))
	}
	bundle.SetNonZeroIfEmpty(metadata.KeyAlbum(decodeString(tag[63:93])))
	yearStr := decodeString(tag[93:97])
	if yearStr != "" {
		if d, err := date.NewPartial(yearStr); err == nil {
			bundle.SetNonZeroIfEmpty(metadata.KeyDate(d))
		}
	}

	commentField := tag[97:127]
	if len(commentField) == 30 && commentField[28] == 0 && commentField[29] != 0 {
		bundle.SetNonZeroIfEmpty(metadata.KeyComment(decodeString(commentField[:28])))
		bundle.SetNonZeroIfEmpty(metadata.KeyTrackNumber(commentField[29]))
	} else {
		bundle.SetNonZeroIfEmpty(metadata.KeyComment(decodeString(commentField)))
	}

	if genre := decodeGenre(tag[127]); genre != "" {
		bundle.SetNonZeroIfEmpty(metadata.KeyGenre(genre))
	}
}

func decodeString(payload []byte) string {
	return id3text.TrimString(id3text.Latin1ToUTF8(bytes.TrimRight(payload, "\x00")))
}

func decodeGenre(index byte) string {
	if int(index) >= len(genres) {
		return ""
	}
	return genres[index]
}

var genres = []string{
	"Blues", "Classic Rock", "Country", "Dance", "Disco", "Funk", "Grunge", "Hip-Hop",
	"Jazz", "Metal", "New Age", "Oldies", "Other", "Pop", "R&B", "Rap",
	"Reggae", "Rock", "Techno", "Industrial", "Alternative", "Ska", "Death Metal", "Pranks",
	"Soundtrack", "Euro-Techno", "Ambient", "Trip-Hop", "Vocal", "Jazz+Funk", "Fusion", "Trance",
	"Classical", "Instrumental", "Acid", "House", "Game", "Sound Clip", "Gospel", "Noise",
	"AlternRock", "Bass", "Soul", "Punk", "Space", "Meditative", "Instrumental Pop", "Instrumental Rock",
	"Ethnic", "Gothic", "Darkwave", "Techno-Industrial", "Electronic", "Pop-Folk", "Eurodance", "Dream",
	"Southern Rock", "Comedy", "Cult", "Gangsta", "Top 40", "Christian Rap", "Pop/Funk", "Jungle",
	"Native American", "Cabaret", "New Wave", "Psychadelic", "Rave", "Showtunes", "Trailer", "Lo-Fi",
	"Tribal", "Acid Punk", "Acid Jazz", "Polka", "Retro", "Musical", "Rock & Roll", "Hard Rock",
	"Folk", "Folk/Rock", "National Folk", "Swing", "Fast Fusion", "Bebob", "Latin", "Revival",
	"Celtic", "Bluegrass", "Avantgarde", "Gothic Rock", "Progressive Rock", "Psychedelic Rock", "Symphonic Rock", "Slow Rock",
	"Big Band", "Chorus", "Easy Listening", "Acoustic", "Humour", "Speech", "Chanson", "Opera",
	"Chamber Music", "Sonata", "Symphony", "Booty Bass", "Primus", "Porn Groove", "Satire", "Slow Jam",
	"Club", "Tango", "Samba", "Folklore", "Ballad", "Power Ballad", "Rhythmic Soul", "Freestyle",
	"Duet", "Punk Rock", "Drum Solo", "A capella", "Euro-House", "Dance Hall",
}
