# ドメインモデル詳細

このドキュメントでは `core/domain` 以下で定義される  
エンティティ・値オブジェクト・集約の関係と利用方法を説明します。

---

## 全体関係図

```
┌─────────────────────────────────────────────────────────────────┐
│                        media パッケージ                          │
│                                                                 │
│  ┌──────────────────────┐       ┌────────────────────────────┐  │
│  │      StreamInfo       │       │          Profile           │  │
│  │ - Index: int          │       │ - Type: MediaType          │  │
│  │ - Type: MediaType     │       │ - MediaAttributes          │  │
│  │ - Metadata: Bundle    │       └────────────────────────────┘  │
│  │ - IsDefault: bool     │                                       │
│  │ - MediaAttributes     │                                       │
│  └──────────────────────┘                                       │
│                                                                 │
│  ┌────────────────────────────┐                                  │
│  │       MediaAttributes      │                                  │
│  │ - Codec: CodecID           │                                  │
│  │ - Video: VideoAttributes   │ (未実装)                         │
│  │ - Audio: AudioAttributes   │                                  │
│  └────────────────────────────┘                                  │
│         │                                                       │
│         └──► AudioAttributes                                    │
│               - CodecID: CodecID                                │
│               - SampleRate: int                                 │
│               - Format: SampleFormat                            │
│               - ChannelLayout: ChannelLayout                    │
│                                                                 │
│  ┌──────────────┐       ┌──────────────────────────────────┐   │
│  │    Packet     │       │          AudioFrame              │   │
│  │ (ResourceBase)│       │       (ResourceBase)             │   │
│  │ - MediaType   │       │ - Format: SampleFormat           │   │
│  │ - StreamIndex │       │ - Layout: ChannelLayout          │   │
│  │ - PTS, DTS    │       │ - SampleRate: int                │   │
│  │ - Timebase    │       │ - Samples: int                   │   │
│  │ - data *[]byte│       │ - meta: *metadata.Bundle         │   │
│  └──────────────┘       │ - planes: [][]byte               │   │
│                         └──────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

---

## `StreamInfo` — ストリーム情報

Demuxer が `Analyze()` で返す各ストリームのメタデータです。  
パイプライン構築時にどのデコーダを使うかの判断に使われます。

```go
type StreamInfo struct {
    Index       int             // ストリームインデックス (0-based)
    Type        MediaType       // "audio", "video" など
    Duration    time.Duration   // 判定できる場合のストリーム尺。未知なら 0
    Metadata    metadata.Bundle // ストリームレベルのタグ情報
    IsDefault   bool            // デフォルトストリームかどうか
    MediaAttributes             // コーデック・音声属性など
}
```

尺は `Metadata` の任意キーではなく `Duration` を正規の表現とします。WAV、FLAC、MP3 の Demuxer はヘッダまたはファイル情報から判定できた場合に設定し、判定不能な stream はゼロのまま返します。

### 使用例

```go
// Demuxer から取得
streams, meta, err := demuxEngine.Analyze()

// ストリームタイプで絞り込み
for _, s := range streams {
    if s.Type == media.MediaAudio {
        fmt.Printf("Audio stream %d: %s %dHz %dch\n",
            s.Index, s.Audio.CodecID, s.Audio.SampleRate, s.Audio.ChannelCount())
    }
}
```

---

## `Profile` — メディアプロファイル

プラグイン選択・変換パス探索時に使う「入力/出力フォーマットの仕様書」です。  
`registry.TransformManifest.Transform()` が Node 構築前に出力 profile を解決するために利用します。

```go
type Profile struct {
    Type MediaType
    MediaAttributes
}

// キャッシュ・visited 判定のためのシグネチャ
func (p Profile) Signature() string
```

`StreamInfo` を `Profile` として扱う場合は構造体リテラルで変換します:

```go
profile := media.Profile{Type: s.Type, MediaAttributes: s.MediaAttributes}
```

---

## `Packet` — エンコード済みデータ

コンテナ(Demuxer)やコーデック(Encoder)が出力する**圧縮・生バイト列**を保持します。

```go
type Packet struct {
    ResourceBase           // 参照カウント
    MediaType   MediaType
    StreamIndex int
    PTS         Pts        // プレゼンテーションタイムスタンプ
    DTS         Dts        // デコードタイムスタンプ
    Timebase    time.Rational
    data        *[]byte    // pool からの借用バイトスライス
}

func (p *Packet) Data() []byte  // 生データへのアクセス
```

### 生成とライフサイクル

```go
// プールから取得 (refCount = 1 で初期化される)
pkt := media.NewPacket(1024, media.WithStreamIndex(0), media.WithPts(pts))

// データ書き込み
copy(pkt.Data(), rawData)

// 他の場所で参照を保持する場合
pkt.Retain()

