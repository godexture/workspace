package id3

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/plugin"
)

func parseV2Document(scope metadata.Scope, slot carrier.ID, encoding plugin.Identity, block metadata.BlockID, payload metadata.Blob) (metadata.Document, v2Layout, error) {
	parsed, err := parseV2Tag(payload)
	if err != nil {
		return metadata.Document{}, v2Layout{}, err
	}
	builder := metadata.NewBuilder(scope)
	builder.AddBlock(metadata.NewSourceBlock(block, slot, encoding, payload))
	layout := v2Layout{unsafe: parsed.editUnsafe || parsed.sourceUnsync, opaque: make(map[metadata.BlockID]int)}
	if parsed.frames.Len() != 0 {
		if err := parseV2Frames(builder, slot, encoding, block, parsed, &layout); err != nil {
			return metadata.Document{}, v2Layout{}, err
		}
	}
	document, err := builder.Build()
	if err != nil {
		return metadata.Document{}, v2Layout{}, err
	}
	return document, layout, nil
}

func parseV2Tag(value metadata.Blob) (v2Tag, error) {
	header, ok := v2Read(value, 0, v2HeaderSize)
	if !ok || !bytes.Equal(header[:3], []byte("ID3")) {
		return v2Tag{}, errV2Malformed
	}
	version, revision, flags := header[3], header[4], header[5]
	if revision == 0xff || !v2VersionValid(version) || flags&0x0f != 0 || !v2HeaderFlagsValid(version, flags) {
		return v2Tag{}, errV2Malformed
	}
	size, ok := v2DecodeSyncSafe(header[6:10])
	if !ok {
		return v2Tag{}, errV2Malformed
	}
	foot := version == 4 && flags&0x10 != 0
	total := v2HeaderSize + size
	if foot {
		total += v2HeaderSize
	}
	if total != value.Len() {
		return v2Tag{}, errV2Malformed
	}
	footer, footerOK := v2Read(value, total-v2HeaderSize, v2HeaderSize)
	if foot && (!footerOK || !v2FooterMatches(footer, header)) {
		return v2Tag{}, errV2Malformed
	}
	payload, ok := value.Slice("", v2HeaderSize, v2HeaderSize+size)
	if !ok {
		return v2Tag{}, errV2Malformed
	}
	sourceUnsync, tagUnsync := false, false
	experimental := flags&0x20 != 0
	if version == 2 {
		if flags&0x40 != 0 {
			return v2Tag{version: version, decodeUnsafe: true, editUnsafe: true}, nil
		}
		if flags&0x80 != 0 {
			payload = metadata.NewBlob("", v2RemoveUnsync(payload.AppendTo(nil)))
			sourceUnsync = true
		}
	} else if version == 3 {
		if flags&0x80 != 0 {
			payload = metadata.NewBlob("", v2RemoveUnsync(payload.AppendTo(nil)))
			sourceUnsync = true
		}
	} else if flags&0x80 != 0 {
		tagUnsync = true
	}
	frames, restrictions, paddingDeclared, err := v2SkipExtendedHeader(version, flags, payload)
	if err != nil {
		return v2Tag{}, err
	}
	return v2Tag{version: version, editUnsafe: experimental || restrictions, sourceUnsync: sourceUnsync, tagUnsync: tagUnsync, footer: foot, paddingDeclared: paddingDeclared, frames: frames}, nil
}

func v2VersionValid(value byte) bool { return value >= 2 && value <= 4 }

func v2HeaderFlagsValid(version, flags byte) bool {
	switch version {
	case 2:
		return flags&^byte(0xe0) == 0
	case 3:
		return flags&^byte(0xe0) == 0
	case 4:
		return flags&^byte(0xf0) == 0
	}
	return false
}

func v2FooterMatches(footer, header []byte) bool {
	return len(footer) == v2HeaderSize && bytes.Equal(footer[:3], []byte("3DI")) && bytes.Equal(footer[3:], header[3:])
}

