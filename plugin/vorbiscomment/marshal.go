package vorbiscomment

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strconv"
	"unicode/utf8"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/plugin"
)

type outputField struct {
	value  string
	length int
	write  func([]byte) []byte
	units  int
}

func textField(value string, units int) outputField {
	return outputField{value: value, length: len(value), units: units}
}

func (f outputField) Len() int {
	if f.write != nil {
		return f.length
	}
	return len(f.value)
}

func (f outputField) AppendTo(destination []byte) []byte {
	if f.write != nil {
		return f.write(destination)
	}
	return append(destination, f.value...)
}

type rawField struct {
	value    string
	position int
	order    int
}

func marshalCanonical(document metadata.Document, source *layout, slot carrier.ID, encoding plugin.Identity, root metadata.BlockID) (metadata.Blob, []loss.Loss, error) {
	semantic, lost := canonicalFields(document.Entries())
	units := 0
	for _, field := range semantic {
		units += field.units
	}
	vendor, raw, err := canonicalRaw(document, source, slot, encoding, root, units)
	if err != nil {
		return metadata.Blob{}, nil, err
	}
	fields := interleave(semantic, raw)
	if uint64(len(vendor)) > math.MaxUint32 || uint64(len(fields)) > math.MaxUint32 {
		return metadata.Blob{}, nil, fmt.Errorf("%w: Vorbis Comment count or vendor exceeds uint32", errUnsupported)
	}
	total := 8 + len(vendor)
	for _, field := range fields {
		if uint64(field.Len()) > math.MaxUint32 || total > math.MaxInt-4-field.Len() {
			return metadata.Blob{}, nil, fmt.Errorf("%w: Vorbis Comment field exceeds uint32", errUnsupported)
		}
		total += 4 + field.Len()
	}
	value := make([]byte, 0, total)
	value = vcAppendString(value, vendor)
	value = binary.LittleEndian.AppendUint32(value, uint32(len(fields)))
	for _, field := range fields {
		value = binary.LittleEndian.AppendUint32(value, uint32(field.Len()))
		value = field.AppendTo(value)
	}
	return metadata.NewBlob(mediaType, value), lost, nil
}

func canonicalFields(entries []metadata.Entry) ([]outputField, []loss.Loss) {
	result := make([]outputField, 0, len(entries))
	var lost []loss.Loss
	pictures := make(map[tag.ArtworkType]int)
	for _, entry := range entries {
		if name, value, ok := vcTextField(entry); ok {
			if !utf8.ValidString(value) {
				lost = append(lost, vcLoss(entry, loss.Dropped, name, "vorbiscomment.text-unrepresentable"))
				continue
			}
			result = append(result, textField(name+"="+value, 1))
			continue
		}
		switch entry.Key() {
		case tag.Date().ID():
			date, ok := entry.Value().(tag.PartialDate)
			if !ok || !date.HasValue() || date.ToISOString() == "" {
				lost = append(lost, vcLoss(entry, loss.Dropped, "DATE", "vorbiscomment.date-unrepresentable"))
				continue
			}
			result = append(result, textField("DATE="+date.ToISOString(), 1))
		case tag.TrackNumber().ID():
			value, ok := vcEntryUnsigned(entry)
			if !ok {
				lost = append(lost, vcLoss(entry, loss.Dropped, "TRACKNUMBER", "vorbiscomment.number-unrepresentable"))
				continue
			}
			result = append(result, textField("TRACKNUMBER="+strconv.FormatInt(value, 10), 1))
		case tag.TotalTracks().ID():
			value, ok := vcEntryUnsigned(entry)
			if !ok {
				lost = append(lost, vcLoss(entry, loss.Dropped, "TRACKTOTAL", "vorbiscomment.number-unrepresentable"))
				continue
			}
			result = append(result, textField("TRACKTOTAL="+strconv.FormatInt(value, 10), 1))
		case tag.DiscNumber().ID():
			value, ok := vcEntryUnsigned(entry)
			if !ok {
				lost = append(lost, vcLoss(entry, loss.Dropped, "DISCNUMBER", "vorbiscomment.number-unrepresentable"))
				continue
			}
			result = append(result, textField("DISCNUMBER="+strconv.FormatInt(value, 10), 1))
		case tag.TotalDiscs().ID():
			value, ok := vcEntryUnsigned(entry)
			if !ok {
				lost = append(lost, vcLoss(entry, loss.Dropped, "DISCTOTAL", "vorbiscomment.number-unrepresentable"))
				continue
			}
			result = append(result, textField("DISCTOTAL="+strconv.FormatInt(value, 10), 1))
		case tag.Picture().ID():
			pictureOutput, valid := vcPictureOutputFor(entry)
			if !valid {
				lost = append(lost, vcLoss(entry, loss.Dropped, "METADATA_BLOCK_PICTURE", "vorbiscomment.picture-unrepresentable"))
				continue
			}
			picture := pictureOutput.picture
			if picture.Type == tag.ArtworkFileIcon || picture.Type == 2 {
				if first, found := pictures[picture.Type]; found {
					result[first].units++
					lost = append(lost, vcLoss(entry, loss.Folded, "METADATA_BLOCK_PICTURE", "vorbiscomment.picture-folded"))
					continue
				}
				pictures[picture.Type] = len(result)
			}
			prefix := "METADATA_BLOCK_PICTURE="
			if pictureOutput.encodedLen() > math.MaxInt-len(prefix) {
				lost = append(lost, vcLoss(entry, loss.Dropped, "METADATA_BLOCK_PICTURE", "vorbiscomment.picture-unrepresentable"))
				continue
			}
			result = append(result, outputField{
				length: len(prefix) + pictureOutput.encodedLen(),
				write: func(destination []byte) []byte {
					destination = append(destination, prefix...)
					return pictureOutput.appendBase64(destination)
				},
				units: 1,
			})
		default:
			lost = append(lost, vcLoss(entry, loss.Dropped, "", "vorbiscomment.unrepresentable"))
		}
	}
	return result, lost
}

