package internal

import (
	"github.com/godexture/godec/plugin/wave/params"
)

const (
	wavTagRIFF     = "RIFF"
	wavTagWAVE     = "WAVE"
	wavTagFmt      = "fmt "
	wavTagFact     = "fact"
	wavTagData     = "data"
	wavTagRF64     = "RF64"
	wavTagDS64     = "ds64"
	wavTagLIST     = "LIST"
	wavTagINFO     = "INFO"
	wavTagID3      = "id3 "
	wavTagID3Upper = "ID3 "
	wavTagCue      = "cue "
	wavTagSmpl     = "smpl"

	wavInfoTagTitle     = "INAM"
	wavInfoTagArtist    = "IART"
	wavInfoTagDate      = "ICRD"
	wavInfoTagComment   = "ICMT"
	wavInfoTagGenre     = "IGNR"
	wavInfoTagAlbum     = "IPRD"
	wavInfoTagEncoder   = "ISFT"
	wavInfoTagCopyright = "ICOP"

	wavAudioPCM        = 1
	wavAudioMSADPCM    = 2
	wavAudioIEEEFloat  = 3
	wavAudioALaw       = 6
	wavAudioULaw       = 7
	wavAudioIMAADPCM   = 0x0011
	wavAudioGSM        = 0x0031
	wavAudioMP3        = 0x0055
	wavAudioExtensible = 0xFFFE
)

var wavSubFormatBase = []byte{0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}

type wavHeader struct {
	audioFormat   uint16
	channels      uint16
	sampleRate    uint32
	bitsPerSample uint16
	blockAlign    uint16
	adpcm         *params.ADPCM

	validBits   uint16
	channelMask uint32
	subFormat   [16]byte

	numSamples uint64

	dataOffset int64
	dataSize   uint64
}
