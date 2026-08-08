package id3v2

type Version byte

const (
	Version2 Version = 2
	Version3 Version = 3
	Version4 Version = 4
)

type Encoding byte

const (
	EncodingISO88591 Encoding = 0x00
	EncodingUTF16    Encoding = 0x01
	EncodingUTF16BE  Encoding = 0x02
	EncodingUTF8     Encoding = 0x03
	EncodingDefault  Encoding = EncodingISO88591
)

type MarshalOptions struct {
	Version  Version
	Encoding Encoding
}
