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

M3-1 で data-path consumer を持たず宣言・検証に留めた contract は、`media/property` と `media/stream` の descriptor/property 経路、`access` の Source/Sink capability と Own/Borrow、Format の実 I/O、実 Format/Codec の decode/encode、endpoint、dynamic Shape、Compile/Suggest、planner/runtime の拡張である。これらは M3-3/M4/M6 以降へ残し、未使用の詳細を先に凍結しない。session factory は M6 review で consumer が無いことを確認して削除した。

### M3-2 の contract 分類

M3-2 の data-path consumer は、`media/metadata` の immutable `Document`/`Builder`、ordered entry、`Origin`、`RawBlock`、immutable `Blob`、metadata Binding と、`media/tag` の共通 key、部分日付、Blob-backed artwork である。foundation の trivial metadata encoding が carrier payload を Document へ parse/marshal し、未知 record の raw bytes、同一 key の複数値と順序を検査する。`host.New` は metadata Binding の target 実在と carrier conflict を codec Binding と同じ declaration 経路で検証する。

時刻に沿って変化する metadata は static `Document` の entry ではなく、`schema.Define` で宣言した typed event の port へ接続する。`stream.Descriptor.Metadata` は static document の immutable attachment として保持する。

M3-2 で宣言・検証に留める contract は `metadata.Mapping` である。source/target key、lossiness、priority、全順序の tie-break を型で表し、その規則を unit test で検査するが、mapping を適用する consumer は planner が入る M4 まで存在しない。詳細を今凍結せず、loss report の surface 表示は M7、実 ID3/Vorbis/RIFF encoding への移行は M8 に残す。`media/side` は M3-3 が担当し、この時点では型が存在しない。

### M3-3 の contract 分類

M3-3 の data-path consumer は、`media/side` の immutable side data と、`media/stream.Event` による stream add/remove/property change である。第三者が `metadata.Key` と同じ clone 規則で packet/frame の side data を追加でき、空の item は追加 allocation や間接参照を負わない。`side.Data` は key 宣言と clone 規則だけを `media/metadata` と共有し、`Document` の scope、carrier origin、raw block は持たない。それらは stream や asset を記述する control plane の概念で、単一 item に付く値には意味がないためである。Event の follow/ignore/fail は `Undecided` を初期値とし、live topology の既定 policy を暗黙に決めない。

`stream.Descriptor` は `stream.ID` を持つ。`schema.ID` は unit の種類を表すため、同じ schema を運ぶ 2 本の audio stream を区別できず、removal と property change が対象を指せない。topology を inspect した側が採番し、core は値を解釈しない。M4 の descriptor fingerprint と M7 の multi-stream mapping はこの identity の上に乗る。

宣言・検証に留める contract は、`access.Reference` の canonical/redacted 表現、`access.Provider` の scheme declaration、transaction class、spool specification、immutable bounded probe view/range request、snapshot identity、`endpoint` の topology/realtime trait、Device descriptor/reference、明示 opt-in の `DeviceQuery` である。Provider の scheme conflict は既存の `plugin.Declaration`/catalog に載せ、独自 registry は作らない。Host/package import や型の構築は device scan、permission prompt、network access を起こさない。

M4 は Provider/Endpoint declaration の planner binding と、宣言 capability に基づく Open 前 diagnostic を担当する。M5 は既に bound 済み operator/output の transaction execution/rollback を担当する。session acquire、候補間で共有する bounded probe、Format inspect、実 capability の再検証、spool insertion と temporary quota/cleanup は、file Provider と WAVE Format が最初の実 consumer になる M6 が一体で担当する。M9 は device/session Endpoint と clock を実装する。timestamp origin、latency/buffer、drop/reconnect/discontinuity、exclusive/shared、hotplug、multi-output `AllOrNothing` は consumer が現れる milestone まで型を増やさず、M3 では設計文書だけを正本とする。

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

## M5 の contract 分類

