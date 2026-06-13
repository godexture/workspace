# codec-mp3 — MP3 コーデックプラグイン

**モジュール:** `github.com/godexture/codec-mp3`  
**場所:** `plugins/codec-mp3/`  
**有効化:** ブランクインポート `_ "github.com/godexture/codec-mp3"`

## 概要

MP3 のデコード・エンコードを担当します。

## サポート状況 (v0.1)

| 機能 | 状態 | 備考 |
|------|------|------|
| Decoder (MP3 → PCM) | ✅ 実装済み | go-mp3 使用 |
| Encoder (PCM → MP3) | ⛔ Stub | 未実装 (常に ErrEAGAIN) |

## デコーダ仕様

- **入力:** `CodecMPEG3` パケット (MP3 バイト列)
- **出力:** `SampleFormatS16`, `LayoutStereo2_0`
- **バックエンド:** `github.com/hajimehoshi/go-mp3 v0.3.4`

### 制約事項

- 出力は常に **16-bit signed LE, Stereo** (go-mp3 ライブラリの制約)
- Mono MP3 をデコードした場合も Stereo として出力される

## エンコーダ (未実装)

現在、エンコーダは STUB です。`ReceivePacket()` は常に `engine.ErrEAGAIN` を返します。

## 今後実装すべき内容 (TODO)

### エンコーダ実装

以下のいずれかのアプローチで実装する:

1. **`github.com/braheezy/shine-mp3`** (pure Go)
   - ライセンス確認が必要 (pkg.go.dev で "None detected")
   - 固定小数点演算ベースのため品質は低め
   - 依存なし (pure Go)

2. **`github.com/git-jiadong/go-lame`** (CGO)
   - LAME ライブラリへのバインディング
   - 高品質だが CGO が必要 (クロスコンパイルが難しくなる)
   - libmp3lame の別途インストールが必要

3. **外部 CLI 呼び出し**
   - `ffmpeg` または `lame` コマンドを `os/exec` で呼び出す
   - 環境に依存するが実装が最もシンプル

### デコーダ改善

- [ ] **Mono MP3 の対応**: Stereo に強制変換する代わりに、入力 MP3 のチャンネル数を解析して正確に報告する
- [ ] **サンプルフォーマット選択**: S16 以外のフォーマット (F32 等) への変換オプション
- [ ] **ストリーミングデコード**: フレーム単位でのデコード (現在はパケット全体を一度にデコード)
- [ ] **VBR/CBR メタデータ**: Xing ヘッダからビットレート・フレーム数を取得
