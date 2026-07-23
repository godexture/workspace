# パッケージ / モジュール リファレンス

各 Go モジュール・パッケージの責務・エクスポートされた型・関数を網羅的に記述します。

---

## `github.com/godexture/core` — コアフレームワーク

**場所:** `packages/core/`

### パッケージ `godec` (ルート)

```go
import godec "github.com/godexture/core"
```

| シンボル | 型 | 説明 |
|---------|-----|------|
| `DefaultRegistry` | `registry.Bundle` | アプリケーション全体で共有するデフォルトレジストリ。各プラグインの `init()` がここに登録する |

---

### パッケージ `node`

```go
import "github.com/godexture/core/node"
```

ノードとポートのインターフェース定義を提供します。

#### インターフェース

| 型 | メソッド | 説明 |
|---|---------|------|
| `Lifecycle` | `Start(ctx context.Context) error`, `Close() error` | 実行と idempotent な resource 解放 |
| `Node` | `Lifecycle` | 全ノードの基底インターフェース |
| `InputNode[T]` | `InputPorts() map[string]*InPort[T]` | 入力ポートを持つノード |
| `OutputNode[T]` | `OutputPorts() map[string]*OutPort[T]` | 出力ポートを持つノード |
| `Encoder` | `Node`, `InputPorts() map[string]*InPort[media.Frame]`, `OutputPorts() map[string]*OutPort[*media.Packet]` | フレーム → パケット |
| `Decoder` | `Node`, `InputPorts() map[string]*InPort[*media.Packet]`, `OutputPorts() map[string]*OutPort[media.Frame]` | パケット → フレーム |
| `Filter` | `Node`, `Process(ctx) error`, `InputPorts`, `OutputPorts` (どちらも `media.Frame`) | フレーム変換 |
| `Muxer` | `Node`, `AddStream(codecName, tb) (int, error)`, `SetMetadata(*metadata.Bundle) error`, `InputPorts()` (*Packet) | コンテナライター |
| `Demuxer` | `Node`, `Metadata() *metadata.Bundle`, `OutputPorts()` (*Packet) | コンテナリーダー |
| `Edge[T]` | `Push(ctx, T) error`, `Pull(ctx) (T, error)`, `Close()` | ノード間通信路 |

#### 型

| 型 | 説明 |
|---|------|
| `InPort[T]` | 入力ポート。`Connect(Edge[T])`, `Pull(ctx)`, `Accept(StreamInfo) error` |
| `OutPort[T]` | 出力ポート。`Connect(Edge[T])`, `Push(ctx, T) error`, `StreamInfo()` |
| `ConstraintFunc` | `func(media.StreamInfo) error` — 入力ポートの受け入れ条件チェック関数 |

#### コンストラクタ

```go
func NewInPort[T any](id string, c ConstraintFunc) *InPort[T]
func NewOutPort[T any](id string, info media.StreamInfo) *OutPort[T]
```

---

### パッケージ `pipeline`

```go
import "github.com/godexture/core/pipeline"
```

パイプラインの組み立てと実行を担当します。

#### 接続

```go
// 2つのノードのポートを Edge で接続する
// A の portA (OutputNode) → B の portB (InputNode)
func Link[T any, A node.OutputNode[T], B node.InputNode[T]](
    nodeA A, portA string,
    nodeB B, portB string,
) error

func LinkWithBufferSize[T any, A node.OutputNode[T], B node.InputNode[T]](
    nodeA A, portA string,
    nodeB B, portB string,
    bufferSize int,
) error

// 接続済み Node の ownership を持つ Pipeline を作る
func New(nodes ...node.Node) (*Pipeline, error)

func NewBuilder() *Builder
```

#### 型

