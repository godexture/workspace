# 同梱プラグイン仕様

このドキュメントでは `plugins/` 以下の同梱プラグインの仕様・挙動・設定を説明します。

---

## `format-wav` — WAV コンテナプラグイン

**モジュール:** `github.com/godexture/format-wav`  
**場所:** `plugins/format-wav/`  
**有効化:** ブランクインポート `_ "github.com/godexture/format-wav"`

### 概要

RIFF/WAV コンテナの読み書きをサポートします。

### Config 型

```go
type Config struct{}
```

Config は reflection 由来の plugin key としても使われます。marker method は不要です。

### Probe スコア

| 条件 | スコア |
|------|--------|
| RIFF/WAVE ヘッダーなし | `ProbeMismatch (0)` |
| RIFF/WAVE あり + バッファ不足 | `ProbeIncompleteSignature (90)` |
| RIFF/WAVE あり + `fmt ` チャンク確認 | `ProbeExactSignature (100)` |
| RIFF/WAVE あり + `fmt ` チャンクなし | `ProbeGenericContainer (25)` |

### サポートされる AudioFormat タグ

| WAV AudioFormat | コーデック | ビット深度 | SampleFormat |
|----------------|-----------|----------|-------------|
| `1` (PCM) | `CodecLPCM` | 8bit | `SampleFormatU8` |
| `1` (PCM) | `CodecLPCM` | 16bit | `SampleFormatS16` |
| `1` (PCM) | `CodecLPCM` | 32bit | `SampleFormatS32` |
| `3` (IEEE Float) | `CodecLPCM` | 32bit | `SampleFormatF32` |
| `3` (IEEE Float) | `CodecLPCM` | 64bit | `SampleFormatF64` |
| `6` (A-law) | `CodecPCMA` | 8bit | `SampleFormatU8` |
| `7` (μ-law) | `CodecPCMU` | 8bit | `SampleFormatU8` |

### Demuxer の挙動

- `Analyze()` でヘッダーを解析し、1つの `StreamInfo` を返す
- `ReadPacket()` で `data` チャンク全体を1つの `*media.Packet` として返す (以降は `io.EOF`)
- WAV は単一ストリームのみサポート
- `io.ReadSeeker` が必要 (通常の `io.Reader` は不可)

```go
// 低レベル API
demuxer, err := wav.NewDemuxer(r)           // *internal.Demuxer
// または
engine, err := wav.NewDemuxerEngine(r)      // engine.DemuxerEngine
// または (Node として)
node := eng.WrapDemuxer(wav.NewDemuxerEngine(r))
```

#### チャネルレイアウトマッピング

| チャネル数 | 割り当てるレイアウト |
|-----------|-------------------|
| 1 | `LayoutMono1` |
| 2 | `LayoutStereo2_0` |
| 3 | `LayoutStereo3_0` |
| 4 | `LayoutQuad4_0` |
| 5 | `LayoutFront5_0` |
| 6 | `LayoutFront5_1` |
| 7 | `LayoutSide7_0` |
| 8 | `LayoutSurround7_1` |
| それ以外 | `NewUnspecified(n)` |

### Muxer の挙動

- シングルオーディオストリームのみサポート
- `AddStream()` でストリームを1つだけ登録可能 (2つ目はエラー)
- パケットをすべてメモリに蓄積し、`WriteTrailer()` 時に一括書き出す
- データサイズが奇数バイトの場合、WAV 仕様に従い末尾に 0x00 パディングを追加

```go
muxer := wav.NewMuxer(w)           // *internal.Muxer
// または
muxEngine := wav.NewMuxerEngine(w) // engine.MuxerEngine
```

#### 出力される WAV ヘッダー構造

```
RIFF (4) + ChunkSize (4) + WAVE (4) +
fmt  (4) + 16 (4) + AudioFormat (2) + Channels (2) +
SampleRate (4) + ByteRate (4) + BlockAlign (2) + BitsPerSample (2) +
data (4) + DataSize (4) +
[PCM data...]
[0x00 パディング (奇数バイトの場合)]
```

### 制限事項

- `RIFF64` (BWF / RF64) は未サポート
- `LIST INFO`, `id3 ` チャンクなどの拡張チャンクは無視される
- Video ストリームは未サポート

---

## `codec-pcm` — PCM / G.711 コーデックプラグイン

**モジュール:** `github.com/godexture/codec-pcm`  
**場所:** `plugins/codec-pcm/`  
**有効化:** ブランクインポート `_ "github.com/godexture/codec-pcm"`

### 概要

Linear PCM (LPCM) および G.711 (μ-law / A-law) のエンコード・デコードをサポートします。

### サポートコーデック

| コーデック | CodecID | 説明 |
|-----------|---------|------|
| Linear PCM | `CodecLPCM` | 無圧縮 PCM |
| G.711 μ-law | `CodecPCMU` | 電話音声圧縮 (8kHz, 8bit, Mono) |
| G.711 A-law | `CodecPCMA` | 電話音声圧縮 (8kHz, 8bit, Mono) |

### Config 型

#### Decoder Config

```go
type Config struct {
    CodecID    media.CodecID
    SampleRate int
    Format     media.SampleFormat
    Layout     media.ChannelLayout
}

func DefaultConfig() Config
// → CodecID: CodecLPCM, SampleRate: 48000, Format: S16, Layout: Stereo2_0

func NewConfigWithAudio(sampleRate int, format media.SampleFormat, layout media.ChannelLayout) Config
```

