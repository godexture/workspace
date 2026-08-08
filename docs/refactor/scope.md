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

最初の縦断経路は finite/static な WAVE + PCM と direct/local byte Access で作る（M6）。次に MP4 (ISO BMFF) を加え、複数 stream、per-track timescale、sample entry binding、moov/mdat の capability alternative、未知 box の raw preservation を実規格で通す（M7）。MP4 の音声は PCM を bind し、video/subtitle track は stream copy のみ扱う。copy は decode しないため `media/video`/`media/subtitle` の frame 型を必要とせず、両 package は実 consumer が現れるまで作らない。

公式実装がこの 5 family（WAVE、PCM、MP4、MP3、FLAC）に限られることは制限ではなく境界である。目標は規格の網羅ではなく、第三者が同じ contract で追加できることであり、MP4 はその主張が fixture ではなく実装で成立することを示すために置く。

ただし public contract は次を閉じない。

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

担当は分ける。graph operation としての seek plan は M7 が持ち、その最初の実装 consumer は同じ M7 の MP4 index（`stbl`/`stss`）である。format ごとの index/preroll 実装は M6 と M8 の family 移行に乗せる。現行 MP3/FLAC には seek 実装があるため、`_legacy/` を削除する前に、新経路が同等の能力を持つことを M8 完了時点で確認する。

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

M3-1 の walking skeleton が data path の consumer として使う contract は、`media/schema` の marker 由来 typed schema/traits と erased descriptor、`media/timing` の optional PTS、`flow` の静的 port shape・`Input` ownership・`Reader`/`Writer`・`Processor`/`Emitter`・operator lifecycle、`plugin.Component` の `Open`、`media/buffer` の ownership handle/view、`media/packet` の Chunk/Packet 境界、`media/audio` の planar frame である。test fixture の駆動 loop は `bytes → chunk → packet → frame → packet → chunk → bytes` を接続し、demux/parser/decoder/encoder/muxer 間で payload ownership、sequence、timestamp を検査する。M5 では typed product の生成場所を `schema.Descriptor` から component execution binding と private runtime へ移し、この contract を item ごとの型消去なしで置換した。

同じ skeleton の host 構築では、`plugin.Set` の一般化された declaration、`media/codec` の codec/parser identity と Binding、`host.New` の declaration conflict/target 検証も consumer として使う。`media/format.Format` は marker 由来 identity を持つ宣言を作り `Valid()` を確認するだけで、tag は Binding key/data として使う。`media/codec.Codec`/`Parser` 自身は I/O や parse/decode を実行せず、data path の trivial parser/decoder/encoder は `plugin.Component` の `flow.Processor` fixture として実装する。第三者 schema の bounded queue/fan-out consumer は M5 の `internal/run/{queue,drive}` test が担当する。

M3-1 で data-path consumer を持たず宣言・検証に留める contract は、`media/property` と `media/stream` の descriptor/property 経路、`access` の Source/Sink capability と Own/Borrow/Factory、Format の実 I/O、実 Format/Codec の decode/encode、endpoint、dynamic Shape、Compile/Suggest、planner/runtime の拡張である。これらは M3-3/M4/M6 以降へ残し、未使用の詳細を先に凍結しない。

### M3-2 の contract 分類

M3-2 の data-path consumer は、`media/metadata` の immutable `Document`/`Builder`、ordered entry、`Origin`、`RawBlock`、immutable `Blob`、metadata Binding と、`media/tag` の共通 key、部分日付、Blob-backed artwork である。foundation の trivial metadata encoding が carrier payload を Document へ parse/marshal し、未知 record の raw bytes、同一 key の複数値と順序を検査する。`host.New` は metadata Binding の target 実在と carrier conflict を codec Binding と同じ declaration 経路で検証する。

時刻に沿って変化する metadata は static `Document` の entry ではなく、`schema.Define` で宣言した typed event の port へ接続する。`stream.Descriptor.Metadata` は static document の immutable attachment として保持する。

M3-2 で宣言・検証に留める contract は `metadata.Mapping` である。source/target key、lossiness、priority、全順序の tie-break を型で表し、その規則を unit test で検査するが、mapping を適用する consumer は planner が入る M4 まで存在しない。詳細を今凍結せず、loss report の surface 表示は M7、実 ID3/Vorbis/RIFF encoding への移行は M8 に残す。`media/side` は M3-3 が担当し、この時点では型が存在しない。

### M3-3 の contract 分類

M3-3 の data-path consumer は、`media/side` の immutable side data と、`media/stream.Event` による stream add/remove/property change である。第三者が `metadata.Key` と同じ clone 規則で packet/frame の side data を追加でき、空の item は追加 allocation や間接参照を負わない。`side.Data` は key 宣言と clone 規則だけを `media/metadata` と共有し、`Document` の scope、carrier origin、raw block は持たない。それらは stream や asset を記述する control plane の概念で、単一 item に付く値には意味がないためである。Event の follow/ignore/fail は `Undecided` を初期値とし、live topology の既定 policy を暗黙に決めない。