M5 の公開 export はすべて実行 consumer を持つ。`host.Prepare`/`Prepared.Plan`/`Prepared.Run`/`Prepared.Close`、`host.Run`、`Result`/`Failure`/output outcome/event、observation と cleanup timeout は `plugin/pcm/linear` の walking skeleton と `host` の lifecycle/failure matrix が使用する。M6 の `standard.Convert` と CLI はこの façade をそのまま利用し、private `Program`、queue、task group、resource manager を公開しない。

`flow.FanInPolicy`/`Batch`/`Joiner` は private runtime の Zip と `SerialFanIn` consumer、終端の宣言は `Flush` 一つに統合したため、別の finalizer 契約は持たない。`SerialFanIn` は ordering algorithm ではなく callback の同期直列化と input ordinal を提供する。`flow.Direct` の consumer は MP4 mux の packets port で、runtime の topology gate と `plan.FanIn.Direct` 投影が受け取る。宣言した port は単一 routed producer が同じ island から駆動し、その call 順を physical order と扱う。MP4 では demux が sample offset の merge で作る格納順がその call 順になる。宣言のない generic Serial 構成の cross-track physical interleave は契約しない。generic Serial をこれらの構成だけで Compile から reject せず、将来の Stable/byte reproducibility は execution signature と別 ordered policy/backpressure の consumer として追加する。`plan.Runtime` の island/buffer/fan-in projection は `Prepared.Plan` と planner/runtime test が使用する inert snapshot で、実行 closure や resource handle を含まない。

`access.Opening`、`endpoint.Opening`、`access.Direct` は Host が node-local boundary view として component へ渡し、provider/endpoint/direct fixture が「選択された capability 以外を見せない」ことを検査する。`access.Flusher`/`Syncer`/`Transaction` は success/rollback coordinator が実行する。`job.Adaptor` と direct input/output choice は resource と通常 component graph node の間を明示的に接続し、Host binding test と PCM の source/sink adaptor が使用する。raw resource の `Close` authority は component に渡さない。

`buffer.Edit` と `audio.Editor` は shared/read-only payload の transactional copy-on-write consumerである。exclusive frame は backing をそのまま move し、fan-out 後は変更 branch だけ Job-local allocator から複製する。buffer/audio unit test と typed runtime fan-out test が commit/discard、0-allocation linear edit、branch isolation、grant repayment を検査する。将来の公式 audio filter は M8 で同じ API を使用する。

`internal/{run,memory,task,observe,bound}` の export は `internal` import rule の内側だけにあり、Host/Program と対象 test が全ての caller である。plugin/surface の public contract ではない。M5 で追加した export に consumer 未定の宣言は残していない。

先行 milestone から引き継いだ宣言は別に監査する。M3 の `access.SpoolSpec` と M4 の `job.ResourcePolicy.AllowSpool` は M5 の transaction coordinator の consumer ではなく、M6 の WAVE/file 経路が Plan node、temporary storage、cancel/rollback cleanup を初めて実装する。M5 の `resource.Request.Workers` は grant で制限された node-local `OpenContext.Tasks()` が実消費し、component が grant を超えて task を開始できない。consumer の無い `Temporary` resource dimension は M5 review で削除し、M6 が spool storage interface と課金単位を同時に決める。realtime `Clock` は M9 の Endpoint と同時に追加する。

## M6 の contract 分類

M6 で実 consumer を得る宣言は次である。`media/format` の Probe/Inspect と capability alternative、`media/carrier` の carrier identity、`metadata.RawBlock` の raw preservation、`media/codec` の codec Binding、`access` の Source/Sink capability と `Own`/`Borrow`、`ProbeView`/`RangeRequest`、`SpoolSpec`/`SpoolStorage`、`job.ResourcePolicy.AllowSpool`、transaction class と `access.Transaction`/`Flusher`/`Syncer` の file 実装である。いずれも WAVE Format または local file Provider が実 consumer になる。