#### Encoder Config

```go
type EncoderConfig struct {
    CodecID media.CodecID
}
```

### Decoder の挙動

1. `SendPacket(pkt)` でパケットを受け取る (一度に1つのみ)
2. `ReceiveFrame()` でデコード結果を取得する

| CodecID | デコード処理 |
|---------|------------|
| `CodecLPCM` | データをそのままコピー |
| `CodecPCMU` | μ-law → S16 に変換 (`ULawToLinear` × サンプル数) |
| `CodecPCMA` | A-law → S16 に変換 (`ALawToLinear` × サンプル数) |

- G.711 のデフォルト: SampleRate=8000, Format=S16, Layout=Mono1
- LPCM のデフォルト: SampleRate=48000, Format=S16, Layout=Stereo2_0

### Encoder の挙動

1. `SendFrame(frame)` でフレームを受け取る (`*media.AudioFrame` のみ受け付ける)
2. `ReceivePacket()` でエンコード結果を取得する

| CodecID | エンコード処理 |
|---------|------------|
| `CodecLPCM` | データをそのままコピー |
| `CodecPCMU` | S16 → μ-law に変換 (`LinearToULaw` × サンプル数) |
| `CodecPCMA` | S16 → A-law に変換 (`LinearToALaw` × サンプル数) |

- 出力パケット: `MediaType = MediaAudio`, `StreamIndex = 0`

### G.711 の数値変換詳細 (g711.go)

#### μ-law (U-law)

| 変換方向 | 関数 | 説明 |
|---------|------|------|
| μ-law → Linear | `ULawToLinear(uVal byte) int16` | ITU-T G.711 準拠の展開 |
| Linear → μ-law | `LinearToULaw(pcm int16) byte` | clip=32635, bias=0x84 |

バイト単位の一括変換:
```go
decoded := pcm_internal.DecodePCMU(data []byte) []byte  // len*2 の []byte
encoded := pcm_internal.EncodePCMU(data []byte) []byte  // len/2 の []byte
```

#### A-law

| 変換方向 | 関数 | 説明 |
|---------|------|------|
| A-law → Linear | `ALawToLinear(aVal byte) int16` | XOR 0x55 デスクランブル後に展開 |
| Linear → A-law | `LinearToALaw(pcm int16) byte` | XOR 0x55 スクランブル |

### 自動登録の詳細

`init()` では 3 つのコーデック (`CodecLPCM`, `CodecPCMU`, `CodecPCMA`) に対して  
それぞれ Decoder と Encoder のマニフェストを登録します。

Capability の `Match()` 実装:
```go
stream.Type == media.MediaAudio && stream.MediaAttributes.Codec == c.codec
```

TransformFunc (Decoder 用):
- `CodecPCMU`/`CodecPCMA` の場合: SampleRate を 8000 に、Format を S16 に、Layout を Mono1 に固定
- それ以外: StreamInfo の属性をそのまま引き継ぐ

---

## `filter-audio` — オーディオフィルタプラグイン

**モジュール:** `github.com/godexture/filter-audio`  
**場所:** `plugins/filter-audio/`

**有効化:** ブランクインポート `_ "github.com/godexture/filter-audio"`

### フィルター

| 設定型 | 登録名 | 用途 |
|--------|--------|------|
| `FormatConfig` | `audio-convert` | packed / planar を含む sample format と bit depth の変換 |
| `ResampleConfig` | `audio-resample` | 線形補間による sample rate 変換 |
| `RemixConfig` | `audio-remix` | channel layout の remap / downmix |
| `GainConfig` | `audio-gain` | dB 単位の gain |
| `NormalizeConfig` | `audio-normalize` | peak normalization |
| `FadeConfig` | `audio-fade` | 線形 fade in / fade out |
| `DCOffsetConfig` | `audio-dc-offset` | 1-pole DC offset 除去 |
| `TrimConfig` | `audio-trim` | 先頭・末尾無音の除去 |

`audio-convert`、`audio-resample`、`audio-remix` は `routing.Negotiator` の format bridge として登録される。後段の `AudioConstraint` が要求する format / rate / layout に入力が一致しない場合、明示的な filter 指定なしで必要な変換が探索・挿入される。エフェクト系フィルターは明示指定で使用する。

`normalize`、`fade`、`trim` は出力を確定するために入力を保持する。指定したメモリ上限を超える PCM サンプルは一時ファイルへ退避する。

---

## プラグインの組み合わせ例

### WAV → G.711 変換 (example/pcm.go より)

```go
import (
    pcm "github.com/godexture/codec-pcm"
    wav "github.com/godexture/format-wav"
)

// デマックス
demux, _ := wav.NewDemuxerEngine(inputFile)
streams, _, _ := demux.Analyze()

// デコード
a := streams[0].MediaAttributes.Audio
dec := pcm.NewDecoderEngine(pcm.NewConfigWithAudio(a.SampleRate, a.Format, a.ChannelLayout))

// G.711 μ-law エンコード
enc := pcm.NewEncoderEngine(pcm.EncoderConfig{CodecID: media.CodecPCMU})

// マックス (G.711 WAV として保存)
mux := wav.NewMuxerEngine(outputFile)
outStream := streams[0]
outStream.Codec = media.CodecPCMU
outStream.Audio.CodecID = media.CodecPCMU
outStream.Audio.Format = media.SampleFormatU8
mux.AddStream(outStream)
```
