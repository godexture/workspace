package mp4

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/plugin"
)

type ilstOutput struct {
	typeID   ilstType
	typeCode uint32
	data     metadata.Blob
	entries  []metadata.Entry
	raw      metadata.Blob
	units    int
}

func (o ilstOutput) Len() (int, bool) {
	if o.raw.Valid() {
		return o.raw.Len(), true
	}
	if o.data.Len() > math.MaxInt-8 {
		return 0, false
	}
	dataSize, ok := ilstAtomSize(8 + o.data.Len())
	if !ok {
		return 0, false
	}
	return ilstAtomSize(dataSize)
}

func (o ilstOutput) AppendTo(destination []byte) []byte {
	if o.raw.Valid() {
		return o.raw.AppendTo(destination)
	}
	dataSize, _ := ilstAtomSize(8 + o.data.Len())
	return ilstAppendAtom(destination, o.typeID, dataSize, func(destination []byte) []byte {
		return ilstAppendAtom(destination, ilstData, 8+o.data.Len(), func(destination []byte) []byte {
			destination = appendUint32(destination, o.typeCode)
			destination = appendUint32(destination, 0)
			return o.data.AppendTo(destination)
		})
	})
}

func marshalIlstCanonical(document metadata.Document, layout *ilstLayout, slot carrier.ID, encoding plugin.Identity, root metadata.BlockID) (metadata.Blob, []loss.Loss, error) {
	semantic, lost := ilstCanonicalOutputs(document.Entries())
	if layout != nil {
		ilstReuseKnownRaw(semantic, layout.known)
	}
	units := 0
	for _, output := range semantic {
		units += output.units
	}
	raw, err := ilstCanonicalRaw(document, layout, slot, encoding, root, units)
	if err != nil {
		return metadata.Blob{}, nil, err
	}
	outputs := ilstInterleave(semantic, raw)
	if len(outputs) == 0 {
		return metadata.NewBlob(ilstMediaType, nil), lost, nil
	}
	total := 0
	for _, output := range outputs {
		size, ok := output.Len()
		if !ok || total > math.MaxInt-size {
			return metadata.Blob{}, nil, fmt.Errorf("%w: canonical ilst exceeds runtime size", errIlstUnsupported)
		}
		total += size
	}
	value := make([]byte, 0, total)
	for _, output := range outputs {
		value = output.AppendTo(value)
	}
	return metadata.NewBlob(ilstMediaType, value), lost, nil
}

func appendUint32(destination []byte, value uint32) []byte {
	return append(destination, byte(value>>24), byte(value>>16), byte(value>>8), byte(value))
}

func ilstReuseKnownRaw(outputs []ilstOutput, known []ilstKnownItem) {
	used := make([]bool, len(known))
	for outputIndex := range outputs {
		for knownIndex := range known {
			if used[knownIndex] || !ilstSameEntries(outputs[outputIndex].entries, known[knownIndex].entries) {
				continue
			}
			outputs[outputIndex].raw = known[knownIndex].raw
			used[knownIndex] = true
			break
		}
	}
}

func ilstSameEntries(left, right []metadata.Entry) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !ilstSameEntry(left[index], right[index]) {
			return false
		}
	}
	return true
}

type ilstRawItem struct {
	raw      metadata.Blob
	position int
	order    int
}

func ilstCanonicalRaw(document metadata.Document, layout *ilstLayout, slot carrier.ID, encoding plugin.Identity, root metadata.BlockID, units int) ([]ilstRawItem, error) {
	var result []ilstRawItem
	for _, block := range document.Blocks() {
		if block.Source() {
			continue
		}
		if block.Carrier() != slot || block.Encoding() != encoding {
			return nil, fmt.Errorf("%w: cannot carry opaque ilst item %s", errIlstUnsupported, block.ID())
		}
		order, ok := ilstParseItemBlockID(root, block.ID())
		if !ok || block.Payload().MediaType() != ilstItemMediaType {
			return nil, fmt.Errorf("%w: invalid opaque ilst item %s", errIlstUnsupported, block.ID())
		}
		item, err := ilstSingleItem(block.Payload())
		if err != nil {
			return nil, fmt.Errorf("%w: invalid opaque ilst item %s", errIlstUnsupported, block.ID())
		}
		if ilstSemanticItem(item.typeID) {
			if _, known, err := ilstDecodeItem(block.Payload(), item); err != nil || known {
				return nil, fmt.Errorf("%w: injected semantic ilst item %s", errIlstUnsupported, block.ID())
			}
		}
		position := units
		if layout != nil {
			if source, found := layout.opaque[block.ID()]; found {
				position = source
			}
		}
		result = append(result, ilstRawItem{raw: block.Payload(), position: position, order: order})
	}
	for index := 1; index < len(result); index++ {
		for current := index; current > 0 && (result[current].position < result[current-1].position || result[current].position == result[current-1].position && result[current].order < result[current-1].order); current-- {
			result[current], result[current-1] = result[current-1], result[current]
		}
	}
	return result, nil
}

