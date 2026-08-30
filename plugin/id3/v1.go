package id3

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/plugin"
)

const (
	v1Size = 128
	v1Tag  = "TAG"
)

var (
	errV1Malformed   = errors.New("malformed ID3v1 tag")
	errV1Unsupported = errors.New("unsupported ID3v1 metadata")
)

type v1Field uint8

const (
	v1Title v1Field = iota
	v1Artist
	v1Album
	v1Date
	v1Genre
	v1Comment
	v1Track
	v1FieldCount
)

func parseV1(ctx metadata.ParseContext) (metadata.Document, error) {
	return parseV1Document(ctx.Scope(), ctx.Carrier(), ctx.Encoding(), ctx.Block(), ctx.Payload())
}

func parseV1Document(scope metadata.Scope, slot carrier.ID, encoding plugin.Identity, block metadata.BlockID, payload metadata.Blob) (metadata.Document, error) {
	value := payload.AppendTo(nil)
	if len(value) != v1Size || string(value[:3]) != v1Tag {
		return metadata.Document{}, errV1Malformed
	}
	builder := metadata.NewBuilder(scope)
	builder.AddBlock(metadata.NewSourceBlock(block, slot, encoding, payload))
	origin := func(native string) metadata.Origin {
		return metadata.Origin{Carrier: slot, Encoding: encoding, Block: block, Native: native}
	}
	if text := decodeV1Text(value[3:33]); text != "" {
		metadata.Add(builder, tag.Title(), text, origin("title"))
	}
	if text := decodeV1Text(value[33:63]); text != "" {
		metadata.Add(builder, tag.Artist(), text, origin("artist"))
	}
	if text := decodeV1Text(value[63:93]); text != "" {
		metadata.Add(builder, tag.Album(), text, origin("album"))
	}
	if parsed, err := tag.ParseDate(decodeV1Text(value[93:97])); err == nil {
		metadata.Add(builder, tag.Date(), parsed, origin("year"))
	}
	comment := value[97:127]
	if comment[28] == 0 && comment[29] != 0 {
		if text := decodeV1Text(comment[:28]); text != "" {
			metadata.Add(builder, tag.Comment(), text, origin("comment"))
		}
		metadata.Add(builder, tag.TrackNumber(), int64(comment[29]), origin("track"))
	} else if text := decodeV1Text(comment); text != "" {
		metadata.Add(builder, tag.Comment(), text, origin("comment"))
	}
	if genre, ok := decodeV1Genre(value[127]); ok {
		metadata.Add(builder, tag.Genre(), genre, origin("genre"))
	}
	return builder.Build()
}

func marshalV1(ctx metadata.MarshalContext) (metadata.Blob, []loss.Loss, error) {
	for _, block := range ctx.Document().Blocks() {
		if !block.Source() {
			return metadata.Blob{}, nil, fmt.Errorf("%w: ID3v1 cannot carry opaque metadata block %s", errV1Unsupported, block.ID())
		}
	}
	if source, ok := ctx.Document().Block(ctx.Block()); ok && source.Source() && source.Carrier() == ctx.Carrier() && source.Encoding() == ctx.Encoding() {
		return marshalV1Source(ctx, source)
	}
	return marshalV1Canonical(ctx.Document())
}

func marshalV1Source(ctx metadata.MarshalContext, source metadata.RawBlock) (metadata.Blob, []loss.Loss, error) {
	parsed, err := parseV1Source(ctx, source)
	if err != nil {
		return metadata.Blob{}, nil, err
	}
	if sameV1Entries(ctx.Document().Entries(), parsed.Entries()) {
		return source.Payload(), nil, nil
	}
	value, lost, err := marshalV1Canonical(ctx.Document())
	if err != nil {
		return metadata.Blob{}, nil, err
	}
	return value, append(lost, v1UnparsedSourceLosses(source)...), nil
}

func parseV1Source(ctx metadata.MarshalContext, source metadata.RawBlock) (metadata.Document, error) {
	return parseV1Document(ctx.Document().Scope(), ctx.Carrier(), ctx.Encoding(), ctx.Block(), source.Payload())
}

