package vorbiscomment

import (
	"encoding/base64"
	"encoding/binary"
	"io"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/tag"
)

func vcDecodePicture(value string) (tag.Artwork, bool) {
	if strings.ContainsAny(value, "\r\n") {
		return tag.Artwork{}, false
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil {
		return tag.Artwork{}, false
	}
	reader := vcPictureReader{data: decoded}
	pictureType, ok := reader.u32()
	if !ok || pictureType > 20 {
		return tag.Artwork{}, false
	}
	mime, ok := reader.string()
	if !ok {
		return tag.Artwork{}, false
	}
	wireMediaType := string(mime)
	mediaType := wireMediaType
	if wireMediaType == "" {
		mediaType = "image/"
	}
	if wireMediaType != "" && !tag.IsImageMediaType(wireMediaType) {
		return tag.Artwork{}, false
	}
	description, ok := reader.string()
	if !ok || !utf8.Valid(description) {
		return tag.Artwork{}, false
	}
	width, ok := reader.u32()
	if !ok {
		return tag.Artwork{}, false
	}
	height, ok := reader.u32()
	if !ok {
		return tag.Artwork{}, false
	}
	depth, ok := reader.u32()
	if !ok {
		return tag.Artwork{}, false
	}
	colors, ok := reader.u32()
	if !ok {
		return tag.Artwork{}, false
	}
	image, ok := reader.string()
	if !ok || len(image) == 0 || reader.offset != len(decoded) {
		return tag.Artwork{}, false
	}
	picture := tag.Artwork{
		Data:          metadata.NewBlob(mediaType, image),
		MediaType:     mediaType,
		Type:          tag.ArtworkType(pictureType),
		Description:   string(description),
		Width:         width,
		Height:        height,
		ColorDepth:    depth,
		IndexedColors: colors,
	}
	if !vcPictureValid(picture) {
		return tag.Artwork{}, false
	}
	return picture, true
}

type vcPictureOutput struct {
	picture       tag.Artwork
	wireMediaType string
	bodySize      int
	encoded       int
}

func vcPictureOutputFor(entry metadata.Entry) (vcPictureOutput, bool) {
	picture, ok := entry.Value().(tag.Artwork)
	if !ok {
		return vcPictureOutput{}, false
	}
	wireMediaType := picture.EffectiveMediaType()
	switch wireMediaType {
	case "":
	case "image/":
		if entry.Origin().Encoding != EncodingIdentity() {
			return vcPictureOutput{}, false
		}
		wireMediaType = ""
	default:
		if !tag.IsImageMediaType(wireMediaType) {
			return vcPictureOutput{}, false
		}
	}
	if !picture.Valid() || picture.Data.Len() == 0 || picture.Type > 20 || !utf8.ValidString(picture.Description) || !vcPictureValid(picture) {
		return vcPictureOutput{}, false
	}
	if uint64(len(wireMediaType)) > uint64(^uint32(0)) || uint64(len(picture.Description)) > uint64(^uint32(0)) || uint64(picture.Data.Len()) > uint64(^uint32(0)) {
		return vcPictureOutput{}, false
	}
	if len(wireMediaType) > math.MaxInt-32 || len(picture.Description) > math.MaxInt-32-len(wireMediaType) || picture.Data.Len() > math.MaxInt-32-len(wireMediaType)-len(picture.Description) {
		return vcPictureOutput{}, false
	}
	bodySize := 32 + len(wireMediaType) + len(picture.Description) + picture.Data.Len()
	encoded := ((uint64(bodySize) + 2) / 3) * 4
	if encoded > uint64(math.MaxInt) || encoded > math.MaxUint32 {
		return vcPictureOutput{}, false
	}
	return vcPictureOutput{picture: picture, wireMediaType: wireMediaType, bodySize: bodySize, encoded: int(encoded)}, true
}

func (p vcPictureOutput) encodedLen() int { return p.encoded }

func (p vcPictureOutput) appendBase64(destination []byte) []byte {
	writer := vcSliceWriter{destination: &destination}
	encoded := base64.NewEncoder(base64.StdEncoding, &writer)
	vcWriteU32(encoded, uint32(p.picture.Type))
	vcWriteString(encoded, p.wireMediaType)
	vcWriteString(encoded, p.picture.Description)
	vcWriteU32(encoded, p.picture.Width)
	vcWriteU32(encoded, p.picture.Height)
	vcWriteU32(encoded, p.picture.ColorDepth)
	vcWriteU32(encoded, p.picture.IndexedColors)
	vcWriteU32(encoded, uint32(p.picture.Data.Len()))
	_, _ = p.picture.Data.Reader().WriteTo(encoded)
	_ = encoded.Close()
	return destination
}

type vcSliceWriter struct{ destination *[]byte }

func (w vcSliceWriter) Write(value []byte) (int, error) {
	*w.destination = append(*w.destination, value...)
	return len(value), nil
}

func vcWriteU32(writer io.Writer, value uint32) {
	var buffer [4]byte
	binary.BigEndian.PutUint32(buffer[:], value)
	_, _ = writer.Write(buffer[:])
}

func vcWriteString(writer io.Writer, value string) {
	vcWriteU32(writer, uint32(len(value)))
	_, _ = io.WriteString(writer, value)
}

func vcPictureValid(picture tag.Artwork) bool {
	return picture.Type != tag.ArtworkFileIcon || (strings.EqualFold(picture.EffectiveMediaType(), "image/png") && picture.Width == 32 && picture.Height == 32)
}

type vcPictureReader struct {
	data   []byte
	offset int
}

func (r *vcPictureReader) u32() (uint32, bool) {
	if r.offset > len(r.data)-4 {
		return 0, false
	}
	value := binary.BigEndian.Uint32(r.data[r.offset:])
	r.offset += 4
	return value, true
}

func (r *vcPictureReader) string() ([]byte, bool) {
	length, ok := r.u32()
	if !ok || uint64(length) > uint64(len(r.data)-r.offset) {
		return nil, false
	}
	start := r.offset
	r.offset += int(length)
	return r.data[start:r.offset], true
}

func vcAppendPictureString(destination, value []byte) []byte {
	destination = binary.BigEndian.AppendUint32(destination, uint32(len(value)))
	return append(destination, value...)
}
