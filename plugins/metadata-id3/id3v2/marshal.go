package id3v2

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/sdk/date"
)

const commentLanguage = "eng"

var frameIDs = map[string]string{
	"AENC": "CRA",
	"APIC": "PIC",
	"COMM": "COM",
	"EQU2": "EQU",
	"EQUA": "EQU",
	"ETCO": "ETC",
	"GEOB": "GEO",
	"GRID": "GRI",
	"IPLS": "IPL",
	"LINK": "LNK",
	"MCDI": "MCI",
	"MLLT": "MLL",
	"PCNT": "CNT",
	"POPM": "POP",
	"PRIV": "PRI",
	"RBUF": "BUF",
	"RVA2": "RVA",
	"RVAD": "RVA",
	"RVRB": "REV",
	"SYLT": "SLT",
	"SYTC": "STC",
	"TALB": "TAL",
	"TBPM": "TBP",
	"TCOM": "TCM",
	"TCON": "TCO",
	"TCOP": "TCR",
	"TDAT": "TDA",
	"TDLY": "TDY",
	"TDOR": "TOR",
	"TDRC": "TYE",
	"TENC": "TEN",
	"TEXT": "TXT",
	"TFLT": "TFT",
	"TIME": "TIM",
	"TIPL": "IPL",
	"TIT1": "TT1",
	"TIT2": "TT2",
	"TIT3": "TT3",
	"TKEY": "TKE",
	"TLAN": "TLA",
	"TLEN": "TLE",
	"TMCL": "IPL",
	"TMED": "TMT",
	"TOAL": "TOT",
	"TOFN": "TOF",
	"TOLY": "TOL",
	"TOPE": "TOA",
	"TORY": "TOR",
	"TPE1": "TP1",
	"TPE2": "TP2",
	"TPE3": "TP3",
	"TPE4": "TP4",
	"TPOS": "TPA",
	"TPUB": "TPB",
	"TRCK": "TRK",
	"TRDA": "TRD",
	"TSRC": "TRC",
	"TSIZ": "TSI",
	"TSSE": "TSS",
	"TXXX": "TXX",
	"TYER": "TYE",
	"UFID": "UFI",
	"USLT": "ULT",
	"WCOM": "WCM",
	"WCOP": "WCP",
	"WOAF": "WAF",
	"WOAR": "WAR",
	"WOAS": "WAS",
	"WPUB": "WPB",
	"WXXX": "WXX",
}

type encoder struct {
	opts   MarshalOptions
	frames [][]byte
}

