# FFmpeg 代替 capability

この文書は「FFmpeg を代理できる拡張性」の範囲を一覧化する。各 contract の詳細は個別資料へ委ね、ここでは重複して API や lifecycle を定義しない。

FFmpeg 代替とは、公式実装ですべての規格・protocol・device を提供することではない。必要な責務が core の closed enum/switch ではなく、第三者の schema、component、Binding、Provider、Endpoint として同じ planner/runtime に参加できることを意味する。

## capability matrix

| 能力 | 拡張点 | foundation が提供する共通機構 | 詳細 |
|---|---|---|---|
| file、memory、HTTP/S3 object | Access Provider | reference、byte capability、snapshot、cancel、transaction | [access](access.md) |
| pipe、既存 Reader/Writer | Access adaptor | Own/Borrow、sequential capability、prefix replay | [access](access.md) |
| RTSP/RTP/HLS/DASH/live broadcast | typed Endpoint | session、clock、reconnect、dynamic topology | [access](access.md) |
| camera、microphone、speaker、display | Device Endpoint | typed port、clock、backpressure、hotplug、lifecycle | [access](access.md) |
| container/elementary stream | Format | bounded Probe、Inspect、Carrier、read/write/finalize | [media](media.md)、[access](access.md) |
| byte stream の packet 化 | Parser | packet boundary、resync、parameter、timestamp | [media](media.md) |
| packet 表現の変更 | Packet Processor | typed `Packet -> Packet`、framing/extradata transformation | [media](media.md) |
| encode/decode | Codec | typed port、delay、Flush、final parameters | [media](media.md)、[plugins](plugins.md) |
| audio/video/subtitle/data processing | Processor/Operator | open schema、typed port、fan-in/out、lifecycle | [plugins](plugins.md)、[runtime](runtime.md) |
| metadata byte 規格 | Metadata Encoding | open Document、raw preservation、parse/marshal | [media](media.md) |
| container/tag と codec/metadata の接続 | Binding | conflict detection、explicit override、Plan provenance | [media](media.md) |
| metadata の意味変換 | Mapping | source/target key、lossiness、priority | [media](media.md) |
| stream/program/chapter mapping | Job/Planner | topology、selector、mapping、default preservation | [surfaces](surfaces.md)、[planner](planner.md) |
| timed metadata/subtitle | typed event schema | time base、ordering、queue | [media](media.md)、[runtime](runtime.md) |
| hardware/native implementation | component Variant | selection、resource、provenance、policy | [performance](performance.md) |
| analysis/report branch | Analyzer component | read-only branch、diagnostic、Plan integration | [plugins](plugins.md)、[planner](planner.md) |
| Go library、CLI、WASM | Host surface | normalized Job、Plan、events、Result | [surfaces](surfaces.md)、[web](web.md) |

## 境界

object-like I/O、session/device、media format を同じ abstraction に潰さない。

```text
object input:
Reference -> Access Provider -> byte Source -> Format -> Packet/Carrier

live/device input:
Endpoint -> typed Packet/Frame/Event

media processing:
typed units -> Parser/Codec/Processor -> typed units
```

- Access は byte の所在、capability、snapshot、transaction を扱い、WAVE/MP4 等の意味を知らない。
- Format は container/elementary stream の構造を扱い、file/HTTP/S3 等の所在を知らない。
- Endpoint は clock、session、dynamic topology を扱い、seekable byte object を装わない。
- Codec/Processor は typed unit を扱い、container や Access Provider を直接 import しない。
- Binding が独立した責務を composition 時に接続する。

foundation の Access contract は permission system ではない。file/network/device の権限は組み込み application、渡された handle/Provider、OS/container/browser が所有する。[C16](decisions.md) に従い、path/scheme/CIDR 等の汎用権限 DSL や production HTTP service は提供しない。

## 初期実装と将来拡張

最初の縦断経路は finite/static な WAVE + PCM と direct/local byte Access で作る。ただし public contract は次を閉じない。

- non-seekable sequential source
- 複数 input/output と複数 stream
- `FiniteStatic`、`LiveStatic`、`LiveDynamic`
- stream add/remove/property change event
- session/device clock と discontinuity
- random access requirement と explicit spool
- rollback 不能 sink と partial commit
- hardware/native component variant

初期実装が未対応の capability は、黙った type assertion や false capability ではなく、Compile diagnostic として表す。

## seek と live topology

seek は Demuxer 一つの optional method ではない。source capability、format index、codec preroll、filter state、timeline、sink/device clock を含む graph operation である。

