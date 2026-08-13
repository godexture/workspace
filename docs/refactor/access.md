# access と endpoint contract

## 責務

Access contract は、planner/runtime が必要とする I/O capability と lifecycle を表す。authorization や plugin sandbox ではない。権限境界は [application が所有する権限](#application-が所有する権限) にまとめる。

## 結論

Access Provider と Device/Session Endpoint は Godec foundation の拡張点に含めるべきである。ただし同じ interface に統合しない。

- **Access**: file、HTTP object、S3 object、memory 等、format が読む・書く byte object への到達方法
- **Provider**: reference を Access source/sink に解決する plugin。URL protocol に限定しない
- **Endpoint**: RTSP、RTP、HLS session、camera、microphone、speaker 等、typed stream を直接生成・消費する graph component
- **Device**: Endpoint のうち、物理・OS resource、permission、clock、hotplug を持つもの

foundation は contract、capability negotiation、lifecycle、policy hook を提供する。HTTP、S3、Oto、OS camera API 等の具体実装を foundation や pure-Go base distribution に必須化しない。

この分離が必要なのは、すべての protocol が一つの byte stream ではないためである。HTTP 上の単一 MP4 object は Access source だが、HLS playlist や RTSP session は複数 resource、clock、再接続、動的 topology を扱う Endpoint である。camera を擬似 URL の `io.Reader` にすることも、seekable file と同じ semantics を装うことになる。

## 現行コードから確認できる問題

現在は I/O の意味と所有権が複数 layer に分散している。

- `sdk/conversion.InputSet` が main/aux input を一律 `io.ReadSeeker` に固定する。
- `core/routing` が `io.ReadSeeker` と `io.Writer` を live spec に保持する。
- Demuxer factory は `io.Reader` を受けた後、各 format が `io.ReadSeeker` へ type assertion する。
- resolver は format 候補ごとに同じ cursor を `Seek(0)` して probe する。
- CLI、web example、WASM が別々に file、temporary file、`[]byte`、close を管理する。
- transactional output は CLI の local file にだけあり、mux `Finalize` と sink `Commit` が一つの lifecycle になっていない。
- Oto playback は generic graph endpoint ではなく、conversion package 固有の `PlaybackSink` 経路を持つ。
- WASM は input/output を全量 `[]byte`/`bytes.Buffer` にし、large input と incremental I/O に対応しにくい。

この構成では、non-seekable pipe を扱える format まで seek を要求し、新しい storage/protocol/device を追加するたび複数 surface を変更することになる。

## 用語と責務

| 概念 | 入出力 | 責務 | 扱わないもの |
|---|---|---|---|
| `access.Reference` | locator | scheme/provider 選択に必要な canonical reference | media format |
| Access Provider trait | reference → source/sink plan/session | credential、network、range、retry、transaction | container/codec semantics |
| `access.Source` | byte read capability | read、random access、size、snapshot、cancel | stream topology |
| `access.Sink` | byte write transaction | write、patch、flush、commit/abort | mux header/index |
| Format | byte capability → carrier/stream | probe、inspect、demux/mux、format seek | URL、credential、retry |
| Endpoint | typed graph source/sink | live session、clock、typed units、backpressure | generic byte object transaction |
| Device | Endpoint | permission、device capability、hotplug、exclusive/shared access | container format |

Access と Format の境界は次である。

```text
reference
  -> Access Provider
  -> byte source capabilities
  -> bounded host probe
  -> Format
  -> typed streams/carriers

typed streams/carriers
  -> Format
  -> transactional byte sink
  -> flush/sync
  -> commit or abort
```

Endpoint はこの byte 経路を必ずしも通らない。

```text
camera/RTSP/HLS source
  -> typed Packet/Frame/Event ports
  -> processors/codecs/formats/other endpoints
```

## Job の endpoint 表現

`Job.Input`/`Job.Output` は nil field を複数持てる loose struct にせず、constructor で作る tagged choice にする。

概念例:

```go
job.InputFromRef(access.Parse("file:///input.flac"))
job.InputFromSource(access.Own(file))
job.InputFromSource(access.Borrow(reader))
job.InputFromEndpoint(camera.Capture(camera.Config{Device: "front"}))

job.OutputToRef(access.Parse("s3://bucket/output.flac"))
job.OutputToSink(access.Own(writer))
job.OutputToEndpoint(oto.Playback(oto.Config{}))
```

surface DTO では明示的な tagged union にする。

```json
{
  "kind": "reference",
  "reference": {
    "scheme": "s3",
    "target": "bucket/output.flac"
  },
  "access": {
    "credential": "credential-ref"
  }
}
```

raw credential、signed URL、local secret path を public `Plan` や diagnostic の node label にそのまま入れない。Reference は private canonical target と policy に従う redacted display を分ける。

library が既に reader/writer を所有している場合、Provider lookup を経ず直接 Source/Sink adaptor を渡せる。これにより custom storage のためだけに plugin を作る負担を増やさない。

## Access Provider

Provider は独立した manifest/interface ではなく、byte source または sink component に付く trait である。component として marker、descriptor、config schema、provenance、`plugin.Set` の composition/override 規則を共有し、trait の acquire 操作が reference を job session resource へ解決する。

Provider は概念上、次を宣言する。

- 対応する reference kind/scheme
- 付き先 component の方向（source は 0-in/1-out、sink は 1-in/0-out）
- config schema と secret field
- 静的に分かる capability alternatives
- read-only inspection の effect
- open/close/cancel semantics
- retry と snapshot consistency
- sink transaction class

同じ scheme の source trait と sink trait は共存できる。同じ方向での scheme 重複は host 構築時に error とする。利用者が意図して置き換える場合だけ、対象 component identity を指定する `Set.Override` を使う。暗黙の last-wins や registration order は使わない。

「Provider」は local file、memory、object store を含む product 上の概念名である。public Go contract は `access.Source`/`access.Sink` の component option と型付き `SourceTrait`/`SinkTrait` accessor に分け、すべてを一つの汎用 protocol interface にしない。

## lifecycle と Prepared Job

source は media probe より前に開く必要がある。一方、output は dry-run/plan 時に作成・truncate してはならない。全 component を一つの `Open` 順序へ押し込まず、job session lifecycle を明示する。

```text
Normalize Job
  -> select Access Provider / Endpoint definitions
  -> resolve config and references
  -> acquire input sessions
  -> snapshot + bounded Probe
  -> Inspect
  -> Compile/Optimize graph
  -> produce immutable Plan + private prepared state
  -> begin output transactions
  -> Open operators/endpoints
  -> Run
  -> Finalize codecs/formats
  -> flush/sync sinks
  -> commit outputs
  -> Close
```

public API は `Plan` と live resource を混同しない。

```go
prepared, err := h.Prepare(ctx, request)
if err != nil {
    return err
}
defer prepared.Close()

plan := prepared.Plan()
result, err := prepared.Run(ctx)
```

`Host.Run(ctx, request)` は Prepare + Run の convenience API とする。`Host.Plan` は Prepare して Plan を返した後 resource を閉じる read-only convenience とできるが、その Plan を将来そのまま実行できるとは約束しない。後で実行する場合は source を再取得して snapshot/capability を再検証する。

CLI の dry-run は input の bounded probe/inspect を行ってよいが、output transaction は開始しない。network access や input spooling が必要なら Plan 前の effect として明示し、budget/policy で拒否できるようにする。

## source capability

capability は巨大な boolean struct にせず、operation と semantics を持つ小さな contract の組み合わせにする。
M6 の公開 capability は sequential read、position-independent random read、stable known size、
sequential write、random write である。`AllOf` は一つの alternative 内の AND を作り、
`Requirements.Alternatives` は優先順を持つ OR の列である。reopen、concurrent range read、
cancellation/close-unblocks-read は実装できる Provider と操作 view が同時に現れるまで型を置かず、
再導入条件を [scope](scope.md#m6-の-contract-分類) に記録する。

random read は共有 cursor を持つ `Seek` より `ReadAt` 相当を優先する。Provider が seek しか提供できない場合、host adaptor が lock と cursor restore を伴う random view を作れるが、その serialization/cost を capability に反映する。

概念例:

現在実装されている capability alternative、bounded `Random` view、snapshot の
正確な API は [access の Example](../../access/example_test.go) を正本とする。
特に `ExampleNewRequirements` は、宣言用の comparable capability と component に
渡す narrow view を混同しない最小経路を実行している。

実際の component `Open` には、宣言した requirement を満たす narrow view だけを渡す。不可能な operation を nil field として渡し、plugin 内で type assertion させない。

Format は capability alternative を宣言できる。

```text
WAVE inspect:
  random + stable size

MP4 output:
  random-write
  OR sequential + fragmented-mode
```

planner は format mode、source/sink capability、policy をまとめて選ぶ。capability 不足を runtime panic/type assertion error にしない。

## cancellation と ownership

`io.Reader`/`io.Writer` adaptor は ownership を曖昧にしない。

- `access.Own(x)`: Host が session 終了時に閉じる。
- `access.Borrow(x)`: Host は閉じない。

Close が blocked read/write を解除できる Provider が現れた時だけ、その操作と保証を contract に含める。単なる `io.Reader` を context-aware と偽らない。non-cooperative borrowed handle は timeout 後も goroutine が残り得るため、Plan warning または policy rejection の対象にする。

input source の cursor を probe、inspect、run が暗黙共有しない。M6 の host は prefix replay または independent range view を選び、chosen Format には定義済みの開始位置から渡す。再取得は remote Provider の実操作と snapshot semantics が揃うまで行わない。

## bounded Probe

Format の `Probe` に生の Source を渡さない。host が一度取得した immutable `probe.View` を全候補へ渡す。

```go
type ProbeResult struct {
    Confidence Confidence
    Need       []Range
    Evidence   []Evidence
}
```

Probe の規則:

- pure で cursor を変更しない
- input/component/global の byte budget を越えない
- candidate ごとの独立 reader または immutable range を使う
- extension/MIME 等の hint と content evidence を区別する
- 追加 range が必要なら bounded request を返す
- malformed data と mismatch を区別する
- 同点は catalog order でなく canonical identity/priority/evidence で決める

host は複数候補の同じ prefix/range request を deduplicate する。sequential source から読んだ prefix は保持し、選ばれた Format の実行 reader へ一度だけ replay する。全候補ごとの rewind と再読をなくす。

suffix/range が必要でも source が提供できない場合、Format の sequential mode、host spool、候補除外を policy に従って比較する。

## spool adaptor

capability が不足した時、Host は user policy と resource budget が許す場合だけ spool adaptor を挿入できる。

- non-seekable finite source → temporary random-readable source
- non-random sink → temporary random-writable sink → final sequential copy
- memory threshold を越えたら host-managed temporary storage
- live/infinite source を有限 spool で random access 化しない

spool は隠れた実装詳細にせず Plan node/effect として表示する。

- 予測/上限 bytes
- memory/disk 使用
- 開始 latency
- final copy
- rollback class
- dry-run で全 input 消費が必要か

resource manager は spool file/segment を job scope で所有し、cancel/failure/Close で削除する。

## sink transaction

byte object output は `io.Writer` だけでなく transaction として扱う。

```text
Begin private/staged sink
  -> Write / optional WriteAt
  -> Format Finalize
  -> Flush
  -> Sync if policy requires
  -> PrepareCommit
  -> Commit

any failure/cancel
  -> Abort
  -> Close
```

Provider は transaction class を宣言する。

- atomic replace
- staged/multipart commit
- rollbackable until commit
- append-only/non-rollbackable
- live/no-commit endpoint

local file の temporary file、Windows replace、permission preservation、directory sync は file Provider が扱う。S3 multipart upload、object version/precondition は S3 Provider が扱う。Format は pathname、rename、multipart を知らない。

複数 output の完全な atomic commit は一般には保証できない。Host は全 output で `PrepareCommit` まで進めた後、deterministic order で commit し、途中失敗時に次を structured result に残す。

- committed
- aborted
- commit outcome unknown
- rollback attempted/unsupported

利用者が `AllOrNothing` を要求した場合、全 sink が共同 transaction を提供できなければ Compile error にする。既定の best-effort multi-output では partial-commit risk を Plan に表示する。

stdout、socket、live broadcast 等は rollback 不能である。成功条件を file transaction と同一にせず、公開済み byte/時間と failure point を result に含める。

## object protocol と session protocol

URL scheme があることだけで Access Provider に分類しない。

| 例 | 分類 | 理由 |
|---|---|---|
| local file、HTTP object、S3 object | Access | 一つの byte object として読める |
| stdin/stdout、pipe | direct Access adaptor | surface が既存 handle を渡す |
| RTSP、RTP、SRT、RTMP | Endpoint | session、clock、reconnect、typed packet を持つ |
| HLS、DASH | Endpoint | playlist、複数 segment、variant、live update を持つ |
| camera、microphone、screen | Device Endpoint | typed frame と hardware clock を持つ |
| speaker/display | Device Endpoint | realtime sink と presentation clock を持つ |

session Endpoint が HTTP segment 等を必要とする場合、catalog/service locator を runtime に自由検索させない。component が Access dependency を宣言し、Compile が具体 Provider と policy を binding し、`Open` にはその job/reference scope に限定した narrow `Fetcher` を渡す。

```text
HLS Source requirement: Fetcher(http, https)
  -> planner binds selected HTTP Provider
  -> Open receives bounded Fetcher
  -> segment requests inherit credential/network/resource policy
```

この依存は Plan に現れ、provider override や license/provenance も追跡できる。

## Endpoint component

Endpoint は閉じた `Device` role registry を新設せず、通常の open typed `component.Spec` で表す。

- source endpoint: input port 0、typed output port 1..N
- sink endpoint: typed input port 1..N、output port 0
- bidirectional/session endpoint:必要な typed ports を明示

追加 descriptor/trait:

- `FiniteStatic`、`LiveStatic`、`LiveDynamic`
- clock domain と timestamp origin
- realtime/offline
- latency/buffer range
- block/drop/duplicate/conceal policy
- reconnect/discontinuity semantics
- exclusive/shared resource
- irreversible external effect

これにより、Oto sink のための別 `BuildPlayback` API は不要になる。通常の Job mapping で `audio.Frame` を Oto Endpoint へ接続し、planner が sample format/rate/layout converter を挿入する。

## Device

Device は component definition と物理 instance を分ける。

- component identity: camera implementation/plugin の Go marker identity
- device reference: 現在の machine/OS session 内の instance ID
- device descriptor: 現時点の supported schema/rate/layout/latency

physical device を immutable Host catalog の component として大量登録しない。enumeration は明示的な opt-in query とする。

```go
devices, err := h.Endpoints(ctx, camera.Component)
```

Host construction/import 時に device scan、permission prompt、network access を起こさない。selected device の capability inspection は Prepare で行い、Plan に snapshot/version を記録する。Open 前に device が消えた、format が変わった、exclusive lock を取得できない場合は phase-aware error にする。

capture/playback の runtime contract:

- capture timestamp は device clock domain を明示する。
- Host は master clock を選び、必要なら drift compensation/resampling を挿入する。
- capture は downstream が遅い時に無限 block できないため、overflow/drop policy を要求する。
- playback は underrun、buffer fill、presentation latency を diagnostic/event にする。
- pause/seek は device、source、codec、filter 全体の graph control operation とする。
- hotplug/property change は mutable descriptor ではなく `stream.Event` とする。

Device 実装は optional package/distribution とする。公式 pure-Go transcode base に Oto、OS API、hardware SDK を依存させない。

## application が所有する権限

Access contract は authorization system ではない。[C16](decisions.md) に従い、foundation は path、scheme、host、CIDR、credential 等の permission DSL や Job ごとの authority engine を提供しない。

Go application は、Host へ渡す Provider、既に開いた handle、configured `http.Client`、`fs.FS`、OS/container permission を通じて利用可能な I/O を決める。untrusted request を扱う application が制限を必要とする場合は、制限済み Provider/handle を composition する。これは Godec の共通 Job semantics にはしない。

browser WASM は browser sandbox、CORS、File/Blob/Stream API に従う。demo HTTP server は固定された公式 plugin と upload/temporary result に限定し、汎用 URL resolver、third-party plugin loading、production authorization を提供しない。

credential/redaction は権限管理とは別の data hygiene として維持する。config schema の secret field または application-owned credential handle を使い、Plan、catalog、error、trace、cache key に raw secret を含めない。in-process plugin は process と同じ権限を持つため、強い隔離が必要なら別 process/OS sandbox を使う。

## snapshot、retry、再現性

random range、reopen、retry が同じ content を指す保証を Access が提供する。

- file identity: platform file identity、size、mtime、必要なら digest
- HTTP: final URL、ETag/Last-Modified、Content-Length
- object store: version ID、ETag、generation
- memory: immutable buffer identity

strong snapshot がない source はその事実を Plan に記録する。probe/inspect 後に content identity が変わった場合、黙って別 content を実行せず、再 Prepare または failure にする。

M6 の実装は次のとおりである。`StableSize` を広告する session は `access.Snapshotter` を実装し、現在の content identity を報告する。判断は session ではなく Host が持ち、acquire 時の identity を記録して run 開始前と output commit 前に照合し、変化していれば `access/snapshot` failure にする。

local file の identity は size と mtime であり、`WeakSnapshot` として報告する。truncate、grow、mtime が動く overwrite は検出できるが、同一 timestamp tick 内の同 size 書き換えは区別できない。強い identity は content を読み直すしかないため、nature を偽らずに weak と宣言する。session は開いた path ではなく開いた file を提供するので、path 差し替えは content の変化ではなく、acquire した bytes をそのまま実行する。`StableSize` は session が渡す byte 列への約束でもあるため、read は acquire 時 size で clamp し、後から追記された byte を返さない。

retry は idempotent な operation と既知 offset に限定する。途中から別 objectへ接続したり、sequential byte を重複/欠落させたりしない。live Endpoint の reconnect は byte retry ではなく discontinuity/event policy として扱う。

## observability と resource

Access metrics は surface の wrapper ではなく job session が一度だけ計測する。

- logical/physical bytes read/written
- cache/range/retry count
- wait/transfer latency
- spool bytes
- commit/abort phase
- reconnect/discontinuity
- device underrun/overflow/drop

byte ごとに中央 atomic を更新しない。I/O call/session-local counter に蓄積し、snapshot 時に集約する。observation off では timing/trace を追加せず、resource limit に必要な粗粒度 byte counter だけを局所保持する。

network/device task は host task group に属し、cancel/join の対象にする。Provider が独自 goroutine を作る場合も `OpenContext.Tasks` を利用する contract と testkit を提供する。

## performance

Access 抽象化は次を性能契約とする。

- format hot loop に reference/config/provider lookup を持ち込まない。
- Open 後は resolved narrow read/write function を保持する。
- probe range を candidate 間で共有し、同じ prefix を再読しない。
- seek cursor emulation は必要な source だけに限定する。
- sequential fast path に mandatory spool/copy を入れない。
- file/memory の compatible path は標準 `io.ReaderAt`/buffer へ低 overhead で適応する。
- I/O metrics は call/batch 単位で、byte/item ごとの global synchronization を行わない。
- network range concurrency、spool threshold、device buffer は resource grant に従う。

benchmark:

- file sequential、file random、memory、non-seekable pipe
- candidate 1/10/100 の bounded probe
- HTTP-style high-latency range fixture
- input/output spool
- transaction commit/abort
- observation off/basic
- device source/sink の steady-state latency と underrun

## package/distribution

[C13](decisions.md) の monorepo方針に従う。M5 時点で存在する配置は次である。

```text
access/              Reference, Provider, Source/Sink capability, transaction
endpoint/            clock, realtime, topology and endpoint traits
job/                 typed Input/Output choices and policy
internal/bind/       declarative Provider/Endpoint normalization
internal/bound/      immutable node-local boundary projection
host/commit.go       unexported multi-output coordination
host/cleanup.go      unexported rollback and cleanup aggregation

plugin/file/         local file Provider
plugin/http/         optional HTTP Provider
plugin/memory/       library/test adaptors if public value isある場合
plugin/oto/          optional playback Endpoint
plugin/<camera>/     optional capture Endpoint
```

下段の具体 Provider は将来配置であり、まだ存在しない。M6 の session owner、bounded shared probe/inspect、spool は file/WAVE consumer と同時に責務境界を決める。実装前から `internal/access`、`internal/probe`、`internal/commit` という package 名を正本にしない。

`io.Reader`/`io.Writer` adaptor は foundation `access` に置けるが、filesystem/network/device 実装は foundation に置かない。stdin/stdout は CLI が `Borrow` adaptor で注入する。

具体 Provider の構成は application/distribution が選ぶ。foundation は direct handle adaptor と contract を提供できるが、HTTP、object store、device/playback を必須依存にしない。公式 CLI が convenience Provider を含める場合も、それを library Host の権限 policy として流用しない。

## 現行コードの移行

| 現行 | 置換先 |
|---|---|
| `conversion.InputSet{io.ReadSeeker}` | `Job.Input` の Reference/Source/Endpoint |
| routing の `Input io.ReadSeeker` | prepared Access view または typed Endpoint port |
| format factory 内 `io.ReadSeeker` assertion | Compile 時 capability requirement |
| resolver の候補ごとの `Seek(0)` | shared immutable bounded `probe.View` |
| CLI の `os.Open` | file Provider または direct owned Source |
| CLI `pendingOutput` | file Sink transaction |
| web temporary input/output | application upload store + Access handles |
| WASM `[]byte`/`bytes.Buffer` | chunk/stream memory Access adaptor |
| `BuildPlayback`/`PlaybackSink` | 通常 Job + typed Endpoint component |
| Oto controller | optional Oto Endpoint + surface control handle |

## testkit

Access Provider conformance:

- reference parse/redaction
- scheme conflict/override
- capability declaration と実 view の一致
- cancel/Close unblock
- snapshot consistency
- retry の byte exactness
- range concurrency
- size/EOF semantics
- sink commit/abort/idempotent Close
- failure injection at every transaction phase
- secret が Plan/error/trace に出ない

Endpoint conformance:

- typed port/schema
- clock/timestamp monotonicity
- overflow/underrun policy
- cancel/join
- hotplug/reconnect/discontinuity
- exclusive resource rollback
- topology event
- observation off overhead

Host integration:

- non-seekable WAVE/MP3 path
- random access requirement + spool
- output patch requirement + fragmented/spool choice
- dry-run が output を作らない
- multi-output partial commit report
- source changed after probe
- application-supplied Provider wrapper が宣言 capability を保つ

## M3 完了条件

M3 は Access と Endpoint の contract を foundation package として新設する milestone である。output transaction の実行は M5、file Provider/session の acquire、probe、inspect と spool の実挿入は M6、HTTP/S3 と device の実装は需要に応じた M6 以降/M9 の担当であり、M3 には要求しない。media 側の条件は [media](media.md#m3-完了条件) を参照する。

- `access.Reference`、`access.Provider`、byte `Source`/`Sink` capability、transaction が別々の型として存在し、一つの汎用 protocol interface に潰れていない。
- source capability が巨大な boolean struct ではなく、sequential read、position-independent random read、stable size の小さな contract の組み合わせで表現される。remote Provider 固有の reopen、concurrent range read、blocked I/O cancellation は実 consumer と同じ milestone で追加する。
- Format が capability alternative を宣言でき、宣言した requirement を満たす narrow view だけを受け取る形になっている。不可能な operation を nil field として渡し plugin 内で type assertion させる余地がない。
- `Own`/`Borrow` で ownership が明示され、Host が閉じるかどうかが決まる。blocked I/O cancellation は保証できる Provider が現れるまで宣言しない。
- Probe が immutable な bounded view を受け取り、source cursor を変更しない。候補ごとの独立 reader または immutable range を使い、byte budget と追加 range request を表現できる。
- sink が `io.Writer` ではなく transaction として表現され、Provider が transaction class（atomic replace、staged commit、rollbackable、append-only、live/no-commit）を宣言できる。
- spool が隠れた実装詳細ではなく、予測 bytes、memory/disk 使用、開始 latency、rollback class を伴う明示的な要素として表現できる。
- Endpoint が閉じた `Device` role registry ではなく通常の typed component として表現され、`FiniteStatic`/`LiveStatic`/`LiveDynamic` と realtime/offline の区別を宣言できる。
- Device が component identity、device reference、device descriptor を分けて表現でき、Host 構築や package import が device scan、permission prompt、network access を起こさない。
- snapshot identity が同じ content を指す保証を型で表現でき、strong snapshot を持たない source がその事実を descriptor に残せる。retry/reopen は remote Provider の実操作と同時に追加する。
- credential が config schema の secret field または application 所有の handle として扱われ、[C16](decisions.md) のとおり path/scheme/CIDR 等の権限 DSL や Job ごとの authority engine を foundation に持たない。
- foundation package が filesystem、network、device の具体実装を import しない。
- 上記を unit/property test で検査する。第三者相当の Provider/Endpoint fixture を含め、公式 plugin と OS/network 依存を持ち込まない。

M3 では次を未完了事項として残す。M4 は Provider/Endpoint declaration の binding と宣言 capability の診断、M5 は transaction の実行と rollback を扱う。prepared job の acquire/probe/inspect、実 capability の再検証、spool insertion、file Provider は WAVE が consumer になる M6、device Endpoint は M9、完成した conformance testkit は M10 が扱う。

capability alternative は M6 の WAVE が最初の consumer になる（`data` chunk size の後追い patch が random-write と sequential の選択を要求する）。設計上の代表例である「random-write OR sequential + fragmented-mode」は M7 の MP4 が moov/mdat 順序として実際に通す。ここで spool 挿入の判断と Plan への表示も初めて実データで検証される。

**consumer を持たない contract は M3 で凍結しない。** clock domain と timestamp origin、latency/buffer range、block/drop/duplicate/conceal policy、reconnect/discontinuity semantics、exclusive/shared resource、hotplug event、multi-output の `AllOrNothing` 共同 transaction は、この文書に設計として記述するが、実装する Endpoint も Provider も現時点の roadmap に存在しない。M3 では最小の型だけを置き、詳細は実際の Endpoint を作る milestone で決める。現行 `MediaAttributes` が「使われないまま形だけ先に決めて後で作り直す」失敗をした構造を繰り返さないためである。

## M6 完了条件

M6 は Access contract が最初の実 I/O consumer を得る milestone である。file Provider、prepared session、共有 probe、Inspect、spool、transactional file output を WAVE/PCM 経路と一体で実装する。media 側の条件と作業単位は [media](media.md#m6-完了条件) を正本とする。

対象は local filesystem だけとする。remote Provider、device/session Endpoint、realtime clock は M6 の条件に含めない。

- **Provider を component の trait として宣言する。** `access` が `plugin.ComponentOption` を提供し、source trait は 0-in/1-out の component、sink trait は 1-in/0-out の component に付く。scheme、capability、transaction class、acquire 操作は trait が持つ。M5 時点の `access.Provider` は component identity を一つしか持たず、[Access Provider](#access-provider) の binding が input boundary へ 0-in/1-out、output boundary へ 1-in/0-out を要求するため同じ scheme で両方向を提供できなかった。trait では方向が付き先の component で決まるので、この不整合が構造的に起きない。合成側の条件は [plugins](plugins.md#m6-完了条件) を正本とする。
- **`ProviderRole`、Provider manifest、`endpoint.Component`、`host.Providers`/`host.Endpoints` を削除する。** 方向は trait の付き先から導出し、component と宣言の対応は trait であることから自明になる。M5 時点で必要だった「Provider component が catalog に無い」「Endpoint manifest が catalog component を記述していない」という検査は表現不能になるため消す。互換のための経路を残さない。
- **acquire を trait の操作にする。** session を開くのは component の `Open` ではなく Prepare の段階なので、acquire は宣言ではなく trait が持つ操作にする。component は取得済みの narrow view を `plugin.OpenServices` の boundary として受け取るだけで、自分では開かない。
- Provider 自身の `Requirements` を削除する。要求元は byte を消費する Format/component であり、Provider が自分の要求を自分の capability に対して解決する現在の形は意味を持たない。
- **capability 要求は Format の方向別 trait が持つ。** `media/format` が read/write の `plugin.ComponentOption` を提供し、boundary に隣接する component へ Format identity と方向別の capability alternative を付ける。方向で要求が異なる（raw PCM の読みは逐次または位置指定+既知 size、書きは逐次で足りる）ため、方向中立な単一 alternative 列では表せない。alternative を `format.Format` から trait へ移し、`Format` は identity、carrier、packetized だけを持つ宣言に戻す。詳細と Probe/Inspect の担当は [scope](scope.md#m6-の-contract-分類) を正本とする。
- scheme の衝突検出を `internal/catalog` が trait の走査で行う。Provider が自分で declaration を作る経路は不要になるので削除し、検出は codec/metadata Binding と同じ composition 診断の channel に残す。
- `plugin/file` が source component と sink component を持ち、それぞれに trait を付ける。foundation package が filesystem 実装を import しない状態を維持する。
- **canonical byte-stream schema を `access` が所有する。** Access boundary を跨ぐ typed item は `access.Bytes()` 一つとし、Provider component と Format component が同じ port schema を使う。M5 時点の schema は `plugin/pcm/linear` 固有 identity であり、そのままでは `plugin/file` が linear PCM を import することになって公式 plugin 間の直接依存禁止に反する。所有者を `access` にするのは、消費側の二者がどちらも既に `access` を import しているためである。`media/format` に置くと filesystem provider が media format を import することになる。
- **byte item の payload は `media/buffer.Handle` とする。** `Fork` は `Share`、`Drop` は `Release`、`Size` は layout size を返す。timestamp を持たないため `Time` trait は宣言しない。`[]byte` を維持すると、`Reader` が read ごとに所有権を渡す以上 buffer を再利用できず、Access boundary の payload だけが Job grant の外の GC allocation になる。`packet.Chunk`、`packet.Packet`、`audio.Frame` がすべて `buffer.Handle` を包んでいる中で boundary だけを例外にしない。`access` は `media/schema` と `media/buffer` を import するが、どちらも `internal/marker` と stdlib しか使わないため循環しない。
- **byte schema の descriptor は media 意味を持たない。** carrier descriptor が運ぶのは stream id と metadata までとし、sample properties や media の time base を載せない。filesystem provider は media format を知らないのでそれらを設定できず、planner にも下流から source へ descriptor を逆伝播する仕組みは無い。この規則は source 側と sink 側の両方に適用する。byte を消費する component（raw PCM reader、WAVE demux）は入力 descriptor に media 意味を要求せず、自分の config または container header から**出力**の properties と time base を確定する。byte を produce する component（raw PCM writer、WAVE mux）も同様に carrier descriptor を出す。
- **carrier descriptor の time base は canonical な placeholder とする。** `stream.NewDescriptor` は有効な time base を必須とするが、`access.Bytes()` は `Time` trait を持たないため byte edge の time base を消費する経路は存在しない（`Limit.Time` は schema が `Time` trait を提供する edge にだけ設定される）。値を再導出させないよう `access` が名前付きで公開し、byte schema と同じ場所で carrier 契約を完結させる。placeholder であることを godoc に明記し、timeline を持たない stream を型で表す正直な形は [scope](scope.md#m6-の-contract-分類) のとおり M7 が担当する。
- **sink 側の item は書き込み位置を表現できる。** 読み側の item は順序どおりの byte 列でよいが、書き側は「末尾へ追加する」と「絶対 offset を patch する」を区別できなければならない。sink 専用の canonical schema を置き、payload は引き続き `buffer.Handle` とする。読み側の item に意味のない append 印を持たせない。M5/M6-1 の file sink は全 handle を自前追跡の offset へ順次書いており、`RandomWrite` を選んでも「現在位置への位置指定書き」にしかならない。
- **mux は自分で I/O せず、位置を item として下流へ渡す。** boundary の narrow view は Provider node にだけ渡り、隣接する Format component には渡らない。これは意図した境界であり、mux が直接 sink へ書くと graph、queue、ownership、transaction をすべて迂回して二つの writer が同じ対象を書くことになる。したがって header patch は `Finalize` の後に `Flush` から位置付き item として emit し、sink が `Appender`/`Patcher` へ適用する。`Finalize` に emitter を持たせない設計は変えない。
- **boundary component が port schema を満たすことを composition で検証する。** `access.Source` の出力 port と Format read trait の入力 port が読み側 schema、`access.Sink` の入力 port と Format write trait の出力 port が書き側 schema であることを `internal/catalog` の trait 走査が検査する。Provider と Format が実際に接続可能であることを Host 構築時に保証し、graph 検証まで持ち越さない。
- **write 側の capability 語彙と narrow view を新設する。** M5 時点の `access` は read 側 6 capability と `Sequential`/`Random` view しか持たない。sink の逐次書きと位置指定書きを別 capability として宣言でき、component は宣言した view だけを受け取る。あわせて `internal/bind` の出力 boundary が入力側と同じ経路で capability を選択する。M5 時点の `bindOutput` は選択を行わず空の `Selection` を渡している。
- **Prepare が session を acquire し、宣言 capability ではなく実 session の capability を検証する。** manifest が宣言した capability を実際に開けなかった場合は、Open 後の type assertion ではなく Prepare の構造化 diagnostic になる。
- **binding は二段階にする。** M6-2 までの capability 選択は Normalize の時点で隣接する明示済み Format trait の要求を読むが、自動判別では Format が Probe の後にしか決まらない。一方 acquire は選択済み capability を要求するため、そのままでは probe 用 session を取得できない。したがって Bind では Provider と boundary だけを確定し、probe 用 session を取得し、Probe の後に Format を選び、実 session の capability と選択 Format の要求から最終的な Opening を作る。capability 不足はこの時点で構造化 diagnostic にする。
- **probe 用 session は位置指定読みを優先し、そのまま run session になる。** 位置指定読みがあれば cursor を進めずに読めるので replay が不要になる。逐次読みしか無い source でだけ prefix を消費し、消費分を replay する。M6 は session を捨てて取り直す contract を持たず、再取得 semantics は remote Provider の実装時に決める。
- **prefix replay buffer は owner と grant を持つ。** 上限は probe budget で決まるが、確保元を決めずに置かない。[M6-1](scope.md#m6-の-contract-分類) で「boundary の payload だけが Job grant の外」という穴を塞いだので、replay buffer で同じ穴を開けない。
- **候補間で bounded probe を共有する。** 複数の Format 候補が同じ入力を判定しても byte source を読み直さず、候補ごとの `Seek(0)` を繰り返さない（[F7](findings.md)）。probe の上限 byte 数と cancel が policy から決まり、非 seekable 入力でも成立する。
- **Probe の実行 contract を持つ。** Probe は複数の immutable な view を受け、terminal result か追加 range 要求を返す反復 protocol とする。Host が range cache と全候補共通の budget を所有し、同一要求を重複排除する。逐次 source への要求は単調な prefix 拡張に限り、実際に読み進めた全 byte を budget に算入する。重複要求、空要求、budget 超過、進捗しない反復は構造化 diagnostic にする。
- **probe の上限は planning budget に置く。** byte 上限と反復回数上限を `job.Budget` が持つ。probe は planning 段階の作業であり、`Budget` は既に compile 数と探索回数と duration を持ち、[planner](planner.md#m4-完了条件) が budget exhaustion を unsupported と区別して最も近い unmet Need と制限値を diagnostic に含めると定めている。`ResourcePolicy` は runtime grant の場所なので使わない。byte 上限だけでは、1 byte ずつ要求する実装が budget 内で無限に往復できるため、反復回数上限を同時に持つ。
- 選択された Format の Inspect が stream、time base、metadata carrier を読み、その結果が `Compile` の入力 descriptor になる。probe/inspect と実行が同じ session と snapshot を使う。
- **Prepare の段階順が [planner pipeline](runtime.md#planner-pipeline) と一致する。** Bind → Acquire → Probe → Inspect → Shape → Compile → Solve → Validate → Optimize → Describe → Build の順を明示し、各 component の `Compile` は pure のままである。`Host.Plan` の dry-run が output を作成も truncate もしない。**M5/M6-1 の実装は acquire を Compile より後に置いており、この順序に反している。** container header からしか得られない事実で descriptor を確定するには Inspect が Compile より前に走る必要があり、M6-2a がこの再構成を担当する。`Host.Plan` の read-only 経路と resource 予約の順序も同時に見直す。
- **spool は capability を変換する Host-owned adapter である。** 逐次書きのみの sink を実効的な位置指定書きへ変換する。`SequentialWrite` そのものを「patch 可能な代替」として扱わない。sink boundary を spool 付きへ差し替える形は変えず、graph node は増やさない。`plan.Boundary` には**元の capability、適応後の capability、`SpoolSpec` を分けて**記録し、「何が足りず、何で埋め、何を代償にしたか」が Plan から読めるようにする。runtime に第二の実行モードを持ち込まない。
- **spool の storage は Host が所有し、上限を予約ではなく quota で表す。** `job.ResourcePolicy` に spool 専用の上限（最大 bytes と storage 種別）を持たせ、Host が Job 単位で spool を所有する。`resource.Request`/`Grant` の予約次元へは戻さない。spool を使う理由が「最終 size が確定しないこと」であり、Open 前に確定量を予約する `memory.Manager` の model と一致しないためである。上限検査は spool-local counter で行い、中央 manager を item ごとに呼ばない。cancel、rollback、Job 終了で必ず削除する。
- spool を Host 内部に閉じ、`plugin.OpenServices` へ temporary service を公開しない。M6 の唯一の consumer が sink boundary の decorator であり、公開すると consumer を持たない plugin API を凍結することになる。第二の consumer が現れた milestone で共通 service へ昇格させるかを決める。
- **transactional file output が実装される。** 同じ filesystem 上の temporary file へ書き、Finalize → Flush → Sync → PrepareCommit → Commit が成功した後に replace する。replace は `os.Rename` とする。Windows でも `MoveFileEx` の `MOVEFILE_REPLACE_EXISTING` に写るため既存 target を置換でき、外部 dependency を増やさない。`ReplaceFile` による ACL/attribute の継承は行わず、その差分を [capability](capability.md#挙動変更の記録) の B5 として記録する。失敗と cancel では元 target を残す。non-seekable/stdout sink で rollback できないことを `Plan` に示す。
- 引き継いだ宣言が consumer を得る。`SpoolSpec`、`SpoolStorage`、`job.ResourcePolicy.AllowSpool`、Source/Sink capability の `Own`/`Borrow`、`ProbeView`、`RangeRequest` のうち M6 が使うものを示し、使わないものは担当 milestone とともに [scope](scope.md#m6-の-contract-分類) へ残す。
- 上記を unit/property test、`integration` の end-to-end test、[quality](quality.md#m6-完了条件) の Access Provider testkit で検査する。cancel、部分書き込み、commit 失敗、spool 中断で temporary file と spool storage が残らないことを含める。

M6 では次を未完了事項として残す。HTTP/S3 等の remote Provider、device/session Endpoint、realtime clock、multi-output の `AllOrNothing` 共同 transaction、live topology event の既定 policy は、需要が現れた milestone と M9 で扱う。

## 文書全体の完了条件

この節は Access/Endpoint contract の最終状態を示す gate であり、個別 milestone の完了判定には各 milestone 固有の条件を用いる。M3 の判定には上記「M3 完了条件」だけを使う。

- 第三者が core/surface を変更せず新しい object Provider を Set に追加できる。
- 第三者が core/surface を変更せず typed session/device Endpoint を追加できる。
- seek/read-at/size 不足が Open 後の type assertion ではなく Compile diagnostic になる。
- Probe が source cursor を変更せず、全候補で byte budget と cache を共有する。
- non-seekable input を対応 Format が spool なしで処理できる。
- mux Finalize、sink flush/sync、commit/abort が一つの failure-safe lifecycle になる。
- direct reader/writer 利用者は ownership を明示でき、Provider plugin を作らなくてよい。
- dry-run は output 作成・truncate・commit を起こさない。
- device/RTSP/HLS 等を seekable byte stream と偽装しない。
- access/device の metrics と resource tracking が hot path の中央 lock/atomic を要求しない。
- official pure-Go base が optional network/device/native dependency を含まず build できる。