| 型 | 説明 |
|---|------|
| `Pipeline` | Node の実行・キャンセル・逆順 Close を所有する single-use lifecycle |
| `Pipeline.Run(ctx) error` | 全 Node を並行実行し、全終了後に必ず Close |
| `Pipeline.Close() error` | Run 前は解放、Run 中は cancel と終了待ち、Run 後は idempotent |
| `Geometry` | Build 前の Node と Edge 定義を所有する |
| `Geometry.Close() error` | Build されず破棄された Node を逆順で Close |
| `Builder.Build(*Geometry) (*Pipeline, error)` | 検証・接続後に ownership を Pipeline へ移す |
| `ChanEdge[T]` | チャネルベースの Edge 実装 (バッファサイズ: 100) |

#### エラー

```go
var ErrInvalidPipeline = errors.New("invalid pipeline")
```

---

### パッケージ `registry`

```go
import "github.com/godexture/core/registry"
```

型パラメータ化されたジェネリックレジストリとマニフェスト型を提供します。

#### 主要な型

```go
// V は Manifest インターフェースを実装する必要がある
type Registry[V Manifest] struct { ... }

func NewRegistry[V Manifest]() *Registry[V]

// role と named config 型から PluginKey を生成して登録する
func (r *Registry[V]) Register(config Configuration, manifest V) error

func (r *Registry[V]) Key(config Configuration) (PluginKey, error)
func (r *Registry[V]) Get(key PluginKey) (V, error)

// PluginKey 順で決定的に走査する
func (r *Registry[V]) Enumerate() iter.Seq[V]
```

`PluginKey` は `(manifest role, config の named reflect.Type)` からのみ生成されます。フィールドは非公開で、plugin は ID を指定・上書きできません。pointer と value は同一視され、nil、typed nil、builtin、匿名型、重複 key は拒否されます。

#### Bundle (全レジストリをまとめた構造体)

```go
type Bundle struct {
    Demuxers *DemuxerRegistry
    Muxers   *MuxerRegistry
    Encoders *EncoderRegistry
    Decoders *DecoderRegistry
    Filters  *FilterRegistry
}
```

#### マニフェスト型

| 型 | フィールド | 説明 |
|---|-----------|------|
| `BaseManifest` | `Name string`, `Description string` | 全マニフェストの基底。`ID()` は registry-assigned `PluginKey` |
| `TransformManifest` | `BaseManifest`, port ごとの `InputRequirements`, `Resources` | 変換ノード共通 |
| `DemuxerManifest` | `BaseManifest`, `Probe manifest.Prober`, `Factory DemuxerFactory` | デマックスプラグイン |
| `MuxerManifest` | `BaseManifest`, `Factory MuxerFactory` | マックスプラグイン |
| `DecoderManifest` | `TransformManifest`, `Factory DecoderFactory` | デコーダプラグイン |
| `EncoderManifest` | `TransformManifest`, `Supports func(CodecID) bool`, `Factory EncoderFactory` | エンコーダプラグイン |
| `FilterManifest` | `TransformManifest`, `Factory FilterFactory` | フィルタプラグイン |

#### ファクトリ関数型

```go
type DemuxerFactory func(r io.Reader, config Configuration) (node.Demuxer, error)
type MuxerFactory   func(w io.Writer, config Configuration) (node.Muxer, error)

type TransformFactoryOptions struct {
    Config Configuration
}

type EncoderFactory func(media.StreamInfo, media.CodecID, TransformFactoryOptions) (node.Encoder, media.StreamInfo, error)
type DecoderFactory func(media.StreamInfo, TransformFactoryOptions) (node.Decoder, media.StreamInfo, error)
type FilterFactory  func(media.StreamInfo, TransformFactoryOptions) (node.Filter, media.StreamInfo, error)

type Preparer interface { Prepare(ResourceGrant) error }
```

`ResourceRequest{Parallelism: true}` は transform が並列予算を利用できることを宣言します。Factory は output `StreamInfo` を唯一の根拠として返し、高コストな初期化は optional な `Preparer` が negotiation 後の `ResourceGrant` を受けて行います。

#### インターフェース

```go
type Configuration interface{}

// マニフェスト共通
type Manifest interface { ID() PluginKey }

// オプション機能
type Defaulter interface { ApplyDefaults() }
type Validator interface { Validate() error }
```

