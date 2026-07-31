package domain

import "errors"

var (
	ErrBitStreamUnderflow    = errors.New("bitStream buffer underflow")
	ErrInvalidSideInfo       = errors.New("invalid side info")
	ErrInsufficientReservoir = errors.New("insufficient data in reservoir")
	ErrNilPacket             = errors.New("codec-mp3 decoder: received nil packet")
	ErrFormatChanged         = errors.New("codec-mp3 decoder: sample rate or channels changed mid-stream")
	ErrNilFrame              = errors.New("codec-mp3 encoder: received nil frame")
)
