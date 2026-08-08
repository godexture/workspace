package id3v2

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/godexture/godec/core/domain/metadata"
	"github.com/godexture/godec/sdk/date"
)

type dateContext struct {
	year string
	tdat string
	time string
	tdrc string
}

func Parse(buffer []byte, bundle *metadata.Bundle) {
	offset := 0
	ctx := &dateContext{}

	for len(buffer[offset:]) >= HeaderSize {
		header, err := ParseHeader(buffer[offset:])
		if err != nil {
			return
		}

		tagDataStart := offset + HeaderSize
		tagDataEnd := tagDataStart + header.TagSize
		tagEnd := offset + header.TotalSize()
		if tagDataEnd > len(buffer) || tagEnd > len(buffer) {
			return
		}

		tagPayload := buffer[tagDataStart:tagDataEnd]
		if header.Flags&0x80 != 0 {
			tagPayload = removeUnsynchronisation(tagPayload)
		}

		parseFrames(header.VersionMajor, tagPayload, bundle, ctx)

		offset = tagEnd
	}

	var parsedDate date.Partial
	var err error
	if ctx.tdrc != "" {
		parsedDate, err = date.NewPartial(ctx.tdrc)
	} else if ctx.year != "" {
		dateStr := ctx.year
		if len(ctx.tdat) == 4 {
			day := ctx.tdat[:2]
			month := ctx.tdat[2:]
			dateStr = fmt.Sprintf("%s-%s-%s", ctx.year, month, day)
			if len(ctx.time) == 4 {
				hour := ctx.time[:2]
				minute := ctx.time[2:]
				dateStr = fmt.Sprintf("%sT%s:%s", dateStr, hour, minute)
			}
		}
		parsedDate, err = date.NewPartial(dateStr)
	}

	if err == nil && parsedDate.HasValue() {
		bundle.SetNonZero(metadata.KeyDate(parsedDate))
	}
}

func parseFrames(version byte, payload []byte, bundle *metadata.Bundle, ctx *dateContext) {
	frameHeaderSize := 10
	if version == 2 {
		frameHeaderSize = 6
	}

	for offset := 0; offset+frameHeaderSize <= len(payload); {
		frameID, frameSize, flags, nextOffset, ok := parseFrameHeader(version, payload, offset)
		if !ok {
			return
		}
		if frameSize <= 0 || nextOffset+frameSize > len(payload) {
			return
		}

		frameData := payload[nextOffset : nextOffset+frameSize]

		if len(flags) == 2 {
			compressed := false
			encrypted := false
			hasDataLength := false
			unsync := false

			if version == 3 {
				compressed = (flags[1] & 0x80) != 0
				encrypted = (flags[1] & 0x40) != 0
			} else if version == 4 {
				compressed = (flags[1] & 0x08) != 0
				encrypted = (flags[1] & 0x04) != 0
				unsync = (flags[1] & 0x02) != 0
				hasDataLength = (flags[1] & 0x01) != 0
			}

			if compressed {
				offset = nextOffset + frameSize
				continue
			}

			if hasDataLength {
				if len(frameData) >= 4 {
					frameData = frameData[4:]
				}
			}

			if encrypted {
				if len(frameData) >= 1 {
					frameData = frameData[1:]
				}
			}
			if unsync {
				frameData = removeUnsynchronisation(frameData)
			}
		}

		if strings.Trim(frameID, "\x00") == "" {
			return
		}
		applyFrame(bundle, frameID, frameData, ctx)
		offset = nextOffset + frameSize
	}
}