---

### パッケージ `resolver`

```go
import "github.com/godexture/core/resolver"
```

レジストリからの自動選択ロジックを実装します。

| 型 | 主要メソッド | 説明 |
|---|------------|------|
| `DefaultDemuxerResolver` | `ResolveDemuxer(io.ReadSeeker, ...Option) (DemuxerManifest, error)` | ProbeScore が最大のデマックスを選択 |
| `DefaultDecoderResolver` | `ResolveDecoder(media.StreamInfo, ...Option) (DecoderManifest, error)` | InputRequirements による受理判定 + Priority で選択 |
| `DefaultEncoderResolver` | `ResolveEncoder(media.CodecID, ...Option) (EncoderManifest, error)` | Supports() + Priority で選択 |
| `DefaultMuxerResolver` | `ResolveMuxer(registry.Configuration) (MuxerManifest, error)` | 設定の型をキーに検索 |
| `DefaultFilterResolver` | `ResolveFilter(registry.Configuration) (FilterManifest, error)` | 設定の型をキーに検索 |
| `Bundle` | — | 上記 Resolver factory をまとめた構造体 |

#### 優先度オプション

```go
type Priority int

// プラグイン選択時の優先度を上書きする
func WithPriority(config registry.Configuration, priority Priority) Option
```

#### ポート選択ユーティリティ

```go
// 複数ポートから最良のオーディオポートを選択する
// 判定基準: IsDefault ボーナス + (チャネル数 × サンプルレート)
func ResolveDefaultAudioPort[T any](ports map[string]node.OutPort[T]) (*node.OutPort[T], error)
```

---

### パッケージ `routing`

```go
import "github.com/godexture/core/routing"
```

入力、明示 filter 列、target codec、resource budget から Geometry を negotiation します。

```go
type FilterSpec struct {
    Config registry.Configuration
    Inputs map[string]string // port -> named auxiliary input
}

type AuxInputSpec struct {
    Source io.ReadSeeker
    DemuxManifest registry.DemuxerManifest
    DemuxConfig registry.Configuration
    DecoderManifest registry.DecoderManifest
    DecodeConfig registry.Configuration
    Filters []FilterSpec
}

type ConversionSpec struct {
    Input, Output ...
    DecodeConfig registry.Configuration
    Filters      []FilterSpec
    AuxInputs    map[string]AuxInputSpec
    TargetCodec  media.CodecID
    EncodeConfig registry.Configuration
    MuxConfig    registry.Configuration
    Resources    registry.ResourceBudget
}

func (n *Negotiator) NegotiateConversion(ctx context.Context, spec ConversionSpec) (*pipeline.Geometry, error)
```

Decoder、全 Filter、Encoder の manifest と profile 遷移を先に解決し、その topology に含まれる並列対応 transform へ予算を公平に割り当ててから Node を構築します。失敗時は構築済み Node を Close します。

---

### パッケージ `domain/media`

```go
import "github.com/godexture/core/domain/media"
```

メディアデータを表すエンティティ・値オブジェクトを定義します。

#### メディアタイプ

```go
type MediaType string
const (
    MediaUnknown    MediaType = ""
    MediaVideo      MediaType = "video"
    MediaAudio      MediaType = "audio"
    MediaSubtitle   MediaType = "subtitle"
    MediaData       MediaType = "data"
    MediaAttachment MediaType = "attachment"
)
```

#### コーデック ID

```go
type CodecID string
const (
    CodecLPCM CodecID = "lpcm"  // Linear PCM
    CodecMPEG CodecID = "mpeg"
    CodecPCMU CodecID = "pcmu"  // G.711 μ-law
    CodecPCMA CodecID = "pcma"  // G.711 A-law
)
```

#### サンプルフォーマット