func v2SkipExtendedHeader(version, flags byte, payload metadata.Blob) (metadata.Blob, bool, bool, error) {
	hasExtended := (version == 3 || version == 4) && flags&0x40 != 0
	if !hasExtended {
		return payload, false, false, nil
	}
	prefix, ok := v2Read(payload, 0, 6)
	if !ok {
		return metadata.Blob{}, false, false, errV2Malformed
	}
	if version == 3 {
		size := int(binary.BigEndian.Uint32(prefix[:4]))
		total := 4 + size
		if size < 6 || total > payload.Len() {
			return metadata.Blob{}, false, false, errV2Malformed
		}
		extendedFlags := binary.BigEndian.Uint16(prefix[4:6])
		expectedSize := 6
		if extendedFlags&0x8000 != 0 {
			expectedSize += 4
		}
		if extendedFlags&^uint16(0x8000) != 0 || size != expectedSize {
			return metadata.Blob{}, false, false, errV2Malformed
		}
		padding, ok := v2Read(payload, 6, 4)
		if !ok {
			return metadata.Blob{}, false, false, errV2Malformed
		}
		paddingSize := int(binary.BigEndian.Uint32(padding))
		if paddingSize > payload.Len()-total || !v2AllZero(payload, payload.Len()-paddingSize) {
			return metadata.Blob{}, false, false, errV2Malformed
		}
		if extendedFlags&0x8000 != 0 {
			if _, ok := v2Read(payload, 10, 4); !ok {
				return metadata.Blob{}, false, false, errV2Malformed
			}
		}
		frames, ok := payload.Slice("", total, payload.Len()-paddingSize)
		if !ok {
			return metadata.Blob{}, false, false, errV2Malformed
		}
		return frames, false, true, nil
	}
	size, ok := v2DecodeSyncSafe(prefix[:4])
	if !ok || size < 6 || size > payload.Len() || prefix[4] != 1 {
		return metadata.Blob{}, false, false, errV2Malformed
	}
	flagsByte := prefix[5]
	if flagsByte&^byte(0x70) != 0 {
		return metadata.Blob{}, false, false, errV2Malformed
	}
	expectedSize := 6
	if flagsByte&0x40 != 0 {
		expectedSize++
	}
	if flagsByte&0x20 != 0 {
		expectedSize += 6
	}
	if flagsByte&0x10 != 0 {
		expectedSize += 2
	}
	if size != expectedSize {
		return metadata.Blob{}, false, false, errV2Malformed
	}
	extended, ok := v2Read(payload, 0, size)
	if !ok {
		return metadata.Blob{}, false, false, errV2Malformed
	}
	cursor := 6
	restrictions := flagsByte&0x10 != 0
	for _, flag := range []byte{0x40, 0x20, 0x10} {
		if flagsByte&flag == 0 {
			continue
		}
		if cursor >= size {
			return metadata.Blob{}, false, false, errV2Malformed
		}
		length := int(extended[cursor])
		cursor++
		expected := 0
		switch flag {
		case 0x20:
			expected = 5
		case 0x10:
			expected = 1
		}
		if length != expected || cursor+length > size {
			return metadata.Blob{}, false, false, errV2Malformed
		}
		if flag == 0x20 {
			if extended[cursor] > 0x0f {
				return metadata.Blob{}, false, false, errV2Malformed
			}
			for _, byteValue := range extended[cursor : cursor+length] {
				if byteValue&0x80 != 0 {
					return metadata.Blob{}, false, false, errV2Malformed
				}
			}
		}
		cursor += length
	}
	if cursor != size {
		return metadata.Blob{}, false, false, errV2Malformed
	}
	frames, ok := payload.Slice("", size, payload.Len())
	if !ok {
		return metadata.Blob{}, false, false, errV2Malformed
	}
	return frames, restrictions, false, nil
}

