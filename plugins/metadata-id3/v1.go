package id3

import (
	"bytes"

	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/sdk/date"
)

func parseTrailingV1(buffer []byte, bundle *metadata.Bundle) {
	if !HasV1Tag(buffer) {
		return
	}

	tag := buffer[len(buffer)-V1TagSize:]
	bundle.SetNonZeroIfEmpty(metadata.KeyTitle(decodeV1String(tag[3:33])))
	bundle.SetNonZeroIfEmpty(metadata.KeyArtist(decodeV1String(tag[33:63])))
	bundle.SetNonZeroIfEmpty(metadata.KeyAlbum(decodeV1String(tag[63:93])))
	yearStr := decodeV1String(tag[93:97])
	if yearStr != "" {
		if d, err := date.NewPartial(yearStr); err == nil {
			bundle.SetNonZeroIfEmpty(metadata.KeyDate(d))
		}
	}

	commentField := tag[97:127]
	if len(commentField) == 30 && commentField[28] == 0 && commentField[29] != 0 {
		bundle.SetNonZeroIfEmpty(metadata.KeyComment(decodeV1String(commentField[:28])))
		bundle.SetNonZeroIfEmpty(metadata.KeyTrackNumber(commentField[29]))
	} else {
		bundle.SetNonZeroIfEmpty(metadata.KeyComment(decodeV1String(commentField)))
	}

	if genre := decodeV1Genre(tag[127]); genre != "" {
		bundle.SetNonZeroIfEmpty(metadata.KeyGenre(genre))
	}
}

func decodeV1String(payload []byte) string {
	return trimString(latin1ToUTF8(bytes.TrimRight(payload, "\x00")))
}

func decodeV1Genre(index byte) string {
	if int(index) >= len(v1Genres) {
		return ""
	}
	return v1Genres[index]
}

var v1Genres = []string{
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