func Marshal(bundle metadata.Bundle, opts MarshalOptions) ([]byte, error) {
	if opts.Version == 0 {
		opts.Version = Version3
	}

	e := &encoder{
		opts:   opts,
		frames: make([][]byte, 0, 16),
	}

	e.addTextFrame("TIT2", string(metadata.Get[metadata.KeyTitle](&bundle)))

	var artists []string
	for _, a := range metadata.Enumerate[metadata.KeyArtist](&bundle) {
		artists = append(artists, string(a))
	}
	if len(artists) > 0 {
		e.addTextFrame("TPE1", strings.Join(artists, "\x00"))
	}

	e.addTextFrame("TALB", string(metadata.Get[metadata.KeyAlbum](&bundle)))

	// Date mapping
	dateVal := date.Partial(metadata.Get[metadata.KeyDate](&bundle))
	if dateVal.HasValue() {
		if e.opts.Version == Version4 {
			e.addTextFrame("TDRC", dateVal.ToISOString())
		} else {
			var yearKey, dateKey string
			if e.opts.Version == Version2 {
				yearKey = "TYE"
				dateKey = "TDA"
			} else {
				yearKey = "TYER"
				dateKey = "TDAT"
			}
			if dateVal.Year().Exists() {
				e.addTextFrame(yearKey, strconv.Itoa(int(dateVal.Year().Unwrap())))
			}
			if dateVal.Month().Exists() && dateVal.Day().Exists() {
				tdata := fmt.Sprintf("%02d%02d", dateVal.Day().Unwrap(), dateVal.Month().Unwrap())
				e.addTextFrame(dateKey, tdata)
			}
		}
	}

	e.addTextFrame("TCON", string(metadata.Get[metadata.KeyGenre](&bundle)))
	e.addTextFrame("TCOM", string(metadata.Get[metadata.KeyComposer](&bundle)))
	e.addTextFrame("TCOP", string(metadata.Get[metadata.KeyCopyright](&bundle)))
	e.addTextFrame("TENC", string(metadata.Get[metadata.KeyEncoder](&bundle)))
	e.addCommentFrame(string(metadata.Get[metadata.KeyComment](&bundle)))
	e.addLyricsFrame(string(metadata.Get[metadata.KeyLyrics](&bundle)))
	e.addUserURLFrame("", string(metadata.Get[metadata.KeyWebsite](&bundle)))

	thumbnails := metadata.Get[metadata.KeyThumbnail](&bundle)
	if len(thumbnails) > 0 {
		e.addAttachedPictureFrames(thumbnails)
	} else if apics, ok := bundle.GetRaw("APIC"); ok {
		for _, apic := range apics {
			e.addRawAttachedPictureFrame(apic)
		}
	} else if pics, ok := bundle.GetRaw("PIC"); ok {
		for _, pic := range pics {
			if e.opts.Version != Version2 {
				thumb := decodePICFrame(pic)
				if len(thumb.Data) > 0 {
					e.addAttachedPictureFrame(thumb)
				}
			} else {
				e.frames = append(e.frames, e.buildFrame("PIC", pic))
			}
		}
	}

	rawFrames := bundle.AllRaw()
	for frameID, payloads := range rawFrames {
		if frameID == "APIC" || frameID == "PIC" {
			continue
		}
		for _, payload := range payloads {
			mappedID := e.mapFrameID(frameID)
			e.frames = append(e.frames, e.buildFrame(mappedID, payload))
		}
	}

	if len(e.frames) == 0 {
		return nil, nil
	}

	payloadSize := 0
	for _, frame := range e.frames {
		payloadSize += len(frame)
	}

	tag := make([]byte, 0, HeaderSize+payloadSize)
	tag = append(tag, 'I', 'D', '3', byte(e.opts.Version), 0x00, 0x00)
	tag = append(tag, EncodeSyncSafeInt(payloadSize)...)
	for _, frame := range e.frames {
		tag = append(tag, frame...)
	}
	return tag, nil
}

func (e *encoder) mapFrameID(id string) string {
	if e.opts.Version == Version2 {
		if mapped, ok := frameIDs[id]; ok {
			return mapped
		}
	}
	return id
}

func (e *encoder) addTextFrame(frameID string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	encByte, valBytes := encodeText(value, e.opts.Encoding, e.opts.Version)
	payload := append([]byte{encByte}, valBytes...)
	mappedID := e.mapFrameID(frameID)
	e.frames = append(e.frames, e.buildFrame(mappedID, payload))
}

func (e *encoder) addURLFrame(frameID string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	mappedID := e.mapFrameID(frameID)
	e.frames = append(e.frames, e.buildFrame(mappedID, []byte(value)))
}

func (e *encoder) addUserURLFrame(description string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	encByte, descBytes := encodeText(description, e.opts.Encoding, e.opts.Version)
	payload := []byte{encByte}
	payload = append(payload, descBytes...)
	payload = append(payload, e.getTerminator(encByte)...)
	payload = append(payload, []byte(value)...)
	mappedID := e.mapFrameID("WXXX")
	e.frames = append(e.frames, e.buildFrame(mappedID, payload))
}

func (e *encoder) addCommentFrame(value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	encByte, valBytes := encodeText(value, e.opts.Encoding, e.opts.Version)
	payload := []byte{encByte}
	payload = append(payload, []byte(commentLanguage)...)
	payload = append(payload, e.getTerminator(encByte)...)
	payload = append(payload, valBytes...)
	mappedID := e.mapFrameID("COMM")
	e.frames = append(e.frames, e.buildFrame(mappedID, payload))
}

func (e *encoder) addLyricsFrame(value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	encByte, valBytes := encodeText(value, e.opts.Encoding, e.opts.Version)
	payload := []byte{encByte}
	payload = append(payload, []byte(commentLanguage)...)
	payload = append(payload, e.getTerminator(encByte)...)
	payload = append(payload, valBytes...)
	mappedID := e.mapFrameID("USLT")
	e.frames = append(e.frames, e.buildFrame(mappedID, payload))
}

