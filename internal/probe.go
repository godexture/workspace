package internal

import (
	"bufio"
	"io"

	"github.com/godexture/core/domain/manifest"
)

// Probe はMP3ファイルを検出する。
// MP3同期ワード (0xFF 0xEx ~ 0xFF 0xFx) またはID3v2ヘッダを確認する。
func Probe(r io.Reader) manifest.ProbeScore {
	reader := bufio.NewReaderSize(r, 10)
	header, err := reader.Peek(10)
	if err != nil && len(header) < 2 {
		return manifest.ProbeMismatch
	}

	hasID3 := len(header) >= 3 && string(header[0:3]) == "ID3"
	hasSync := isMP3SyncWord(header)

	switch {
	case hasID3 && hasSync:
		return manifest.ProbeMultipleSync
	case hasID3:
		return manifest.ProbeSharedMetadata
	case hasSync:
		return manifest.ProbeSingleSync
	default:
		return manifest.ProbeMismatch
	}
}

// isMP3SyncWord はバッファの先頭にMP3フレームの同期ワードがあるか確認する。
// MP3フレームヘッダは 0xFF から始まり、次のバイトの上位5ビットが全て1でなければならない。
// さらに MPEG version と layer のフィールドを確認して誤検出を減らす。
func isMP3SyncWord(buf []byte) bool {
	if len(buf) < 2 {
		return false
	}
	// 同期ワード: byte[0] == 0xFF, byte[1] の上位5ビットが 0b11111 (0xE0)
	if buf[0] != 0xFF || (buf[1]&0xE0) != 0xE0 {
		return false
	}
	// MPEG version: bit 19-20 → 00=予約済, それ以外は有効
	mpegVersion := (buf[1] >> 3) & 0x03
	if mpegVersion == 0x01 { // 予約済 → 無効
		return false
	}
	// Layer: bit 17-18 → 00=予約済, それ以外は有効
	layer := (buf[1] >> 1) & 0x03
	if layer == 0x00 { // 予約済 → 無効
		return false
	}
	return true
}
