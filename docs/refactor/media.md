# media と metadata

## 二つの層

media model は、plan を作る control plane と、packet/frame を運ぶ data plane を分ける。

Control plane:

- stream topology
- format/codec/schema descriptor
- time base
- metadata document
- property と capability
- component requirement

Data plane:

- encoded packet/chunk
- decoded audio/video/subtitle/custom unit
- timed event
- buffer ownership
- PTS/DTS/duration

control plane では open property set や一部 type erasure を許容する。data plane では具体型と compile 済み edge を使う。

## open typed schema

現在の `LinkAny` は `Packet` と `Frame` に分岐し、`MediaAttributes.Video` は未実装である。この形で stream kind を増やすたびに core を編集すると、第三者拡張という目標を満たせない。

schema を値型として登録し、port は schema の具体型で結ぶ。

```go
type framesID struct{}

var Frames = schema.Define[framesID, *audio.Frame[float32]](
    schema.Fork(audio.Fork),
    schema.Drop(audio.Drop),
    schema.Size(audio.Size),
    schema.Time(audio.Time),
)

in  := flow.In(Frames)
out := flow.Out(Frames)
```

第三者も同じ仕組みで独自 unit を宣言できる。

```go
type Cue struct {
    Start timing.Timestamp
    End   timing.Timestamp
    Text  string
}

type cuesID struct{}

var Cues = schema.Define[cuesID, Cue](schema.Time(cueTime))
```

`schema.Type[T]` は identity と runtime traits を持つ。traits は fan-out、drop、queue accounting、timestamp scheduling が必要な時だけ呼ぶ。観測や byte limit が無効な linear path では `Size` を毎回計算しない。

万能な `Frame` interface は廃止し、`audio.Frame`、`video.Frame`、`subtitle.Cue` を別 schema にする。generic filter は、受け入れる schema constraint を明示するか、schema ごとの implementation を登録する。

### 現行 `Frame` の監査結果

現行 `core/domain/media.Frame` が共通化する操作は `Retain`、`Release`、`Pts` だけである。意味的な処理をする production code は `*media.AudioFrame` へ type assertion しており、audio filter、PCM/FLAC encoder、playback、観測処理等で型不一致を runtime error にしている。`core/pipeline.LinkAny` も `*Packet` と `Frame` の二種類を hard-code する。

したがって現在の `Frame` は複数 media の有用な抽象ではなく、「audio 型を interface の後ろへ隠している」状態である。これを video/subtitle に拡張すると、接続自体は compile できても各 filter が runtime に拒否する API になる。

### 全データ型に共通する処理

次の処理は schema を問わず必要になる。

- tee/split と fan-out
- discard/null sink
- queue、buffer、backpressure
- passthrough
- progress、metrics、trace
- cancel 時の drain/drop

これらは payload の意味を変える filter ではなく、host の搬送処理である。schema の `Fork`、`Drop`、`Size`、`Timing` trait を使って型別 operator を構築する。

timestamp shift、trim、segment、rate control は全型へ無条件に適用しない。audio の PTS は sample、video は frame duration/reordering/keyframe、subtitle は開始・終了区間を持ち、同じ操作名でも正しい semantics が異なる。component が対応 schema と timing rule を明示する。

### Go で control plane だけを型消去する方法

planner は異なる `schema.Type[T]` を同じ catalog/graph に格納する必要があるが、data item を `any` で運ぶ必要はない。schema/component 登録時に、`T` が確定した generic factory closure を descriptor へ格納する。

概念例:

```go
type Type[T any] struct {
    descriptor Descriptor
}

func Define[ID, T any](traits Traits[T]) Type[T] {
    return Type[T]{
        descriptor: Descriptor{
            identity: reflect.TypeFor[ID](),
            newPipe: func(limit Limit) erasedPipe {
                return erasePipe(newPipe[T](limit, traits))
            },
            newTee: func(outputs int) erasedOperator {
                return eraseOperator(newTee[T](outputs, traits.Fork, traits.Drop))
            },
        },
    }
}
```

component definition も、Open 時に erased endpoint を一度だけ `pipe[T]` へ検証して typed Reader/Writer を保持する factory closure を登録する。type assertion は Program の Open 時だけであり、item ごとには行わない。

```text
catalog / planner: schema ID と erased factory
Open:              endpoint 型を一度検証
Run:               Reader[T] -> Processor[I,O] -> Writer[O]
```

この方式なら、第三者が `schema.Define[unitID, acme.Unit]` を呼んだ build に `pipe[acme.Unit]`、tee、drop 等の型付き実装が含まれる。core が `acme.Unit` を事前に知らなくてもよく、hot path で reflection、string lookup、serialize、`any` map を使わない。専用 marker により、payload 型の refactor と schema identity も分離できる。

## stream descriptor