// 参照を手放す
pkt.Release()  // refCount が 0 になると pool に返却
```

> **注意:** `NewPacket` で生成された直後の refCount は **1** です。  
> `pool.Put` は `Release()` 内部で自動的に呼ばれるため、直接呼ぶ必要はありません。

---

## `Frame` / `AudioFrame` — デコード済みフレーム

デコーダやフィルタが扱う**生サンプルデータ**を保持します。

```go
type Frame interface {
    Retainer
    Pts() Pts
    Metadata() *metadata.Bundle
}
```

現在実装されているのは `AudioFrame` のみです:

```go
type AudioFrame struct {
    ResourceBase
    Format     SampleFormat   // ex: SampleFormatS16P
    Layout     ChannelLayout
    SampleRate int            // ex: 48000
    Samples    int            // サンプル数 (フレームあたり)
    meta       *metadata.Bundle
    planes     [][]byte       // チャネル/プレーン別のスライス
}
```

### プレーン構造

| フォーマット | planes の構造 |
|------------|--------------|
| Interleaved (packed, 末尾に `p` なし) | `planes[0]` に全データ (LRLRLR...) |
| Planar (末尾に `p` あり) | `planes[i]` にチャネル i のデータ (LLLL... / RRRR...) |

```go
// 型安全なプレーンアクセス
samples := media.Plane[int16](frame, 0)  // 第0プレーンを []int16 として取得
```

### 生成

```go
frame := media.NewAudioFrame(
    media.SampleFormatS16,
    media.LayoutStereo2_0,
    48000,  // sample rate
    1024,   // sample count per channel
    media.WithAudioPts(pts),
)
defer frame.Release()

// データ書き込み
copy(frame.Planes()[0], pcmData)
```

---

## `ChannelLayout` — チャネルレイアウト

チャネルの配置を表す値オブジェクトです。  
3種類のモードがあります:

### Native レイアウト (ビットマスク)

```go
// ChannelPosition はビットフラグ
const (
    FrontLeft  ChannelPosition = 1 << iota
    FrontRight
    FrontCenter
    // ...
)

layout := media.NewNativeLayout(media.FrontLeft | media.FrontRight)
layout.ChannelCount() // → 2
layout.Contains(media.FrontLeft) // → true
layout.Index(media.FrontRight)   // → 1 (ビット列内の位置)
```

### Custom レイアウト (陽示的な順序)

```go
// チャネル順序を明示する（例: DualMono では FrontCenter が2回）
layout := media.NewCustomLayout(media.FrontCenter, media.FrontCenter)
layout.ChannelCount() // → 2

// 内部的には FNV ハッシュをキーにグローバルなマップへ登録される
// ID は CustomLayoutID (uint64) として管理
```

### Unspecified レイアウト

```go
// チャネル数のみ分かっている場合
layout := media.NewUnspecified(6)
layout.IsUnspecified() // → true
layout.ChannelCount()  // → 6
```

### Ambisonic レイアウト

```go
layout := media.NewAmbisonicLayout(1) // 1st-order (4ch)
layout.IsAmbisonic() // → true
layout.ChannelCount() // → (order+1)^2 = 4
```

---

## `metadata.Bundle` — タグ情報

フレームやストリームに付随する型安全なキー/値ストアです。

```go
bundle := metadata.NewBundle()

// 書き込み
bundle.Set(metadata.KeySilence{}, true)
bundle.Set(metadata.KeyVolume{}, 0.85)

// 型安全な読み込み
vol, err := metadata.Get[float64](bundle, metadata.KeyVolume{})
if errors.Is(err, metadata.ErrNotFound) {
    // キーが存在しない
}

// フレーム処理後のリセット
bundle.Clear()
```

### 標準キー

| キー型 | 値型 | 説明 |
|-------|------|------|
| `metadata.KeySilence{}` | `bool` | 無音フラグ |
| `metadata.KeyVolume{}` | `float64` | 音量レベル (0.0〜1.0) |
| `metadata.KeyIsKeyFrame{}` | `bool` | キーフレームかどうか |

> キー型はポインタではなく **値型の空構造体** を使う慣習です。  
> これにより型安全性が保証され、文字列キーとの衝突を避けられます。

---

## `manifest.Capability` — プラグイン能力宣言

プラグインが「どのような入力を受け付けるか」を宣言するインターフェースです。

```go
type Capability interface {
    Match(p media.StreamInfo) bool
    Diagnose(p media.StreamInfo) error
}
```

### `AudioConstraint` の使い方

```go
cap := &manifest.AudioConstraint{
    SampleRates: manifest.IntConstraint{Values: []int{44100, 48000}},
    Channels:    manifest.IntConstraint{Values: []int{1, 2}},
    Layouts:     []media.ChannelLayout{media.LayoutMono1, media.LayoutStereo2_0},
    SampleFormats: []manifest.SampleFormatConstraint{{Format: media.SampleFormatS16}},
}

cap.Match(stream)    // → bool
cap.Diagnose(stream) // → error (詳細なエラーメッセージ付き)
```

---

## `manifest.Prober` — フォーマット検出関数

Demuxer プラグインが提供するフォーマット検出ロジックです。

```go
type Prober func(r io.Reader) ProbeScore
```

`DefaultDemuxerResolver` は全登録 Demuxer の `Probe` を呼び出し、  
スコアが最大のものを選択します。

```
ProbeMismatch (0)        → 明らかに違う
ProbeExtensionOnly (10)  → 拡張子のみで判断
ProbeGenericContainer(25)→ RIFF ヘッダー等の汎用シグネチャを確認
ProbeSharedMetadata (40) → ID3v2 等の共有メタデータ確認
ProbeSingleSync (60)     → 同期パターン1つ確認
ProbeMultipleSync (80)   → 複数同期パターン確認
ProbeIncompleteSignature(90)→ シグネチャは一致するがバッファ不足
ProbeExactSignature (100)→ 完全一致
```