```go
type SampleFormat string
// Interleaved: SampleFormatU8, S16, S32, F32, F64
// Planar:      SampleFormatU8P, S16P, S32P, F32P, F64P

func (f SampleFormat) IsPlanar() bool
func (f SampleFormat) IsPacked() bool
func (f SampleFormat) Planar() SampleFormat  // U8 -> U8P
func (f SampleFormat) Packed() SampleFormat  // U8P -> U8
func (f SampleFormat) BytesPerSample() int   // 1, 2, 4, 8 のいずれか
```

#### チャネルレイアウト

```go
type ChannelLayout struct { ... }

// ビットマスクで位置を指定するネイティブレイアウト
func NewNativeLayout(mask ChannelPosition) ChannelLayout

// チャネル順序を陽示的に列挙するカスタムレイアウト
func NewCustomLayout(layout ...ChannelPosition) ChannelLayout

// チャネル数のみ既知の未指定レイアウト
func NewUnspecified(channels int) ChannelLayout

// アンビソニクスレイアウト
func NewAmbisonicLayout(order uint8) ChannelLayout

func (l ChannelLayout) ChannelCount() int
func (l ChannelLayout) Contains(c ChannelPosition) bool
func (l ChannelLayout) Enumerate() []ChannelPosition
func (l ChannelLayout) Index(c ChannelPosition) int
```

定義済みレイアウト定数 (一部抜粋):

```go
var (
    LayoutMono1       ChannelLayout  // 1ch: FrontCenter
    LayoutStereo2_0   ChannelLayout  // 2ch: FL+FR
    LayoutStereo3_0   ChannelLayout  // 3ch: FL+FR+FC
    LayoutSurround5_1 ChannelLayout  // 6ch (Front5_1 相当)
    LayoutSurround7_1 ChannelLayout  // 8ch
    // ... 他多数
)
```

#### Packet

```go
type Packet struct {
    ResourceBase
    MediaType   MediaType
    StreamIndex int
    PTS         Pts
    DTS         Dts
    Timebase    time.Rational
}

func (p *Packet) Data() []byte

// コンストラクタ (pool から取得)
func NewPacket(size int, opts ...PacketOption) *Packet

// オプション
func WithStreamIndex(idx int) PacketOption
func WithPts(pts Pts) PacketOption
func WithDts(dts Dts) PacketOption
```

#### Frame / AudioFrame

```go
type Frame interface {
    Retainer
    Pts() Pts
}

type AudioFrame struct {
    ResourceBase
    Format     SampleFormat
    Layout     ChannelLayout
    SampleRate int
    Samples    int
    // ...
}

func (f *AudioFrame) Pts() Pts
func (f *AudioFrame) Planes() [][]byte

// ジェネリックプレーン取得 (unsafe ポインタ経由)
func Plane[T SampleType](f *AudioFrame, planeIndex int) []T

// コンストラクタ (pool から取得)
func NewAudioFrame(
    format SampleFormat, layout ChannelLayout,
    sampleRate, samples int,
    opts ...AudioFrameOption,
) *AudioFrame

func WithAudioPts(pts Pts) AudioFrameOption
```

#### 参照カウント (ResourceBase)

```go
type Retainer interface {
    Retain()
    Release()
}

type ResourceBase struct { ... }
func (r *ResourceBase) Init(free func())
func (r *ResourceBase) Retain()   // refCount++ (1以下ならpanicする)
func (r *ResourceBase) Release()  // refCount-- → 0でfree()
```

#### StreamInfo / Profile

```go
type StreamInfo struct {
    Index       int
    Type        MediaType
    Duration    time.Duration
    Metadata    metadata.Bundle
    IsDefault   bool
    MediaAttributes
}

type Profile struct {
    Type MediaType
    MediaAttributes
}

func (p Profile) Signature() string  // キャッシュキーとして使用

type MediaAttributes struct {
    Codec CodecID
    Video VideoAttributes  // 未実装
    Audio AudioAttributes
}

type AudioAttributes struct {
    CodecID       CodecID
    SampleRate    int
    Format        SampleFormat
    ChannelLayout ChannelLayout
}
func (a AudioAttributes) ChannelCount() int
```

#### タイムスタンプ