stream kind の closed enum と全属性を詰め込んだ `MediaAttributes` を廃止する。

```go
type Descriptor struct {
    Schema     schema.ID
    TimeBase   timing.Base
    Properties property.Set
    Metadata   metadata.Document
}
```

`property.Set` は immutable な control-plane 値であり、audio sample rate、video pixel format、subtitle language、codec parameters 等を typed key で保持する。未知 property をコピーできるが、component は requirement として宣言した property だけを解釈する。

hot path の unit がこの map を毎回持たない。stream-local な不変情報は compiled edge/port に保持する。

## time

- stream ごとに一つの integer time base を持つ。
- PTS、DTS、duration は型を区別した integer とする。
- rescale は checked integer arithmetic を使い、overflow と rounding policy を明示する。
- control plane で必要なら rational を使えるが、item ごとに `big.Rat` を使わない。
- timestamp が不明な状態を `0` と混同せず optional/flag で表す。
- decoder reorder、encoder delay、filter latency を descriptor/plan に反映する。

## Packet、Chunk、Parser

format が読む単位と decoder が必要とする packet は常に同じではない。WAVE 内の MP3、H.264 Annex B/AVCC、AAC ADTS、FLAC の resync 等に対応するため、Parser/packetizer を第一級 component とする。

```text
Format Reader -> container Chunk -> Parser -> codec Packet -> Decoder
Decoder -> Frame -> Encoder -> codec Packet -> optional Bitstream Filter -> Format Writer
```

すでに packetized された container では Parser は identity または不要である。bitstream filter は `Packet -> Packet` component であり、decoder/encoder とは独立する。

未知の codec tag/sample entry は raw carrier として保持し、対応 Binding がないことを structured diagnostic にする。

## Format、Codec、Carrier、Metadata

混同しやすい概念を次の六つに分ける。

### Format

container または elementary stream の framing、stream table、interleave、seek、header/footer を扱う。例: RIFF/WAVE、MP4、Matroska、native FLAC stream。

Format は「この byte 列が何であるか」を読み書きするが、その byte 列が file、HTTP、S3、memory のどこから来たかは扱わない。Source/Sink capability と transaction は [Access](access.md) が所有する。Format は特定 decoder、metadata parser、Access Provider を直接 import しない。

### Codec

media の圧縮/表現方式である。例: PCM、MP3、FLAC、H.264。`Codec` という語は media codec に限定し、ID3/Vorbis Comment には使わない。

### Carrier

format または codec bitstream 内にある、外部 payload を置く物理的な slot/envelope である。

例:

- RIFF の `LIST/INFO` chunk
- WAVE の `id3 ` chunk
- MP3 stream の先頭/末尾 tag area
- FLAC metadata block
- MP4 atom/sample entry
- 将来の video SEI message

Carrier は payload の意味を決めない。format-owned carrier だけでなく codec/bitstream-owned carrier もあるため、`ContainerID` 一つを owner として埋め込まない。

### Metadata Encoding

carrier payload の byte 表現を semantic document へ parse/marshal する規格である。例: ID3v2.4、Vorbis Comment、RIFF INFO。

### Metadata Document

format に依存しない semantic entry の列である。title、artist、picture のような共通概念と、第三者固有 key、origin、未解釈 raw block を保持する。

### Binding

carrier と metadata encoding、または container codec tag と media codec/parser の対応を結ぶ composition-time declaration である。Format/Codec/Encoding 本体のどれにも対応表を埋め込まない。

## 関係の全体像

関係は継承ではなく、二つの独立した経路と Binding で表す。

```text
encoded media:

Source
  -> Format
       -> stream carrier/tag
       -> Codec Binding
       -> optional Parser
       -> Packet
       -> Codec
       -> decoded schema

metadata:

Format or codec bitstream
  -> Carrier
       -> Metadata Binding
       -> Metadata Encoding
       -> Document
       -> shared keys / third-party keys / raw blocks
```

WAVE 内の MP3 と ID3 を例にすると次の通りである。

```text
RIFF/WAVE Format
  ├─ fmt tag 0x0055
  │    └─ codec Binding -> MP3 Parser -> MP3 Packet -> MP3 Decoder
  └─ "id3 " chunk Carrier
       └─ metadata Binding -> ID3v2 Encoding -> Document
```

Format は WAVE chunk と tag の物理表現を知るが、MP3 decoder や ID3 parser を import しない。MP3 Codec は音声を decode するが、RIFF chunk を知らない。ID3 Encoding は tag payload を解釈するが、それが WAVE chunk、MP3 stream 先頭、別の carrier のどこに置かれたかを知らない。standard composition が二種類の Binding を足して初めて一つの経路になる。

native FLAC のように一つの規格名が format と codec の両方を含む場合も、component と責務は分ける。同じ plugin package に凝集させることと、一つの interface に融合することは別である。