func parseV2Frames(builder *metadata.Builder, slot carrier.ID, encoding plugin.Identity, root metadata.BlockID, tagData v2Tag, layout *v2Layout) error {
	frames, err := v2ScanFrames(tagData)
	if err != nil {
		return err
	}
	legacyDates := v2PlanLegacyDates(tagData, frames, slot, encoding, root)
	for _, frame := range frames {
		if date, ok := legacyDates.output[frame.offset]; ok {
			metadata.Add(builder, tag.Date(), date.value, date.origin)
			layout.semantic++
			continue
		}
		if legacyDates.skip[frame.offset] {
			continue
		}
		if !v2FrameIDValid(frame.id) {
			v2AddOpaqueFrame(builder, slot, encoding, root, frame, layout, frame.safe(tagData.version, tagData.sourceUnsync || tagData.tagUnsync))
			continue
		}
		if !tagData.decodeUnsafe {
			if pictureData, ok := v2PictureData(tagData, frame); ok && v2ApplyPicture(builder, slot, encoding, root, tagData.version, frame.id, pictureData) {
				layout.semantic++
				continue
			}
		}
		data, semanticSafe := v2FrameData(tagData, frame)
		if tagData.decodeUnsafe || !semanticSafe {
			v2AddOpaqueFrame(builder, slot, encoding, root, frame, layout, false)
			continue
		}
		if units, ok := v2ApplyFrame(builder, slot, encoding, root, tagData.version, frame.id, data); ok {
			layout.semantic += units
			continue
		}
		v2AddOpaqueFrame(builder, slot, encoding, root, frame, layout, frame.safe(tagData.version, tagData.sourceUnsync || tagData.tagUnsync))
	}
	return nil
}

func v2ScanFrames(tagData v2Tag) ([]v2Frame, error) {
	var result []v2Frame
	for offset := 0; offset < tagData.frames.Len(); {
		frame, padding, err := v2NextFrame(tagData.version, tagData.frames, offset)
		if err != nil {
			return nil, err
		}
		if padding {
			if tagData.footer || tagData.paddingDeclared {
				return nil, errV2Malformed
			}
			return result, nil
		}
		offset += frame.raw.Len()
		result = append(result, frame)
	}
	return result, nil
}

type v2Frame struct {
	id     string
	data   metadata.Blob
	raw    metadata.Blob
	flags  [2]byte
	offset int
}

func (f v2Frame) safe(version byte, tagUnsync bool) bool {
	return version == 4 && !tagUnsync && f.flags == [2]byte{}
}

func v2PictureData(tagData v2Tag, frame v2Frame) (metadata.Blob, bool) {
	if frame.id != "APIC" && frame.id != "PIC" {
		return metadata.Blob{}, false
	}
	if tagData.version == 2 {
		return frame.data, true
	}
	if tagData.version == 3 {
		if frame.flags != [2]byte{} {
			return metadata.Blob{}, false
		}
		return frame.data, true
	}
	if frame.flags[0] != 0 || frame.flags[1]&^byte(0x4f) != 0 || frame.flags[1]&0x4c != 0 {
		return metadata.Blob{}, false
	}
	if !tagData.tagUnsync && frame.flags[1]&0x03 == 0 {
		return frame.data, true
	}
	data, ok := v2FrameData(tagData, frame)
	if !ok {
		return metadata.Blob{}, false
	}
	return metadata.NewBlob("", data), true
}

func v2FrameIDValid(value string) bool {
	if len(value) != 3 && len(value) != 4 {
		return false
	}
	for _, rune := range value {
		if !(rune >= 'A' && rune <= 'Z') && !(rune >= '0' && rune <= '9') {
			return false
		}
	}
	return true
}