func vcEntryUnsigned(entry metadata.Entry) (int64, bool) {
	value, ok := entry.Value().(int64)
	return value, ok && value >= 0
}

func canonicalRaw(document metadata.Document, source *layout, slot carrier.ID, encoding plugin.Identity, root metadata.BlockID, units int) (string, []rawField, error) {
	vendor := defaultVendor
	var result []rawField
	for _, block := range document.Blocks() {
		if block.Source() {
			continue
		}
		if block.Carrier() != slot || block.Encoding() != encoding {
			return "", nil, fmt.Errorf("%w: cannot carry opaque metadata block %s", errUnsupported, block.ID())
		}
		if block.ID() == vendorBlockID(root) {
			if block.Payload().MediaType() != vendorMediaType || !utf8.Valid(block.Payload().AppendTo(nil)) {
				return "", nil, fmt.Errorf("%w: invalid vendor block %s", errUnsupported, block.ID())
			}
			vendor = string(block.Payload().AppendTo(nil))
			continue
		}
		index, owned := parseFieldBlockID(root, block.ID())
		if !owned || block.Payload().MediaType() != fieldMediaType {
			return "", nil, fmt.Errorf("%w: invalid opaque metadata block %s", errUnsupported, block.ID())
		}
		value := block.Payload().AppendTo(nil)
		if !utf8.Valid(value) || !vcFieldSyntax(value) {
			return "", nil, fmt.Errorf("%w: invalid opaque field %s", errUnsupported, block.ID())
		}
		field := string(value)
		if vcKnownField(field) {
			return "", nil, fmt.Errorf("%w: injected semantic raw field %s", errUnsupported, block.ID())
		}
		position := units
		if source != nil {
			if known, found := source.raw[block.ID()]; found {
				position = known
			}
		}
		result = append(result, rawField{value: field, position: position, order: index})
	}
	if uint64(len(vendor)) > math.MaxUint32 {
		return "", nil, fmt.Errorf("%w: vendor exceeds uint32", errUnsupported)
	}
	sort.SliceStable(result, func(left, right int) bool {
		if result[left].position != result[right].position {
			return result[left].position < result[right].position
		}
		return result[left].order < result[right].order
	})
	return vendor, result, nil
}

func interleave(semantic []outputField, raw []rawField) []outputField {
	result := make([]outputField, 0, len(semantic)+len(raw))
	rawIndex, units := 0, 0
	for _, field := range semantic {
		for rawIndex < len(raw) && raw[rawIndex].position <= units {
			result = append(result, textField(raw[rawIndex].value, 0))
			rawIndex++
		}
		result = append(result, field)
		units += field.units
	}
	for rawIndex < len(raw) {
		result = append(result, textField(raw[rawIndex].value, 0))
		rawIndex++
	}
	return result
}

func vcAppendString(destination []byte, value string) []byte {
	destination = binary.LittleEndian.AppendUint32(destination, uint32(len(value)))
	return append(destination, value...)
}