func ilstSingleItem(payload metadata.Blob) (ilstAtom, error) {
	items, err := ilstScan(payload, 0, payload.Len())
	if err != nil || len(items) != 1 || items[0].offset != 0 || items[0].size != payload.Len() {
		return ilstAtom{}, errIlstMalformed
	}
	return items[0], nil
}

func ilstParseItemBlockID(root, value metadata.BlockID) (int, bool) {
	prefix := string(root) + "/item/"
	text := strings.TrimPrefix(string(value), prefix)
	if text == string(value) || len(text) != 8 {
		return 0, false
	}
	parsed, err := strconv.ParseUint(text, 10, 31)
	return int(parsed), err == nil
}

func ilstInterleave(semantic []ilstOutput, raw []ilstRawItem) []ilstOutput {
	result := make([]ilstOutput, 0, len(semantic)+len(raw))
	rawIndex, units := 0, 0
	for _, output := range semantic {
		for rawIndex < len(raw) && raw[rawIndex].position <= units {
			result = append(result, ilstOutput{raw: raw[rawIndex].raw})
			rawIndex++
		}
		result = append(result, output)
		units += output.units
	}
	for rawIndex < len(raw) {
		result = append(result, ilstOutput{raw: raw[rawIndex].raw})
		rawIndex++
	}
	return result
}

type ilstOrdinalPlan struct {
	number int
	total  int
}

func ilstPlanOrdinal(entries []metadata.Entry, number, total key.ID) ilstOrdinalPlan {
	result := ilstOrdinalPlan{number: -1, total: -1}
	for index, entry := range entries {
		switch entry.Key() {
		case number:
			if _, ok := ilstNumber(entry, true); ok && result.number < 0 {
				result.number = index
			}
		case total:
			if _, ok := ilstNumber(entry, false); ok && result.total < 0 {
				result.total = index
			}
		}
	}
	return result
}

func ilstNumber(entry metadata.Entry, required bool) (uint16, bool) {
	value, ok := entry.Value().(int64)
	if !ok || value < 0 || value > math.MaxUint16 || required && value == 0 {
		return 0, false
	}
	return uint16(value), true
}

