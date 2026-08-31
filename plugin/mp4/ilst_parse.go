package mp4

import (
	"encoding/binary"
	"fmt"
	"unicode/utf8"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/plugin"
)

type ilstSemantic struct {
	typeID  ilstType
	text    string
	date    tag.PartialDate
	number  int64
	total   int64
	picture tag.Artwork
	ordinal bool
}

func parseIlstDocument(scope metadata.Scope, slot carrier.ID, encoding plugin.Identity, root metadata.BlockID, payload metadata.Blob) (metadata.Document, ilstLayout, error) {
	items, err := ilstScan(payload, 0, payload.Len())
	if err != nil {
		return metadata.Document{}, ilstLayout{}, err
	}
	builder := metadata.NewBuilder(scope)
	builder.AddBlock(metadata.NewSourceBlock(root, slot, encoding, payload))
	layout := ilstLayout{opaque: make(map[metadata.BlockID]int)}
	semantic := 0
	for index, item := range items {
		value, known, err := ilstDecodeItem(payload, item)
		if err != nil {
			return metadata.Document{}, ilstLayout{}, err
		}
		if !known {
			raw, ok := ilstAtomBlob(payload, item, ilstItemMediaType)
			if !ok {
				return metadata.Document{}, ilstLayout{}, fmt.Errorf("%w: item range", errIlstMalformed)
			}
			id := ilstItemBlockID(root, index)
			builder.AddBlock(metadata.NewRawBlock(id, slot, encoding, raw))
			layout.opaque[id] = semantic
			continue
		}
		start := semantic
		semantic += ilstAddSemantic(builder, slot, encoding, root, value)
		raw, ok := ilstAtomBlob(payload, item, ilstItemMediaType)
		if !ok {
			return metadata.Document{}, ilstLayout{}, fmt.Errorf("%w: item range", errIlstMalformed)
		}
		layout.known = append(layout.known, ilstKnownItem{raw: raw, start: start, count: semantic - start})
	}
	document, err := builder.Build()
	if err != nil {
		return metadata.Document{}, ilstLayout{}, err
	}
	entries := document.Entries()
	for index := range layout.known {
		known := &layout.known[index]
		known.entries = append([]metadata.Entry(nil), entries[known.start:known.start+known.count]...)
	}
	return document, layout, nil
}

func ilstItemBlockID(root metadata.BlockID, index int) metadata.BlockID {
	return metadata.BlockID(fmt.Sprintf("%s/item/%08d", root, index))
}

func ilstDecodeItem(payload metadata.Blob, item ilstAtom) (ilstSemantic, bool, error) {
	if !ilstSemanticItem(item.typeID) {
		return ilstSemantic{}, false, nil
	}
	children, err := ilstScan(payload, item.payloadStart, item.payloadEnd)
	if err != nil {
		return ilstSemantic{}, false, err
	}
	if len(children) != 1 || children[0].typeID != ilstData {
		return ilstSemantic{}, false, nil
	}
	data, ok := ilstDataAtom(payload, children[0])
	if !ok {
		return ilstSemantic{}, false, nil
	}
	if ilstKnownText(item.typeID) {
		text := string(data.value.AppendTo(nil))
		if data.typeCode != ilstDataTypeUTF8 || data.locale != 0 || !utf8.ValidString(text) {
			return ilstSemantic{}, false, nil
		}
		return ilstSemantic{typeID: item.typeID, text: text}, true, nil
	}
	switch item.typeID {
	case ilstDate:
		if data.typeCode != ilstDataTypeUTF8 || data.locale != 0 {
			return ilstSemantic{}, false, nil
		}
		text := string(data.value.AppendTo(nil))
		date, err := tag.ParseDate(text)
		if err != nil || date.ToISOString() != text {
			return ilstSemantic{}, false, nil
		}
		return ilstSemantic{typeID: item.typeID, date: date}, true, nil
	case ilstTrack, ilstDisc:
		if data.typeCode != 0 || data.locale != 0 || data.value.Len() != 8 {
			return ilstSemantic{}, false, nil
		}
		value := data.value.AppendTo(nil)
		if binary.BigEndian.Uint16(value[:2]) != 0 || binary.BigEndian.Uint16(value[6:]) != 0 {
			return ilstSemantic{}, false, nil
		}
		number := binary.BigEndian.Uint16(value[2:4])
		if number == 0 {
			return ilstSemantic{}, false, nil
		}
		return ilstSemantic{typeID: item.typeID, number: int64(number), total: int64(binary.BigEndian.Uint16(value[4:6])), ordinal: true}, true, nil
	case ilstCover:
		if data.locale != 0 || data.value.Len() == 0 {
			return ilstSemantic{}, false, nil
		}
		mediaType := ""
		switch data.typeCode {
		case ilstDataTypeJPEG:
			mediaType = "image/jpeg"
		case ilstDataTypePNG:
			mediaType = "image/png"
		default:
			return ilstSemantic{}, false, nil
		}
		image, ok := data.value.Slice(mediaType, 0, data.value.Len())
		if !ok {
			return ilstSemantic{}, false, nil
		}
		return ilstSemantic{typeID: item.typeID, picture: tag.Artwork{Data: image, MediaType: mediaType, Type: tag.ArtworkFrontCover}}, true, nil
	}
	return ilstSemantic{}, false, nil
}