func v2FrameData(tagData v2Tag, frame v2Frame) ([]byte, bool) {
	data := frame.data.AppendTo(nil)
	if tagData.version == 2 {
		return data, true
	}
	if frame.flags[0] != 0 {
		return nil, false
	}
	format := frame.flags[1]
	if tagData.version == 3 {
		if format != 0 {
			return nil, false
		}
		return data, true
	}
	if format&^byte(0x4f) != 0 || format&0x4c != 0 {
		return nil, false
	}
	if format&0x01 != 0 {
		if len(data) < 4 {
			return nil, false
		}
		declared, ok := v2DecodeSyncSafe(data[:4])
		if !ok {
			return nil, false
		}
		data = data[4:]
		if tagData.tagUnsync || format&0x02 != 0 {
			data = v2RemoveUnsync(data)
		}
		if len(data) != declared {
			return nil, false
		}
		return data, true
	}
	if tagData.tagUnsync || format&0x02 != 0 {
		data = v2RemoveUnsync(data)
	}
	return data, true
}

func v2NextFrame(version byte, payload metadata.Blob, offset int) (v2Frame, bool, error) {
	headerSize := 10
	if version == 2 {
		headerSize = 6
	}
	header, ok := v2Read(payload, offset, headerSize)
	if !ok {
		if v2AllZero(payload, offset) {
			return v2Frame{}, true, nil
		}
		return v2Frame{}, false, errV2Malformed
	}
	idSize := 4
	if version == 2 {
		idSize = 3
	}
	if bytes.Equal(header[:idSize], make([]byte, idSize)) {
		if !v2AllZero(payload, offset) {
			return v2Frame{}, false, errV2Malformed
		}
		return v2Frame{}, true, nil
	}
	size := 0
	switch version {
	case 2:
		size = int(header[3])<<16 | int(header[4])<<8 | int(header[5])
	case 3:
		size = int(binary.BigEndian.Uint32(header[4:8]))
	case 4:
		var ok bool
		size, ok = v2DecodeSyncSafe(header[4:8])
		if !ok {
			return v2Frame{}, false, errV2Malformed
		}
	}
	if size > payload.Len()-offset-headerSize {
		return v2Frame{}, false, errV2Malformed
	}
	raw, rawOK := payload.Slice("", offset, offset+headerSize+size)
	data, dataOK := payload.Slice("", offset+headerSize, offset+headerSize+size)
	if !rawOK || !dataOK {
		return v2Frame{}, false, errV2Malformed
	}
	result := v2Frame{id: string(header[:idSize]), data: data, raw: raw, offset: offset}
	if version != 2 {
		result.flags = [2]byte{header[8], header[9]}
	}
	return result, false, nil
}

func v2AddOpaqueFrame(builder *metadata.Builder, slot carrier.ID, encoding plugin.Identity, root metadata.BlockID, frame v2Frame, layout *v2Layout, safe bool) {
	id := v2FrameBlockID(root, frame.offset)
	mediaType := v2UnsafeRawMediaType
	if safe {
		mediaType = v2RawMediaType
	}
	raw, ok := frame.raw.Slice(mediaType, 0, frame.raw.Len())
	if !ok {
		return
	}
	builder.AddBlock(metadata.NewRawBlock(id, slot, encoding, raw))
	layout.opaque[id] = layout.semantic
}

func v2FrameBlockID(root metadata.BlockID, offset int) metadata.BlockID {
	return metadata.BlockID(fmt.Sprintf("%s/frame/%08d", root, offset))
}

func v2Read(blob metadata.Blob, offset, size int) ([]byte, bool) {
	if offset < 0 || size < 0 || offset > blob.Len()-size {
		return nil, false
	}
	value := make([]byte, size)
	_, err := blob.Reader().ReadAt(value, int64(offset))
	return value, err == nil
}

func v2AllZero(blob metadata.Blob, offset int) bool {
	reader := blob.Reader()
	buffer := make([]byte, 4096)
	for offset < blob.Len() {
		count := len(buffer)
		if remaining := blob.Len() - offset; remaining < count {
			count = remaining
		}
		if _, err := reader.ReadAt(buffer[:count], int64(offset)); err != nil {
			return false
		}
		if bytes.IndexFunc(buffer[:count], func(r rune) bool { return r != 0 }) >= 0 {
			return false
		}
		offset += count
	}
	return true
}