```go
type Pts int64  // Presentation Timestamp
type Dts int64  // Decode Timestamp
```

#### エラー型

```go
type PiplineStage string
const (
    StageDemuxer PiplineStage = "demuxer"
    StageDecoder PiplineStage = "decoder"
    StageFilter  PiplineStage = "filter"
    StageEncoder PiplineStage = "encoder"
    StageMuxer   PiplineStage = "muxer"
)

type Error struct {
    PTS         Pts
    Stage       PiplineStage
    StreamIndex int
    Err         error
}
func (e *Error) Error() string
func (e *Error) Unwrap() error

type ErrorAction int
const (
    ActionStop   ErrorAction = iota
    ActionIgnore
)

type ErrorHandler interface {
    Handle(err *Error) ErrorAction
}
```

---

### パッケージ `domain/metadata`

```go
import "github.com/godexture/core/domain/metadata"
```

型安全なキー/値ストアです。`StreamInfo.Metadata` / `Muxer.SetMetadata` / `Demuxer.Metadata`
を通じてストリーム単位のタグ情報 (タイトル・アーティスト名など) を運びます。`AudioFrame` は
これを保持しません — フレーム単位のメタデータは現状未実装です。

```go
type Bundle struct { ... }

func NewBundle() *Bundle
func (b *Bundle) Clear()
func (b *Bundle) Merge(other *Bundle)
func (b *Bundle) Clone() *Bundle

// single キーは上書き、multiple キーは追記
func (b *Bundle) Set(value single)
func (b *Bundle) PushBack(value multiple)
func (b *Bundle) PushFront(value multiple)

// 型パラメータで取得 (未設定ならゼロ値)
func Get[T single](b *Bundle) T
func Enumerate[T multiple](b *Bundle) []T
```

#### 定義済みキー型 (抜粋)

```go
type KeyTitle string        // タイトル
type KeyArtist string        // アーティスト (multiple)
type KeyAlbum string          // アルバム名
type KeyTrackNumber int64     // トラック番号
type KeyThumbnail []Thumbnail // サムネイル画像 (multiple)
```

全キーは `core/domain/metadata/keys.go` を参照してください。

---

### パッケージ `domain/manifest`

```go
import "github.com/godexture/core/domain/manifest"
```

プラグイン能力宣言に使うインターフェースと ProbeScore を定義します。

```go
// プローブ結果のスコア定義
type ProbeScore int
const (
    ProbeMismatch           ProbeScore = 0
    ProbeExtensionOnly      ProbeScore = 10
    ProbeGenericContainer   ProbeScore = 25
    ProbeSharedMetadata     ProbeScore = 40
    ProbeSingleSync         ProbeScore = 60
    ProbeMultipleSync       ProbeScore = 80
    ProbeIncompleteSignature ProbeScore = 90
    ProbeExactSignature     ProbeScore = 100
)

type Prober func(r io.Reader) ProbeScore

// ノードタイプ
type NodeType string
const (
    RoleDemuxer NodeType = "demuxer"
    RoleMuxer   NodeType = "muxer"
    RoleDecoder NodeType = "decoder"
    RoleEncoder NodeType = "encoder"
    RoleFilter  NodeType = "filter"
    RoleUnknown NodeType = "unknown"
)

// 能力インターフェース
type Capability interface {
    Match(p media.StreamInfo) bool
    Diagnose(p media.StreamInfo) error
}

type AudioConstraint struct {
    Codecs        []media.CodecID
    SampleRates   IntConstraint
    Channels      IntConstraint
    Layouts     []media.ChannelLayout
    SampleFormats []SampleFormatConstraint
}
func (c *AudioConstraint) Match(p media.StreamInfo) bool
func (c *AudioConstraint) Diagnose(p media.StreamInfo) error
```

---

### パッケージ `domain/time`

```go
import "github.com/godexture/core/domain/time"
```

```go
// math/big.Rat の型エイリアス（タイムベース表現用）
type Rational big.Rat
```

---