`stream.Descriptor` は `stream.ID` を持つ。`schema.ID` は unit の種類を表すため、同じ schema を運ぶ 2 本の audio stream を区別できず、removal と property change が対象を指せない。topology を inspect した側が採番し、core は値を解釈しない。M4 の descriptor fingerprint と M7 の multi-stream mapping はこの identity の上に乗る。

宣言・検証に留める contract は、`access.Reference` の canonical/redacted 表現、`access.Provider` の scheme declaration、transaction class、spool specification、immutable bounded probe view/range request、snapshot identity、`endpoint` の topology/realtime trait、Device descriptor/reference、明示 opt-in の `DeviceQuery` である。Provider の scheme conflict は既存の `plugin.Declaration`/catalog に載せ、独自 registry は作らない。Host/package import や型の構築は device scan、permission prompt、network access を起こさない。

M4 が Provider/Endpoint declaration の planner binding、acquire/probe/inspect と capability diagnostic を担当し、M5 が transaction execution/rollback と spool insertion、M6 が file/HTTP 等の具体 Provider/Format 接続、M9 が device/session Endpoint 実装を担当する。clock/timestamp origin、latency/buffer、drop/reconnect/discontinuity、exclusive/shared、hotplug、multi-output `AllOrNothing` は consumer が現れる milestone まで型を増やさず、M3 では設計文書だけを正本とする。

### M3 時点で型を持たない capability matrix の行

M3 完了条件が求める「型が存在しない行の担当明示」である。他の 13 行は上の分類節に挙げた型で満たしている。

| 行 | 状態 | 担当 |
|---|---|---|
| stream/program/chapter mapping | `job` package が無い。mapping と selector は Job の正規化に属する | 型は M4-2 の `job`。実 consumer は M7 の MP4 であり、WAVE/MP3/FLAC はいずれも単一 stream のため mapping を実規格で検証できない |
| hardware/native implementation | variant 型が無い | M4 が型と hard filter（accuracy/repeatability/platform で候補を絞る側）を持ち、M8 が実 variant を公式 plugin へ入れる |
| analysis/report branch | 専用の型は作らない | 出力 port を持たない通常 component として既に表現でき、core の closed role を増やさない。read-only branch が診断を出すための `Diagnostics` service は M5 が host service として用意する |
| Go library、CLI、WASM | `host` だけが存在し、`job`・`Plan`・`Result`・surface DTO が無い | `job`/`Plan` は M4、`Result` と実行 event は M5、library と CLI の最短経路は M6、全 surface の完成は M9 |

いずれも core の closed enum や switch を増やさずに追加できることが条件であり、担当 milestone が書けない行は残さない。

M3 はこの文書の capability matrix が要求する拡張点のうち、contract として表現できる範囲を満たす milestone である。各行の実装は M6 以降が担当する。個別の条件は [media](media.md#m3-完了条件) と [access](access.md#m3-完了条件) を参照し、この節は網羅性だけを見る。

- capability matrix の各行に対応する拡張点の型が foundation に存在し、追加時に core の closed enum や switch を編集する必要がない。M3 時点で型が存在しない行があれば、どの milestone が担当するかを明示する。
- object Access、session Endpoint、Device Endpoint、Format の責務が型の上で混ざっていない。
- 初期実装が対応しない capability を、false capability や隠れた type assertion ではなく、宣言された requirement として表現できる。
- `FiniteStatic`、`LiveStatic`、`LiveDynamic` と stream event を表現でき、[D3](decisions.md) の未決事項を暗黙の既定で埋めていない。
- seek を Demuxer の optional method として表現していない。graph operation として扱う前提が型の上で崩れていない。
- object Access、typed session/device Endpoint、packet/frame side data、live stream event、Format/Codec/Metadata の各拡張点が、第三者の型・declaration として追加できる。
- M3 時点で consumer を持たない詳細を最小型へ閉じ込め、担当 milestone と実行 consumer を checkpoint に残している。

## 文書全体の完了条件

この節は拡張点全体の最終状態を示す gate であり、個別 milestone の完了判定には各 milestone 固有の条件を用いる。M3 の判定には上記「M3 完了条件」だけを使う。

- capability matrix の各行を第三者 definition として追加でき、core/surface の switch を変更しない。
- object Access、session Endpoint、Device Endpoint、Format の責務が混ざらない。
- 初期未対応 capability が runtime panic ではなく Compile diagnostic になる。
- video/subtitle/custom schema と live/device fixture が、同じ Host/planner/runtime へ参加できる。
- decode 実装を持たない stream（MP4 の video/subtitle track など）が、raw carrier と structured diagnostic を通じて情報を失わずに copy される。