func ilstSemanticItem(value ilstType) bool {
	return ilstKnownText(value) || value == ilstDate || value == ilstTrack || value == ilstDisc || value == ilstCover
}

type ilstDataValue struct {
	typeCode uint32
	locale   uint32
	value    metadata.Blob
}

func ilstDataAtom(payload metadata.Blob, atom ilstAtom) (ilstDataValue, bool) {
	if atom.payloadEnd-atom.payloadStart < 8 {
		return ilstDataValue{}, false
	}
	var header [8]byte
	if _, err := payload.Reader().ReadAt(header[:], int64(atom.payloadStart)); err != nil {
		return ilstDataValue{}, false
	}
	value, ok := payload.Slice(ilstMediaType, atom.payloadStart+8, atom.payloadEnd)
	if !ok {
		return ilstDataValue{}, false
	}
	return ilstDataValue{typeCode: binary.BigEndian.Uint32(header[:4]), locale: binary.BigEndian.Uint32(header[4:]), value: value}, true
}

func ilstKnownText(value ilstType) bool {
	switch value {
	case ilstName, ilstArt, ilstAlbum, ilstComposer, ilstGenre, ilstComment, ilstLyrics, ilstCopyright, ilstEncoder:
		return true
	}
	return false
}

func ilstAddSemantic(builder *metadata.Builder, slot carrier.ID, encoding plugin.Identity, root metadata.BlockID, value ilstSemantic) int {
	origin := metadata.Origin{Carrier: slot, Encoding: encoding, Block: root, Native: ilstAtomString(value.typeID)}
	if value.ordinal {
		if value.typeID == ilstTrack {
			metadata.Add(builder, tag.TrackNumber(), value.number, origin)
			metadata.Add(builder, tag.TotalTracks(), value.total, origin)
		} else {
			metadata.Add(builder, tag.DiscNumber(), value.number, origin)
			metadata.Add(builder, tag.TotalDiscs(), value.total, origin)
		}
		return 2
	}
	switch value.typeID {
	case ilstName:
		metadata.Add(builder, tag.Title(), value.text, origin)
	case ilstArt:
		metadata.Add(builder, tag.Artist(), value.text, origin)
	case ilstAlbum:
		metadata.Add(builder, tag.Album(), value.text, origin)
	case ilstComposer:
		metadata.Add(builder, tag.Composer(), value.text, origin)
	case ilstGenre:
		metadata.Add(builder, tag.Genre(), value.text, origin)
	case ilstDate:
		metadata.Add(builder, tag.Date(), value.date, origin)
	case ilstComment:
		metadata.Add(builder, tag.Comment(), value.text, origin)
	case ilstLyrics:
		metadata.Add(builder, tag.Lyrics(), value.text, origin)
	case ilstCopyright:
		metadata.Add(builder, tag.Copyright(), value.text, origin)
	case ilstEncoder:
		metadata.Add(builder, tag.Encoder(), value.text, origin)
	case ilstCover:
		metadata.Add(builder, tag.Picture(), value.picture, origin)
	}
	return 1
}