func marshalV1Canonical(document metadata.Document) (metadata.Blob, []loss.Loss, error) {
	entries := document.Entries()
	selected := [v1FieldCount]int{}
	for index := range selected {
		selected[index] = -1
	}
	hasRepresentable := false
	for index, entry := range entries {
		field, ok := v1FieldFor(entry.Key())
		if ok && selected[field] < 0 && v1FieldRepresentable(field, entry) {
			selected[field] = index
			hasRepresentable = true
		}
	}

	track, trackOK := selectedV1Track(entries, selected[v1Track])
	commentWidth := 30
	if trackOK {
		commentWidth = 28
	}
	value := make([]byte, v1Size)
	copy(value, v1Tag)
	value[127] = 255
	var lost []loss.Loss
	for index, entry := range entries {
		field, ok := v1FieldFor(entry.Key())
		if !ok {
			lost = append(lost, v1Loss(entry, loss.Dropped, "", "id3v1.unrepresentable"))
			continue
		}
		if selected[field] != index {
			if v1FieldRepresentable(field, entry) {
				lost = append(lost, v1Loss(entry, loss.Folded, v1Native(field), "id3v1.single-value"))
			} else {
				lost = append(lost, v1UnrepresentableLoss(entry, field))
			}
			continue
		}
		switch field {
		case v1Title:
			lost = append(lost, writeV1Text(value[3:33], entry, "title")...)
		case v1Artist:
			lost = append(lost, writeV1Text(value[33:63], entry, "artist")...)
		case v1Album:
			lost = append(lost, writeV1Text(value[63:93], entry, "album")...)
		case v1Date:
			date, ok := entry.Value().(tag.PartialDate)
			if !ok {
				return metadata.Blob{}, nil, fmt.Errorf("%w: ID3v1 date has type %T", errV1Unsupported, entry.Value())
			}
			year, present := date.Year()
			if !present {
				lost = append(lost, v1Loss(entry, loss.Dropped, "year", "id3v1.date-unrepresentable"))
				continue
			}
			yearText := fmt.Sprintf("%04d", year)
			copy(value[93:97], yearText)
			if date.ToISOString() != yearText {
				lost = append(lost, v1Loss(entry, loss.Truncated, "year", "id3v1.date-year"))
			}
		case v1Genre:
			genre, ok := entry.Value().(string)
			if !ok {
				return metadata.Blob{}, nil, fmt.Errorf("%w: ID3v1 genre has type %T", errV1Unsupported, entry.Value())
			}
			index, canonical, found := encodeV1Genre(genre)
			if !found {
				if genre != "" {
					lost = append(lost, v1Loss(entry, loss.Dropped, "genre", "id3v1.genre-unrepresentable"))
				}
				continue
			}
			value[127] = index
			if genre != canonical {
				lost = append(lost, v1Loss(entry, loss.Substituted, "genre", "id3v1.genre-substituted"))
			}
		case v1Comment:
			lost = append(lost, writeV1Text(value[97:97+commentWidth], entry, "comment")...)
		case v1Track:
			value[125] = 0
			value[126] = byte(track)
		}
	}
	if !hasRepresentable {
		return metadata.NewBlob("application/x-id3v1", nil), lost, nil
	}
	return metadata.NewBlob("application/x-id3v1", value), lost, nil
}

func sameV1Entries(left, right []metadata.Entry) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].Key() != right[index].Key() || left[index].Origin() != right[index].Origin() || !reflect.DeepEqual(left[index].Value(), right[index].Value()) {
			return false
		}
	}
	return true
}

func v1FieldFor(identity key.ID) (v1Field, bool) {
	switch identity {
	case tag.Title().ID():
		return v1Title, true
	case tag.Artist().ID():
		return v1Artist, true
	case tag.Album().ID():
		return v1Album, true
	case tag.Date().ID():
		return v1Date, true
	case tag.Genre().ID():
		return v1Genre, true
	case tag.Comment().ID():
		return v1Comment, true
	case tag.TrackNumber().ID():
		return v1Track, true
	}
	return 0, false
}

func v1Native(field v1Field) string {
	switch field {
	case v1Title:
		return "title"
	case v1Artist:
		return "artist"
	case v1Album:
		return "album"
	case v1Date:
		return "year"
	case v1Genre:
		return "genre"
	case v1Comment:
		return "comment"
	case v1Track:
		return "track"
	}
	return ""
}

func selectedV1Track(entries []metadata.Entry, index int) (int64, bool) {
	if index < 0 {
		return 0, false
	}
	value, ok := entries[index].Value().(int64)
	return value, ok && value >= 1 && value <= 255
}