func (e *encoder) addAttachedPictureFrames(thumbnails []metadata.Thumbnail) {
	for _, thumbnail := range thumbnails {
		e.addAttachedPictureFrame(thumbnail)
	}
}

func (e *encoder) addAttachedPictureFrame(thumbnail metadata.Thumbnail) {
	if len(thumbnail.Data) == 0 {
		return
	}

	mimeType := strings.TrimSpace(thumbnail.MIMEType)
	if mimeType == "" {
		mimeType = metadata.DetectImageMIME(thumbnail.Data)
	}

	pictureType := byte(thumbnail.PictureType)
	description := strings.TrimSpace(thumbnail.Description)

	encByte, descBytes := encodeText(description, e.opts.Encoding, e.opts.Version)

	var payload []byte
	var mappedID string

	if e.opts.Version == Version2 {
		format := mimeToFormat(mimeType)
		payload = []byte{encByte}
		payload = append(payload, []byte(format)...)
		payload = append(payload, pictureType)
		payload = append(payload, descBytes...)
		payload = append(payload, e.getTerminator(encByte)...)
		payload = append(payload, thumbnail.Data...)
		mappedID = "PIC"
	} else {
		payload = []byte{encByte}
		payload = append(payload, []byte(mimeType)...)
		payload = append(payload, 0x00, pictureType)
		payload = append(payload, descBytes...)
		payload = append(payload, e.getTerminator(encByte)...)
		payload = append(payload, thumbnail.Data...)
		mappedID = "APIC"
	}

	e.frames = append(e.frames, e.buildFrame(mappedID, payload))
}

func (e *encoder) addRawAttachedPictureFrame(payload []byte) {
	if len(payload) == 0 {
		return
	}
	if e.opts.Version == Version2 {
		thumb := decodeAPICFrame(payload)
		if len(thumb.Data) > 0 {
			e.addAttachedPictureFrame(thumb)
			return
		}
	}
	mappedID := e.mapFrameID("APIC")
	e.frames = append(e.frames, e.buildFrame(mappedID, payload))
}

func (e *encoder) getTerminator(encByte byte) []byte {
	if encByte == 1 || encByte == 2 {
		return []byte{0x00, 0x00}
	}
	return []byte{0x00}
}

func (e *encoder) buildFrame(frameID string, payload []byte) []byte {
	if e.opts.Version == Version2 {
		id := frameID
		if len(id) > 3 {
			id = id[:3]
		}
		for len(id) < 3 {
			id += " "
		}

		frame := make([]byte, 0, 6+len(payload))
		frame = append(frame, []byte(id)...)

		size := len(payload)
		frame = append(frame,
			byte((size>>16)&0xFF),
			byte((size>>8)&0xFF),
			byte(size&0xFF),
		)
		frame = append(frame, payload...)
		return frame
	}

	id := frameID
	if len(id) > 4 {
		id = id[:4]
	}
	for len(id) < 4 {
		id += " "
	}

	frame := make([]byte, 0, 10+len(payload))
	frame = append(frame, []byte(id)...)

	size := len(payload)
	if e.opts.Version == Version4 {
		frame = append(frame, EncodeSyncSafeInt(size)...)
	} else {
		frame = append(frame,
			byte((size>>24)&0xFF),
			byte((size>>16)&0xFF),
			byte((size>>8)&0xFF),
			byte(size&0xFF),
		)
	}

	frame = append(frame, 0x00, 0x00) // flags
	frame = append(frame, payload...)
	return frame
}

func mimeToFormat(mime string) string {
	mime = strings.ToLower(mime)
	if strings.Contains(mime, "png") {
		return "PNG"
	}
	if strings.Contains(mime, "jpeg") || strings.Contains(mime, "jpg") {
		return "JPG"
	}
	parts := strings.Split(mime, "/")
	if len(parts) > 1 {
		format := strings.ToUpper(parts[1])
		if len(format) > 3 {
			format = format[:3]
		}
		if len(format) == 3 {
			return format
		}
	}
	return "   "
}
