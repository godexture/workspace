package internal

import (
	"bufio"
	"io"

	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/format-mp3/header"
)

// Probe はMP3ファイルを検出する。
// MP3同期ワード (0xFF 0xEx ~ 0xFF 0xFx) またはID3v2ヘッダを確認する。
func Probe(r io.Reader) manifest.ProbeScore {
	br := bufio.NewReaderSize(r, header.ID3v2HeaderSize)
	headerBytes, err := br.Peek(header.ID3v2HeaderSize)
	if err != nil && len(headerBytes) < 2 {
		return manifest.ProbeMismatch
	}

	hasID3Header := len(headerBytes) >= 3 && string(headerBytes[0:3]) == "ID3"
	hasSyncWord := isMP3SyncWord(headerBytes)

	switch {
	case hasID3Header && hasSyncWord:
		return manifest.ProbeMultipleSync
	case hasID3Header:
		return manifest.ProbeSharedMetadata
	case hasSyncWord:
		return manifest.ProbeSingleSync
	default:
		return manifest.ProbeMismatch
	}
}

// isMP3SyncWord はバッファの先頭にMP3フレームの同期ワードがあるか確認する。
// MP3フレームヘッダは 0xFF から始まり、次のバイトの上位5ビットが全て1でなければならない。
// さらに MPEG version と layer のフィールドを確認して誤検出を減らす。
func isMP3SyncWord(buffer []byte) bool {
	if len(buffer) < 2 {
		return false
	}
	// 同期ワード: byte[0] == 0xFF, byte[1] の上位5ビットが 0b11111 (0xE0)
	if buffer[0] != 0xFF || (buffer[1]&0xE0) != 0xE0 {
		return false
	}
	// MPEG version: bit 19-20 → 00=予約済, それ以外は有効
	mpegVersion := (buffer[1] >> 3) & 0x03
	if mpegVersion == 0x01 { // 予約済 → 無効
		return false
	}
	// Layer: bit 17-18 → 00=予約済, それ以外 is valid
	layer := (buffer[1] >> 1) & 0x03
	if layer == 0x00 { // 予約済 → 無効
		return false
	}
	return true
}
