# format-mp3 — MP3 コンテナプラグイン

**モジュール:** `github.com/godexture/format-mp3`  
**場所:** `plugins/format-mp3/`  
**有効化:** ブランクインポート `_ "github.com/godexture/format-mp3"`

## 概要

MP3 コンテナの読み書きをサポートします。

## サポート状況 (v0.1)

| 機能 | 状態 |
|------|------|
| Demuxer (MP3 読み込み) | ✅ 実装済み |
| Muxer (MP3 書き込み) | ✅ 実装済み |

## 制限事項・既知の問題

- Demuxer はファイル全体を1パケットとして返す (ストリーミング非対応)
- チャンネル情報は常に Stereo として報告される (go-mp3 ライブラリの制約)
- ID3v2 / ID3v1 タグの読み書きは未サポート

## 今後実装すべき内容 (TODO)

- [ ] **ストリーミング対応**: フレーム単位でのパケット読み込み (現在はファイル全体を1パケット)
- [ ] **ID3v2 タグの読み込み**: `metadata.Bundle` への格納 (KeyTitle, KeyArtist 等)
- [ ] **ID3v2 タグの書き込み**: Muxer の `SetMetadata()` で ID3v2 ヘッダを出力
- [ ] **Mono MP3 の正確なチャンネル情報**: ID3v2 または MPEGフレームヘッダのチャンネルモードフィールドを解析
- [ ] **VBR (Variable Bit Rate) 対応**: Xing/VBRI ヘッダの解析
- [ ] **シーク対応**: バイトオフセットではなくサンプル単位でのシーク