func parseFrameHeader(version byte, payload []byte, offset int) (string, int, []byte, int, bool) {
	if version == 2 {
		if offset+6 > len(payload) {
			return "", 0, nil, 0, false
		}
		headerBytes := payload[offset : offset+6]
		if bytes.Equal(headerBytes[:3], []byte{0, 0, 0}) {
			return "", 0, nil, 0, false
		}
		frameID := string(headerBytes[:3])
		frameSize := int(headerBytes[3])<<16 | int(headerBytes[4])<<8 | int(headerBytes[5])
		return frameID, frameSize, nil, offset + 6, true
	}

	if offset+10 > len(payload) {
		return "", 0, nil, 0, false
	}
	headerBytes := payload[offset : offset+10]
	if bytes.Equal(headerBytes[:4], []byte{0, 0, 0, 0}) {
		return "", 0, nil, 0, false
	}
	frameID := string(headerBytes[:4])
	frameSize := int(binary.BigEndian.Uint32(headerBytes[4:8]))
	if version == 4 {
		frameSize = DecodeSyncSafeInt(headerBytes[4:8])
	}
	flags := headerBytes[8:10]
	return frameID, frameSize, flags, offset + 10, true
}

func applyFrame(bundle *metadata.Bundle, frameID string, frameData []byte, ctx *dateContext) {
	switch frameID {
	case "TIT2", "TT2":
		bundle.SetNonZero(metadata.KeyTitle(decodeTextFrame(frameData)))

	case "TPE1", "TP1":
		artists := decodeTextFrames(frameData)
		for _, a := range artists {
			bundle.PushBack(metadata.KeyArtist(a))
		}

	case "TPE2", "TP2", "TPE3", "TP3", "TPE4", "TP4":
		artists := decodeTextFrames(frameData)
		for _, a := range artists {
			bundle.PushBack(metadata.KeyArtist(a))
		}

	case "TCOM", "TCM":
		bundle.SetNonZero(metadata.KeyComposer(decodeTextFrame(frameData)))

	case "TALB", "TAL":
		bundle.SetNonZero(metadata.KeyAlbum(decodeTextFrame(frameData)))

	case "TRCK", "TRK":
		trackNumber, totalTracks := decodeTrackOrDiskNumberFrame(frameData)
		bundle.SetNonZero(metadata.KeyTrackNumber(trackNumber))
		bundle.SetNonZero(metadata.KeyTotalTracks(totalTracks))

	case "TPOS", "TPA":
		discNumber, totalDiscs := decodeTrackOrDiskNumberFrame(frameData)
		bundle.SetNonZero(metadata.KeyDiscNumber(discNumber))
		bundle.SetNonZero(metadata.KeyTotalDiscs(totalDiscs))

	case "TYER", "TYE":
		ctx.year = decodeTextFrame(frameData)

	case "TDRC":
		ctx.tdrc = decodeTextFrame(frameData)

	case "TDAT", "TDA":
		ctx.tdat = decodeTextFrame(frameData)

	case "TIME", "TIM":
		ctx.time = decodeTextFrame(frameData)

	case "TCON", "TCO":
		bundle.SetNonZero(metadata.KeyGenre(decodeTextFrame(frameData)))

	case "TCOP", "TCR":
		bundle.SetNonZero(metadata.KeyCopyright(decodeTextFrame(frameData)))

	case "TENC", "TEN":
		bundle.SetNonZero(metadata.KeyEncoder(decodeTextFrame(frameData)))

	case "WXXX", "WXX":
		bundle.Set(metadata.KeyWebsite(decodeUserURLValue(frameData)))

	case "WOAF", "WAF", "WOAR", "WAR", "WOAS", "WAS":
		bundle.PushBack(metadata.KeyWebsite(decodeURLFrame(frameData)))

	case "COMM", "COM":
		bundle.SetNonZero(metadata.KeyComment(decodeEncodedTextFrame(frameData)))

	case "USLT", "ULT":
		bundle.SetNonZero(metadata.KeyLyrics(decodeEncodedTextFrame(frameData)))

	case "APIC":
		addThumbnail(bundle, decodeAPICFrame(frameData))
		bundle.AddRaw(frameID, frameData)

	case "PIC":
		addThumbnail(bundle, decodePICFrame(frameData))
		bundle.AddRaw(frameID, frameData)

	default:
		bundle.AddRaw(frameID, frameData)
	}
}