func ilstCanonicalOutputs(entries []metadata.Entry) ([]ilstOutput, []loss.Loss) {
	tracks := ilstPlanOrdinal(entries, tag.TrackNumber().ID(), tag.TotalTracks().ID())
	discs := ilstPlanOrdinal(entries, tag.DiscNumber().ID(), tag.TotalDiscs().ID())
	seen := make(map[ilstType]bool)
	result := make([]ilstOutput, 0, len(entries))
	var lost []loss.Loss
	for index, entry := range entries {
		if typeID, found := ilstTextType(entry.Key()); found {
			text, ok := entry.Value().(string)
			if !ok || !utf8.ValidString(text) {
				lost = append(lost, ilstLoss(entry, loss.Dropped, ilstAtomString(typeID), "mp4.ilst.text-unrepresentable"))
				continue
			}
			if seen[typeID] {
				lost = append(lost, ilstLoss(entry, loss.Folded, ilstAtomString(typeID), "mp4.ilst.folded"))
				continue
			}
			seen[typeID] = true
			result = append(result, ilstOutput{typeID: typeID, typeCode: ilstDataTypeUTF8, data: metadata.NewBlob(ilstMediaType, []byte(text)), entries: []metadata.Entry{entry}, units: 1})
			continue
		}
		switch entry.Key() {
		case tag.Date().ID():
			date, ok := entry.Value().(tag.PartialDate)
			if !ok || !date.HasValue() || date.ToISOString() == "" {
				lost = append(lost, ilstLoss(entry, loss.Dropped, ilstAtomString(ilstDate), "mp4.ilst.date-unrepresentable"))
				continue
			}
			if seen[ilstDate] {
				lost = append(lost, ilstLoss(entry, loss.Folded, ilstAtomString(ilstDate), "mp4.ilst.folded"))
				continue
			}
			seen[ilstDate] = true
			result = append(result, ilstOutput{typeID: ilstDate, typeCode: ilstDataTypeUTF8, data: metadata.NewBlob(ilstMediaType, []byte(date.ToISOString())), entries: []metadata.Entry{entry}, units: 1})
		case tag.TrackNumber().ID():
			value, ok := ilstNumber(entry, true)
			if !ok {
				lost = append(lost, ilstLoss(entry, loss.Dropped, ilstAtomString(ilstTrack), "mp4.ilst.number-unrepresentable"))
				continue
			}
			if index != tracks.number {
				lost = append(lost, ilstLoss(entry, loss.Folded, ilstAtomString(ilstTrack), "mp4.ilst.folded"))
				continue
			}
			total, paired := uint16(0), []metadata.Entry{entry}
			if tracks.total >= 0 {
				total, _ = ilstNumber(entries[tracks.total], false)
				paired = append(paired, entries[tracks.total])
			}
			result = append(result, ilstOrdinalOutput(ilstTrack, value, total, paired))
		case tag.TotalTracks().ID():
			if _, ok := ilstNumber(entry, false); !ok || tracks.number < 0 {
				lost = append(lost, ilstLoss(entry, loss.Dropped, ilstAtomString(ilstTrack), "mp4.ilst.number-unrepresentable"))
			} else if index != tracks.total {
				lost = append(lost, ilstLoss(entry, loss.Folded, ilstAtomString(ilstTrack), "mp4.ilst.folded"))
			}
		case tag.DiscNumber().ID():
			value, ok := ilstNumber(entry, true)
			if !ok {
				lost = append(lost, ilstLoss(entry, loss.Dropped, ilstAtomString(ilstDisc), "mp4.ilst.number-unrepresentable"))
				continue
			}
			if index != discs.number {
				lost = append(lost, ilstLoss(entry, loss.Folded, ilstAtomString(ilstDisc), "mp4.ilst.folded"))
				continue
			}
			total, paired := uint16(0), []metadata.Entry{entry}
			if discs.total >= 0 {
				total, _ = ilstNumber(entries[discs.total], false)
				paired = append(paired, entries[discs.total])
			}
			result = append(result, ilstOrdinalOutput(ilstDisc, value, total, paired))
		case tag.TotalDiscs().ID():
			if _, ok := ilstNumber(entry, false); !ok || discs.number < 0 {
				lost = append(lost, ilstLoss(entry, loss.Dropped, ilstAtomString(ilstDisc), "mp4.ilst.number-unrepresentable"))
			} else if index != discs.total {
				lost = append(lost, ilstLoss(entry, loss.Folded, ilstAtomString(ilstDisc), "mp4.ilst.folded"))
			}
		case tag.Picture().ID():
			picture, ok := entry.Value().(tag.Artwork)
			if !ok || !picture.Valid() || picture.Data.Len() == 0 || picture.Type != tag.ArtworkFrontCover {
				lost = append(lost, ilstLoss(entry, loss.Dropped, ilstAtomString(ilstCover), "mp4.ilst.picture-unrepresentable"))
				continue
			}
			dataType, supported := ilstPictureType(picture.EffectiveMediaType())
			if !supported {
				lost = append(lost, ilstLoss(entry, loss.Dropped, ilstAtomString(ilstCover), "mp4.ilst.picture-unrepresentable"))
				continue
			}
			if seen[ilstCover] {
				lost = append(lost, ilstLoss(entry, loss.Folded, ilstAtomString(ilstCover), "mp4.ilst.folded"))
				continue
			}
			seen[ilstCover] = true
			result = append(result, ilstOutput{typeID: ilstCover, typeCode: dataType, data: picture.Data, entries: []metadata.Entry{entry}, units: 1})
			if picture.Description != "" || picture.Width != 0 || picture.Height != 0 || picture.ColorDepth != 0 || picture.IndexedColors != 0 {
				lost = append(lost, ilstLoss(entry, loss.Truncated, ilstAtomString(ilstCover), "mp4.ilst.covr-qualifiers"))
			}
		default:
			lost = append(lost, ilstLoss(entry, loss.Dropped, "", "mp4.ilst.unrepresentable"))
		}
	}
	return result, lost
}

func ilstOrdinalOutput(typeID ilstType, number, total uint16, entries []metadata.Entry) ilstOutput {
	value := make([]byte, 8)
	value[2], value[3] = byte(number>>8), byte(number)
	value[4], value[5] = byte(total>>8), byte(total)
	return ilstOutput{typeID: typeID, data: metadata.NewBlob(ilstMediaType, value), entries: entries, units: len(entries)}
}

func ilstTextType(identity key.ID) (ilstType, bool) {
	switch identity {
	case tag.Title().ID():
		return ilstName, true
	case tag.Artist().ID():
		return ilstArt, true
	case tag.Album().ID():
		return ilstAlbum, true
	case tag.Composer().ID():
		return ilstComposer, true
	case tag.Genre().ID():
		return ilstGenre, true
	case tag.Comment().ID():
		return ilstComment, true
	case tag.Lyrics().ID():
		return ilstLyrics, true
	case tag.Copyright().ID():
		return ilstCopyright, true
	case tag.Encoder().ID():
		return ilstEncoder, true
	}
	return ilstType{}, false
}

func ilstPictureType(mediaType string) (uint32, bool) {
	switch {
	case strings.EqualFold(mediaType, "image/jpeg"):
		return ilstDataTypeJPEG, true
	case strings.EqualFold(mediaType, "image/png"):
		return ilstDataTypePNG, true
	}
	return 0, false
}