func v1FieldRepresentable(field v1Field, entry metadata.Entry) bool {
	switch field {
	case v1Title, v1Artist, v1Album, v1Comment:
		value, ok := entry.Value().(string)
		return ok && value != ""
	case v1Date:
		value, ok := entry.Value().(tag.PartialDate)
		if !ok {
			return false
		}
		_, present := value.Year()
		return present
	case v1Genre:
		value, ok := entry.Value().(string)
		_, _, found := encodeV1Genre(value)
		return ok && value != "" && found
	case v1Track:
		_, ok := selectedV1Track([]metadata.Entry{entry}, 0)
		return ok
	}
	return false
}

func v1UnrepresentableLoss(entry metadata.Entry, field v1Field) loss.Loss {
	detail := "id3v1.unrepresentable"
	switch field {
	case v1Title, v1Artist, v1Album, v1Comment:
		detail = "id3v1.text-unrepresentable"
	case v1Date:
		detail = "id3v1.date-unrepresentable"
	case v1Genre:
		detail = "id3v1.genre-unrepresentable"
	case v1Track:
		detail = "id3v1.track-unrepresentable"
	}
	return v1Loss(entry, loss.Dropped, v1Native(field), detail)
}

func writeV1Text(destination []byte, entry metadata.Entry, native string) []loss.Loss {
	value, ok := entry.Value().(string)
	if !ok {
		return []loss.Loss{v1Loss(entry, loss.Dropped, native, "id3v1.text-unrepresentable")}
	}
	encoded, substituted, truncated := encodeV1Text(value, len(destination))
	copy(destination, encoded)
	var lost []loss.Loss
	if substituted {
		lost = append(lost, v1Loss(entry, loss.Substituted, native, "id3v1.text-substituted"))
	}
	if truncated {
		lost = append(lost, v1Loss(entry, loss.Truncated, native, "id3v1.text-truncated"))
	}
	return lost
}

func v1Loss(entry metadata.Entry, kind loss.Kind, native, detail string) loss.Loss {
	return loss.Loss{Key: entry.Key(), Kind: kind, Native: native, Detail: detail, Source: entry.Origin().LossOrigin()}
}

func decodeV1Text(value []byte) string {
	value = bytes.TrimRight(value, "\x00")
	runes := make([]rune, len(value))
	for index, value := range value {
		runes[index] = rune(value)
	}
	return string(runes)
}

func encodeV1Text(value string, limit int) ([]byte, bool, bool) {
	result := make([]byte, 0, limit)
	substituted := false
	for _, rune := range value {
		if len(result) == limit {
			return result, substituted, true
		}
		if rune == 0 || rune > 0xff {
			result = append(result, '?')
			substituted = true
			continue
		}
		result = append(result, byte(rune))
	}
	return result, substituted, false
}

func decodeV1Genre(index byte) (string, bool) {
	if int(index) >= len(v1Genres) {
		return "", false
	}
	return v1Genres[index], true
}

func encodeV1Genre(value string) (byte, string, bool) {
	if value == "" {
		return 255, "", true
	}
	if strings.EqualFold(value, "Psychadelic") {
		return 67, v1Genres[67], true
	}
	for index, genre := range v1Genres {
		if strings.EqualFold(value, genre) {
			return byte(index), genre, true
		}
	}
	return 255, "", false
}

func v1UnparsedSourceLosses(source metadata.RawBlock) []loss.Loss {
	value := source.Payload().AppendTo(nil)
	if len(value) != v1Size || string(value[:3]) != v1Tag {
		return nil
	}
	origin := func(native string) loss.Origin {
		return metadata.Origin{Carrier: source.Carrier(), Encoding: source.Encoding(), Block: source.ID(), Native: native}.LossOrigin()
	}
	var lost []loss.Loss
	if year := decodeV1Text(value[93:97]); year != "" {
		if _, err := tag.ParseDate(year); err != nil {
			lost = append(lost, loss.Loss{Key: tag.Date().ID(), Kind: loss.Dropped, Native: "year", Detail: "id3v1.date-unparsed", Source: origin("year")})
		}
	}
	if value[127] != 255 {
		if _, ok := decodeV1Genre(value[127]); !ok {
			lost = append(lost, loss.Loss{Key: tag.Genre().ID(), Kind: loss.Dropped, Native: "genre", Detail: "id3v1.genre-unparsed", Source: origin("genre")})
		}
	}
	return lost
}