M6 でも宣言に留める contract と担当は次である。`metadata.Mapping` と loss report は実 encoding 間変換が現れる M7、`stream.Event` の live topology 既定 policy と `flow.FanInPolicy` の Zip/SerialFanIn 以外の semantics は実 component が現れる M7 以降、別 container の global time-ordered fan-in は M9 以降（実 consumer と backpressure 設計が現れた時）、`endpoint` の Device/DeviceQuery と realtime clock は M9 が担当する。M6 で使わないと判断したものは、この節へ担当を書いてから残す。担当は正本 roadmap の `M0`〜`M11` に実在する milestone でなければならず、`testkit.Coverage.AssignUncovered` がその許可集合を機械的に検査する。「remote Provider を作る milestone」のような roadmap 外の文字列は owner にしない。

`access.Snapshot` は M6 review で実 consumer を得た。local file Provider が `access.Snapshotter` として content identity を報告し、Host が phase 間で照合する。意味は [access](access.md#snapshotretry再現性) を正本とする。再取得と retry は operation 自体が無いため宣言も置かず、remote Provider を実装する milestone が実操作と同時に導入する。

M6 review では、操作 view と consumer を持たなかった `Reopen` と `ConcurrentRead`、blocked syscall を解除できない file Provider が誤って宣言していた `CancelableRead` を削除した。`Reopen` と `ConcurrentRead` は HTTP/S3 等の remote Provider を実装する milestone で、再取得操作・snapshot semantics・並行 range consumer と同時にだけ再導入する。`CancelableRead` は blocked I/O を context cancel または Close で実際に解除できる Provider と、その保証を検査する test が同じ milestone にある場合だけ再導入する。session factory は stdin/stdout を扱う M9 で複数 session を生成する実 consumer が必要になった場合に限り再設計する。

`resource` の temporary 次元は M5 review で削除し、**戻さないと決めた**。spool の上限は `job.ResourcePolicy` の spool 専用 quota として持ち、Host が Job 単位で storage を所有する。予約次元へ戻さないのは、spool を使う理由が「最終 size が確定しないこと」であり、Open 前に確定量を予約する `memory.Manager` の model と一致しないためである。spool 自体は Host 内部に閉じ、`plugin.OpenServices` へ temporary service を公開しない。MP4 fragmented 等の第二の consumer が現れた milestone で、共通 service へ昇格させるかを決める。

`PolicyFor` の preset は `AllowSpool` を有効にしない。spool は `standard.WithPolicy` 等から明示した `job.ResourcePolicy` に正の上限と storage を与えた場合だけ利用できる。M7 の MP4 fragmented/spool でも preset の暗黙 effect へ変えず、必要なら独立した user choice として設計する。

M6 が新設する write 側 capability（sink の逐次書きと位置指定書き）と narrow view は、WAVE mux と local file Provider が同時に consumer になる。size 不明 header を書く streaming 出力は M6 では提供せず、需要が確認された milestone が opt-in と `Plan` warning を伴って追加する。

M6-1 は data unit schema の所有者も是正する。Access boundary の byte stream は `access`、container framing（`packet.Chunk`）は `media/format`、codec packet（`packet.Packet`）は `media/codec` が所有し、`plugin/pcm/linear` の固有宣言を削除する。schema identity が plugin 固有だと、WAVE demux と PCM parser のように別 plugin の component 同士が接続できず、codec Binding が format tag から parser を選ぶ設計も成立しない。これは新しい contract の宣言ではなく、既に consumer を持つ宣言の移設である。

M6-2 は sink 側に位置を表現できる canonical schema を足す。読み側の item は順序どおりの byte 列でよいが、書き側は末尾追加と絶対 offset patch を区別する必要があり、WAVE の `RIFF`/`data` size 後追い patch がその最初の実 consumer になる。boundary の narrow view は Provider node にだけ渡る設計を維持し、mux は自分で I/O せず位置付き item を下流へ渡す。読み側 schema に意味のない append 印を持たせない。

M6-2 は Inspect も担当する。「既知 format の header を読む」ことと「どの format か決める」ことは別操作であり、前者の実 consumer は明示指定された WAVE である。後者（共有 bounded probe、自動判別、非 seekable 入力の prefix replay）は M6-3 に残す。Inspect を後段に残すと M6-2 が header 情報を fixture か config から捏造することになり、M6-1 が carrier descriptor 規則で除去した形を再導入する。

M6-5a は Job に Format の指定手段を足す。`job.Input` に任意の Format hint、`job.Output` に Format request を持たせ、Host が Prepare 中に対応する Read/Write trait を選んで挿入する。M6-4 時点の `job.Output` は Format を指定できず、Host が自動挿入するのは入力側だけなので、`out.wav` を指定しても solver が raw writer を選び得る。同じ要求に複数の実装が該当する場合は M8 の variant selection を先取りせず ambiguity diagnostic にする。

**拡張子と Format の対応は Format trait が宣言し、catalog 経由で解決する。** `standard` に固定表を置くと、`Set.Add` で足した第三者 format が拡張子ベースの利便性に参加できない。第三者は `standard` を編集できないため、これは「第三者が core を変更せず追加できる」という中心目標の穴になる。拡張子は evidence ではなく hint であり、入力では content evidence を優先し、複数の Format が同じ拡張子を宣言した場合は ambiguity diagnostic にする。file path から Reference を作る constructor は責務を持つ `plugin/file` に置く。

M6-5b の progress は Host 全体の設定ではなく Run/Prepared 単位の bounded event delivery とする。M6-4 時点の event は `Run` 完了後の `Result.Events` にしか現れず、実行中の表示ができない。**溢れた場合は media 処理へ backpressure をかけず、drop して落とした件数を報告する。** 待たせると observation が hot path のコストになるためである。CLI renderer の失敗は cancellation へ接続し、surface 側で polling や独自の進捗計算を行わない。cleanup timeout によって live sink の join を確認できない場合、surface は renderer を停止状態へ移し、履歴補完や同じ stream への結果表示を続けない。既存の Host-global `host.Observe` option と Host の observation state は削除し、snapshot-only と live delivery の両方を同じ per-Run option に一本化する。共有 catalog/platform の lifetime と surface ごと・実行ごとに異なる観測要求を結び付けず、Host default と Run override の二重設定を残さない。

**direct resource と Inspect の組み合わせは M9 が担当する。** M6-2a/M6-3 が Inspect を入れたことで、`access.Direct[T]` として渡す direct source は Inspect 必須 Format（WAVE 等）で Prepare できない。Inspect は `access.Opening` を要求するのに対し、direct resource は session/capability model の外を通るためである。これは deferred contract ではなく Inspect 導入で新しく開いた穴だが、`Direct[T]` を session model へ統合する作業の最初の実 consumer は CLI の stdin/stdout であり、[capability](capability.md) がそれを M9 に置いている。M6-5 の `cmd/godec` は入出力指定だけなので M6 内に consumer を持たない。**M6-4 では、direct source と Inspect 必須 Format の組み合わせが明示的な diagnostic で失敗することを条件とする。** 分かりにくい失敗のまま残すと、M9 で原因の所在を追えなくなる。

M6-3 の自動判別は content evidence を持つ format にだけ成立する。WAVE は RIFF/WAVE signature で判別できるが、raw PCM は任意の byte 列と区別できない。したがって raw PCM は判別結果ではなく**低順位の明示的 fallback** とし、Plan に `Automatic` と専用 reason で現れ、content evidence が無い旨の warning を伴う。signature が一致した後の malformed 入力は raw へ降格せず失敗する。同順位の fallback が複数あれば identity 順で黙って選ばず ambiguity diagnostic にする。**この判断は M6-5 で「hint の opt-in」に確定した。** evidence 無しの raw fallback は既定で禁止し、入力ごとの明示 Format hint/config がある場合だけ許す。既定 config での fallback は rate/layout/endian を捏造して成功を返すことを意味し、利用者から見れば雑音が出力されて正常終了する。`.raw`/`.pcm` という拡張子だけでは既定値の承認とみなさず、raw 入力は typed API と CLI で明示 config を要求する。content evidence は常に hint より優先し、signature 一致後の malformed 入力は raw へ降格せず失敗する。M6-3 完了条件の「入力 format を明示せずに判別」は content evidence を持つ format についての条件と読む。

非 seekable な WAVE 入力は M6 では扱わない。signature 検出後に capability diagnostic とする。WAVE Inspect を逐次化すると、data の後方にある metadata を保存するために Inspect 時点で入力全体を読む必要が生じ、bounded prefix の契約を破るためである。旧実装の demuxer も `io.ReadSeeker` を要求しており、capability の変更ではない。M6-3 の逐次入力 gate は「共有 Probe と raw fallback が prefix replay で動く」までとする。

M6-2c は `plugin.CompileContext` へ cancellation を通す。marshalled metadata は sink へ渡す payload であり、payload の grant は `Compile` で宣言するため、size を知るには `Compile` から `Marshal` を呼ぶしかない。`context.Background()` では planning の cancellation と budget を無視する。渡すのは `Value` が常に nil の context に限り、`Compile` の決定性を honor system にしない。この結合は、外部入力の大きさで決まる metadata payload に上限を設けるかという M8 の判断とも接する。

M6-2c は `plugin.Component` を control-plane 拡張へ広げる。M5 時点の component は typed `Spec` を必須とするため、`Open` も port も持たない metadata Encoding を宣言できない。合成時の拡張がすべて component に乗るという M6-0 の決定の帰結として、Spec を optional にする。有効性は「Spec と port を持つ」または「trait を一つ以上持つ」で判定し、どちらも持たない component は従来どおり拒否する。trait は port shape を要求するかどうかを自分で表明し、control-plane component は solver 候補にならず、Job node に指定された場合は専用の diagnostic で拒否する。詳細は [plugins](plugins.md#m6-完了条件) を正本とする。

M6-2 は `metadata.Encoding` を component trait として追加する。M5 の `metadata.Binding` は宣言だけで、Parse/Marshal の契約は foundation test の private interface にしかない。M3 の skeleton は encoding を `flow.Operator` として模していたが、Parse は Inspect の最中すなわち Compile より前に必要で、`Open` は Program 確定後なので循環する。trait なら composition 時に解決でき、trait 判定規則にも合致する。Parse/Marshal は payload grant を取らない control-plane 操作とする。ただし embedded artwork は MB 級になり得るため、**外部入力の大きさで決まる control-plane allocation に上限を設けるかは M8 が決める**。RIFF INFO だけを扱う M6-2c では blocker にならない。

M6-2 の Inspect transport は、M6-0 が作った marker key + 型消去値 + 型付き accessor の機構を再利用する。prepared value 専用の単一 slot を作らない。2 種類目の prepared value が現れた時点で map へ作り替えられるためで、機構を二つ持たない。型付きの出入口は `media/format` に閉じる。

**format の選択は Prepare の段階であり、solver の gap-fill ではない。** Inspect transport は「node の Compile より前に Inspection が解決済み」を前提にする。M6-2a は明示指定なので成立するが、M6-3 の自動判別を solver が demux 候補を挿す形で実装すると前提が壊れる。documented pipeline どおり `Probe → 選択 → Inspect → Shape → Compile → Solve` の順を守る。Inspect は node ごとに 1 回、Compile は budget の範囲で複数回呼ばれるので、Compile が Inspection に対して pure であることを test で固定する。

M6-2 は codec Binding を実際の選択へ接続する。M5 の solver は入力 schema だけで候補を索引しており、codec tag と無関係に同じ schema を受ける Parser/Decoder がすべて候補になる。PCM だけの間は不可視だが MP3/FLAC が入る M8 で誤選択になる。ただし tag が絞るのは codec/parser の選択だけとし、codec Binding を持たない component（converter、resampler、bitstream filter 等）は候補に残す。tag を全候補の filter にすると solver の一般性が失われる。

M6-1 は carrier descriptor の意味も確定する。byte schema の descriptor は stream id と metadata までを運び、sample properties と media の time base を持たない。filesystem provider は media format を知らず、planner にも下流から source へ descriptor を逆伝播する仕組みが無いため、byte を消費する component が自分の config または container header から出力の意味を確定する。M5 の PCM 経路は fixture source が byte descriptor に PCM properties を捏造していたので成立していたが、実 Provider では不可能である。

`stream.NewDescriptor` が有効な time base を必須とするため、carrier descriptor は canonical な placeholder を使い、`access` が名前付きで公開する。`access.Bytes()` は `Time` trait を持たず byte edge の time base を消費する経路が存在しないので、値は実行に影響しない。**timeline を持たない stream を型で表す正直な形は M7 が担当する。** time base 必須を「schema が `Time` trait を宣言する場合だけ」に条件化する変更で、`stream.NewDescriptor` の validity、`validateCompiledOutputs`、descriptor fingerprint、erased な `schema.Descriptor` への timeline 有無の露出を伴う。M7 は seek と timeline を扱うため、そこで初めて実 consumer が現れる。placeholder のまま恒久化させない。

M6-1 は payload grant の勘定も是正する。`resource.Request.Memory` の意味を「in-flight 1 item 分の payload bytes」と明確化し、queue 容量分の乗算を Host が行う。M5 の `host.Prepare` は component の宣言値をそのまま予約しており、[runtime](runtime.md#allocator-と-pool) が定めた「予約 capacity を queue 単位で account する」を満たしていない。payload は下流へ move されるため、bounded queue が保持する item 数だけ生成元の allocator が同時に必要になるが、queue 容量は Host policy であり component は知らない。`[]byte` payload 時代は sink 直前の component がコピーしていたため retention chain が切れて表面化しなかったが、handle payload では即座に `ErrLimit` になる。component 側の宣言は 1 item 分のまま変えない。

canonical byte schema の payload は `media/buffer.Handle` とする。`[]byte` を維持すると、`Reader` が read ごとに所有権を渡す以上 buffer を再利用できず、Access boundary の payload だけが Job grant の外の GC allocation になる。`packet.Chunk`/`packet.Packet`/`audio.Frame` がすべて `buffer.Handle` を包む中で boundary だけを例外にしない。したがって byte を produce する component は `resource.Request.Memory` を宣言し、`OpenContext.Buffers()` から確保する。

M6-1 は Format を方向別の component trait にする。boundary へ要求する capability alternative は方向で異なる（raw PCM の読みは逐次または位置指定+既知 size、書きは逐次で足りる）のに対し、M5 時点の `format.Format` は方向を持たない単一の `[]access.Alternative` を持ち、しかもどの component が消費するかを表せない。したがって alternative を `format.Format` から trait へ移し、`Format` 自体は identity、carrier、packetized だけを持つ方向中立な宣言に戻す。M5 時点の `format.Format` は production consumer を一つも持っていない（`linear.Raw()` は test と Example からしか参照されない）ため、この移動は既存 consumer を壊さない。Probe/Inspect の contract は同じ trait へ後から足すが、形を決めるのは実 consumer が現れる M6-3 とし、M6-1 では宣言しない。

M6-0 で削除する合成 API は `access.ProviderRole`、`access.Provider` の manifest 形とその declaration 生成、`endpoint.Component`、`host.Providers`/`host.Endpoints` である。Access と Endpoint は宣言する component の trait になり、`plugin.Set` が唯一の合成値になる。`plugin` に増えるのは marker key 付きの trait slot 一つで、取り出しは各 package の型付き accessor に閉じる。

**trait 種は foundation が定義する閉じた集合である。** 判定規則は「host が Open より前に参照しなければ binding を決められないか」とする。Access trait は「その component が byte boundary であること」、Format trait は「boundary へ何を要求し、後に何で判別するか」を表し、どちらも bind 時に host が読む。Open 以降にしか要る場面がない情報は trait にせず、`Compile`/`Open` の contract に置く。第三者が拡張できるのは trait の**種類**ではなく**実装**であり、host が解釈しない独自 trait 種を付けても無視される。

M6 時点で foundation が定義する trait 種は Access（source/sink）、Format（read/write）、Metadata Encoding である。Format 側の詳細は下の M6-1 の項、Metadata Encoding は下の M6-2 の項を参照する。

## 文書全体の完了条件

この節は拡張点全体の最終状態を示す gate であり、個別 milestone の完了判定には各 milestone 固有の条件を用いる。M3 の判定には上記「M3 完了条件」だけを使う。

- capability matrix の各行を第三者 definition として追加でき、core/surface の switch を変更しない。
- object Access、session Endpoint、Device Endpoint、Format の責務が混ざらない。
- 初期未対応 capability が runtime panic ではなく Compile diagnostic になる。
- video/subtitle/custom schema と live/device fixture が、同じ Host/planner/runtime へ参加できる。
- decode 実装を持たない stream（MP4 の video/subtitle track など）が、raw carrier と structured diagnostic を通じて情報を失わずに copy される。

## M7 の contract 分類

`plugin.Scratch` の三 method はいずれも MP4 mux の chunk-offset journal を実 consumer に持つ。`Append` は Open 後に journal 全体を一度だけ確保し、`WriteAt` は到着した chunk の出力 offset をその track の region へ書き、`ReadAt` は Flush で region ごとに読み戻して `stco`/`co64` を patch する。positioned write が要るのは、demux が source の格納順に emit するために track の chunk が互いに割り込んで届くからであり、append-only の journal では track ごとの run を作れない。

`flow.Direct` は MP4 mux の packets port を consumer に持つ。runtime の topology gate、`plan.FanIn.Direct` 投影、`plan.Buffer.Connections` はいずれもこの一つの宣言を説明するために存在する。

`job.Mapping` の input/output index は M7 では 0 だけを受け付ける。複数 input/output を持つ surface が現れる M9 まで、この二つは「将来の値域を先に型へ置いた」のではなく、mapping が結び付ける両端を名前で指すための識別子として使う。M9 で rich selector を追加する時に、値域の拡張と duplication/並べ替えを同時に扱う。

## M8 の contract 分類

`sample.Coding`、`sample.Packing`、`sample.Endian`、`sample.Description` は `plugin/pcm/linear` の decoder/encoder、`plugin/wave` の fmt header、`plugin/mp4` の sample entry を実 consumer に持つ。`sample.Frames`、`sample.Schema`、`sample.CodingOf`、`sample.Stores` は canonical schema 四つと scalar 型の対応を一箇所に集める。`Frames` は port 宣言、`Schema` は testkit の fixture 検証、`CodingOf`/`Stores` は codec component が扱えない coding を Compile で閉じるために使う。

`sample.LayoutCodec`/`CodingCodec`/`EndianCodec` は `plugin/pcm/linear` の config schema を consumer に持つ。個々の plugin が同じ enum を書き直さないための共有であり、M8-3 の processor と M8-6 の FLAC encoder が同じものを使う。

`sample.Layout` の `Count`/`Mask`/`FromMask` は WAVE の `dwChannelMask` を実 consumer に持つ。`Positioned`、`At`、`Has` は現在 `Layout.String` と test だけが呼ぶ。channel 位置を見て動く実 component は M8-3 の mixer/channel 処理が最初になるので、そこを consumer とする。位置を問い合わせられない layout は書き込み専用の値になるため、三つは M8-1 で置く。

`audio.Plane` は testkit の frame fixture、`plugin/pcm/linear` の pack/unpack、`plugin/audio` の converter を consumer に持つ。leased plane を typed slice として読む唯一の入口であり、`unsafe` を三箇所へ複製しないために置く。

`plugin/audio.ConverterIdentity` は explicit graph が converter を名前で指すための入口である。planner は descriptor 探索で converter を選ぶので composition の binding は要らないが、`linear.DecoderIdentity` と同じく「その component を Job から指名する」公開 API として置く。

`plugin/wave.CodecTag`/`Codings` と `plugin/mp4.SampleEntryCodings` は `standard` の composition を実 consumer に持つ。format が宣言する codec tag と、その tag を読む component の対応を composition だけが持つという [M8-C02](m8-0.md) の形を、公式 family でそのまま満たすための入口である。
