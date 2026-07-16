package vorbiscomment

import (
	"encoding/binary"
	"errors"
	_ "image/png"
	"net/http"

	"github.com/godexture/core/domain/metadata"
)

func ParsePicture(payload []byte) (metadata.Thumbnail, error) {
	offset := 0
	pictureType, err := readPictureUint32(payload, &offset)
	if err != nil {
		return metadata.Thumbnail{}, err
	}
	mimeType, err := readPictureString(payload, &offset)
	if err != nil {
		return metadata.Thumbnail{}, err
	}
	description, err := readPictureString(payload, &offset)
	if err != nil {
		return metadata.Thumbnail{}, err
	}
	width, err := readPictureUint32(payload, &offset)
	if err != nil {
		return metadata.Thumbnail{}, err
	}
	height, err := readPictureUint32(payload, &offset)
	if err != nil {
		return metadata.Thumbnail{}, err
	}
	depth, err := readPictureUint32(payload, &offset)
	if err != nil {
		return metadata.Thumbnail{}, err
	}
	colors, err := readPictureUint32(payload, &offset)
	if err != nil {
		return metadata.Thumbnail{}, err
	}
	dataLength, err := readPictureUint32(payload, &offset)
	if err != nil || uint64(dataLength) > uint64(len(payload)-offset) {
		return metadata.Thumbnail{}, errors.New("truncated picture data")
	}
	end := offset + int(dataLength)
	if end != len(payload) {
		return metadata.Thumbnail{}, errors.New("picture has trailing data")
	}
	thumbnail := metadata.Thumbnail{
		Data:          append([]byte(nil), payload[offset:end]...),
		MIMEType:      mimeType,
		PictureType:   metadata.PictureType(pictureType),
		Description:   description,
		Width:         width,
		Height:        height,
		ColorDepth:    depth,
		IndexedColors: colors,
	}
	if thumbnail.Width == 0 || thumbnail.Height == 0 {
		_, _ = thumbnail.FillImageProperties()
	}
	return thumbnail, nil
}

func MarshalPicture(thumb metadata.Thumbnail) []byte {
	mimeType := thumb.MIMEType
	if mimeType == "" {
		mimeType = http.DetectContentType(thumb.Data)
	}
	data := make([]byte, 0, len(thumb.Data)+64+len(mimeType)+len(thumb.Description))
	data = binary.BigEndian.AppendUint32(data, uint32(thumb.PictureType))
	data = appendPictureString(data, mimeType)
	data = appendPictureString(data, thumb.Description)
	data = binary.BigEndian.AppendUint32(data, thumb.Width)
	data = binary.BigEndian.AppendUint32(data, thumb.Height)
	data = binary.BigEndian.AppendUint32(data, thumb.ColorDepth)
	data = binary.BigEndian.AppendUint32(data, thumb.IndexedColors)
	data = binary.BigEndian.AppendUint32(data, uint32(len(thumb.Data)))
	return append(data, thumb.Data...)
}

func readPictureUint32(data []byte, offset *int) (uint32, error) {
	if len(data)-*offset < 4 {
		return 0, errors.New("truncated picture field")
	}
	value := binary.BigEndian.Uint32(data[*offset : *offset+4])
	*offset += 4
	return value, nil
}

func readPictureString(data []byte, offset *int) (string, error) {
	length, err := readPictureUint32(data, offset)
	if err != nil {
		return "", err
	}
	if uint64(length) > uint64(len(data)-*offset) {
		return "", errors.New("truncated picture string")
	}
	end := *offset + int(length)
	value := string(data[*offset:end])
	*offset = end
	return value, nil
}

func appendPictureString(data []byte, value string) []byte {
	data = binary.BigEndian.AppendUint32(data, uint32(len(value)))
	return append(data, value...)
}