## codec binding

format が認識する tag/sample entry と、codec/parser/parameter schema の関係を standard composition で登録する。

```go
codec.Bind(wave.FormatTag(0x0055), mp3.Codec, mp3.Parser)
codec.Bind(wave.FormatTag(0x0001), pcm.Codec, pcm.Parameters)
codec.Bind(mp4.SampleEntry("avc1"), h264.Codec, h264.AVCC)
```

このため、第三者 codec を WAVE に追加する際に WAVE plugin も core も編集しなくてよい。第三者は新しい Binding を `Set` に足すだけである。

binding key が重複して異なる codec を指す場合、host build を失敗させる。利用者が意図的に置換する場合だけ explicit override を使う。

## metadata document

単純な `map[reflect.Type]any` では、同一 key の複数値、順序、元の frame/block、未知 payload、部分編集を表現しにくい。document は ordered entry と provenance を持つ。

```go
type Document struct {
    Entries []Entry
    Blocks  []RawBlock
}

type Entry struct {
    Key    KeyID
    Value  Value
    Origin Origin
}

type Origin struct {
    Encoding EncodingID
    Carrier  CarrierID
    Block    BlockID
    Native   string
}
```

実際の API では slice の直接 mutation を許さず、persistent/immutable value または builder を使う。

`RawBlock` は未解釈 block、未知 frame、vendor field、元 byte 列を保持する。同じ format/carrier へ変更なしで出力する場合は raw copy を優先し、metadata を編集した時だけ影響 block を再 encode する。

artwork の大きな byte slice は entry clone のたびに複製せず、immutable `Blob`/reference-counted buffer を参照する。

## open key と共通 vocabulary

core contract は `KeyID`、`Entry`、`Document` の仕組みだけを定義し、Title 等の concrete key enum を持たない。

公式の任意 package `tag` が共有 vocabulary を提供する。

```go
var Title   = metadata.DefineKey[titleID, string]()
var Artist  = metadata.DefineKey[artistID, string]()
var Date    = metadata.DefineKey[dateID, tag.Date]()
var Picture = metadata.DefineKey[pictureID, tag.Picture]()
```

第三者も core を変更せず固有 key を定義できる。

```go
var ReplayGain = metadata.DefineKey[replayGainID, Gain]()
```

現行 `Bundle` の `single`/`multiple` が unexported method を要求する方式は、外部 package に key family を追加させられないため置換する。

## metadata binding と変換

carrier と encoding を binding する。

```go
metadata.Bind(wave.ID3Chunk, id3.V24)
metadata.Bind(flac.VorbisBlock, vorbiscomment.Encoding)
metadata.Bind(mp3.LeadingTag, id3.Auto)
```

parse の流れ:

```text
Format/bitstream
  -> Carrier(raw payload)
  -> selected Metadata Encoding
  -> Document entries + origin + unknown raw blocks
```

marshal の流れ:

```text
Document
  -> target Carrier capabilities
  -> selected Metadata Encoding
  -> payload
  -> Format/bitstream writer
```

異なる規格同士の一般的な変換は、規格間の全組み合わせを直接書くのではなく、共通 vocabulary を hub にする。

```text
ID3 TIT2 -> tag.Title -> Vorbis TITLE
RIFF IART -> tag.Artist -> ID3 TPE1
```

これで encoding が N 個に増えても N² 個の converter は不要になる。

ただし任意の第三者 key が同じ意味かどうかは自動判定できない。必要な場合は optional な Mapping component を追加する。

```go
metadata.Map(acme.Mood, tag.Genre, mapMood)
```

Mapping は source key、target key、lossiness、priority を宣言する。曖昧な変換を host が推測しない。

## metadata scope

静的 document には scope を持たせる。

- asset/container
- program
- stream
- chapter

時刻に沿って変化する metadata は static `Document` に詰めず、typed timed-event stream とする。これにより字幕、章 event、放送 metadata、video side data を同じ data-plane 原則で扱える。

## loss と strictness

出力 carrier/encoding が entry を表現できない場合は三段階で処理する。

1. raw block を同じ carrier へ保持できるなら lossless copy
2. 共通 key または明示 Mapping で表現できるなら変換
3. どちらも不可能なら loss report

default は best effort + structured warning とし、黙って捨てない。`StrictMetadata` では 3 を job failure にする。

diagnostic は key だけでなく origin、target carrier、理由、選ばれなかった mapping を含める。

## default output

無指定出力では入力 format/codec/stream と metadata raw block の保持を優先する。media model が保証するのは、copy可否、Binding、raw preservation、loss/effect を planner が判断できる descriptor を提供することである。

具体的な候補順、自動挿入、明示 output との競合は [planner](planner.md#default-transcode-の探索)、利用者向け既定動作は [surfaces](surfaces.md#default-behavior) を正本とする。
