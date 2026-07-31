package media

type CodecID string

const (
	CodecLPCM     CodecID = "lpcm"
	CodecMPEG     CodecID = "mpeg"
	CodecPCMU     CodecID = "pcmu"
	CodecPCMA     CodecID = "pcma"
	CodecMP3      CodecID = "mp3"
	CodecMSADPCM  CodecID = "adpcm_ms"
	CodecIMAADPCM CodecID = "adpcm_ima"
	CodecGSM      CodecID = "gsm"
	CodecFLAC     CodecID = "flac"
)