担当は分ける。graph operation としての seek plan は M7、format ごとの index/preroll 実装は M6 と M8 の family 移行に乗せる。現行 MP3/FLAC には seek 実装があるため、M11 で旧経路を削除する前に、新経路が同等の能力を持つことを M8 完了時点で確認する。

実装する時は次を一つの seek plan に記録する。

- target timeline/domain
- source index/keyframe point
- decoder preroll
- processor reset/warm-up
- metadata/event replay
- output/device clock reset

live topology は static descriptor の mutationではなく `stream.Event` とする。新しい stream を follow/ignore/fail のどれにするかは [D3](decisions.md) として延期されているため、初期実装で暗黙の既定を決めない。

## 延期事項

初期 implementation を妨げない未決事項は [decision ledger](decisions.md#deferred-without-blocking-the-first-implementation) を正本とする。

- dynamic install の方式
- remote plugin wire protocol
- live dynamic topology の既定 policy
- 公式 hardware accelerator/device adaptor の提供範囲

これらを実装する際も、既存の schema/component/Provider/Endpoint/variant contract へ接続し、core の closed role を追加しない。

## M3 完了条件

### M3-1 の contract 分類

M3-1 の walking skeleton が data path の consumer として使う contract は、`media/schema` の marker 由来 typed schema/traits と erased descriptor factory、`media/timing` の optional PTS、`flow` の静的 port shape・`Input` ownership・`Reader`/`Writer`・`Processor`/`Emitter`・operator lifecycle、`plugin.Component` の `Open`、`media/buffer` の ownership handle/view、`media/packet` の Chunk/Packet 境界、`media/audio` の planar frame である。test fixture の駆動 loop は `bytes → chunk → packet → frame → packet → chunk → bytes` を接続し、demux/parser/decoder/encoder/muxer 間で payload ownership、sequence、timestamp を検査する。

同じ skeleton の host 構築では、`plugin.Set` の一般化された declaration、`media/codec` の codec/parser identity と Binding、`host.New` の declaration conflict/target 検証も consumer として使う。`media/format.Format` は marker 由来 identity を持つ宣言を作り `Valid()` を確認するだけで、tag は Binding key/data として使う。`media/codec.Codec`/`Parser` 自身は I/O や parse/decode を実行せず、data path の trivial parser/decoder/encoder は `plugin.Component` の `flow.Processor` fixture として実装する。第三者 schema では erased descriptor から typed queue/fan-out を Open 時に一度だけ検証し、実際に item を通す。

M3-1 で data-path consumer を持たず宣言・検証に留める contract は、`media/property` と `media/stream` の descriptor/property 経路、`access` の Source/Sink capability と Own/Borrow/Factory、Format の実 I/O、実 Format/Codec の decode/encode、metadata/side data、endpoint、dynamic Shape、Compile/Suggest、planner/runtime の拡張である。これらは M3-2/M3-3/M4/M6 以降へ残し、未使用の詳細を先に凍結しない。

M3 はこの文書の capability matrix が要求する拡張点のうち、contract として表現できる範囲を満たす milestone である。各行の実装は M6 以降が担当する。個別の条件は [media](media.md#m3-完了条件) と [access](access.md#m3-完了条件) を参照し、この節は網羅性だけを見る。

- capability matrix の各行に対応する拡張点の型が foundation に存在し、追加時に core の closed enum や switch を編集する必要がない。M3 時点で型が存在しない行があれば、どの milestone が担当するかを明示する。
- object Access、session Endpoint、Device Endpoint、Format の責務が型の上で混ざっていない。
- 初期実装が対応しない capability を、false capability や隠れた type assertion ではなく、宣言された requirement として表現できる。
- `FiniteStatic`、`LiveStatic`、`LiveDynamic` と stream event を表現でき、[D3](decisions.md) の未決事項を暗黙の既定で埋めていない。
- seek を Demuxer の optional method として表現していない。graph operation として扱う前提が型の上で崩れていない。

## 文書全体の完了条件

この節は拡張点全体の最終状態を示す gate であり、個別 milestone の完了判定には各 milestone 固有の条件を用いる。M3 の判定には上記「M3 完了条件」だけを使う。

- capability matrix の各行を第三者 definition として追加でき、core/surface の switch を変更しない。
- object Access、session Endpoint、Device Endpoint、Format の責務が混ざらない。
- 初期未対応 capability が runtime panic ではなく Compile diagnostic になる。
- video/subtitle/custom schema と live/device fixture が、同じ Host/planner/runtime へ参加できる。