### パッケージ `internal/xsync`

```go
// 内部パッケージのため直接インポート不可
```

```go
// RWMutex でガードされたジェネリック Map
type Map[K comparable, V any] struct { ... }

func NewMap[K comparable, V any]() *Map[K, V]
func (m *Map[K, V]) Load(key K) (V, bool)
func (m *Map[K, V]) Store(key K, value V)
func (m *Map[K, V]) LoadOrStore(key K, value V) (actual V, loaded bool)
func (m *Map[K, V]) All() iter.Seq2[K, V]
func (m *Map[K, V]) Clone() map[K]V

// Map の値を型アサートして iter.Seq[T] に変換するユーティリティ
func EnumerateMapValues[T any, K comparable, V any](m *Map[K, V]) iter.Seq[T]
```

---

## `github.com/godexture/sdk` — SDK ユーティリティ

**場所:** `packages/pkg/`

### パッケージ `engine`

```go
import "github.com/godexture/sdk/engine"
```

プラグイン実装側が実装すべき **Engine インターフェース** と、
それを `node.Node` に変換する **Adapter** を提供します。

#### Engine インターフェース

```go
// コンテナライター
type MuxerEngine interface {
    AddStream(info media.StreamInfo) (streamIndex int, err error)
    SetMetadata(meta metadata.Bundle) error
    WriteHeader() error
    WritePacket(streamIndex int, pkt *media.Packet) error
    WriteTrailer() error
}

// コンテナリーダー
type DemuxerEngine interface {
    Analyze() (streams []media.StreamInfo, globalMeta metadata.Bundle, err error)
    ReadPacket() (pkt *media.Packet, streamIndex int, err error)
}

// エンコーダ
type EncoderEngine interface {
    SendFrame(frame *media.Frame) error
    ReceivePacket() (*media.Packet, error)
    Flush() error
}

// デコーダ
type DecoderEngine interface {
    SendPacket(pkt *media.Packet) error
    ReceiveFrame() (*media.Frame, error)
    Flush() error
}

// フィルタ
type FilterEngine interface {
    SendFrame(frame *media.Frame) error
    ReceiveFrame() (*media.Frame, error)
    Flush() error
}
```

#### ラッパー関数 (Engine → Node 変換)

```go
func WrapDemuxer(engine DemuxerEngine) node.Demuxer
func WrapMuxer(engine MuxerEngine) node.Muxer
func WrapDecoder(engine DecoderEngine) node.Decoder
func WrapEncoder(engine EncoderEngine) node.Encoder
```

#### エラー定数

```go
var ErrEAGAIN = errors.New("resource temporarily unavailable (need more data)")
var ErrEOF    = errors.New("end of file or stream")
```

#### コーデックループ (内部ユーティリティ)

```go
// Encoder/Decoder/Filter の send→receive ループを共通実装
func runCodecLoop[I, O any](
    ctx context.Context,
    in node.Edge[I], out node.Edge[O],
    send func(I) error,
    receive func() (O, error),
    flush func() error,
) error
```

---

### パッケージ `pool`

```go
import "github.com/godexture/sdk/pool"
```

サイズ別 (2^6 〜 2^20 バイト) の `sync.Pool` を管理します。

```go
func Get(size int) *[]byte   // プールからバイトスライスを取得
func Put(b *[]byte)          // バイトスライスをプールに返却
```

サイズが 2^20 を超える場合は通常の `make` にフォールバックします。

---

### パッケージ `hash`

```go
import "github.com/godexture/sdk/hash"
```

```go
// FNV-1a 64bit ハッシュ (最上位ビットを強制的に1にセット)
func FNV(data []byte) uint64
```

> **注:** 最上位ビットの強制セットは、カスタムチャネルレイアウトの ID が  
> ネイティブレイアウトのビットマスクと衝突しないようにするための処置です。

---

### パッケージ `bits`

```go
import "github.com/godexture/sdk/bits"
```

現在は空の stub です。将来的なビット操作ユーティリティ用に予約されています。
