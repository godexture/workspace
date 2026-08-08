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
| `access.Provider` | reference → source/sink plan/session | credential、network、range、retry、transaction | container/codec semantics |
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

Provider は plugin marker、descriptor、config schema、provenance を他の component と共有するが、media transform node ではない。reference を job session resource へ解決する control-plane extension である。

Provider は概念上、次を宣言する。

- 対応する reference kind/scheme
- source、sink、または両方
- config schema と secret field
- 静的に分かる capability alternatives
- read-only inspection の effect
- open/close/cancel semantics
- retry と snapshot consistency
- sink transaction class

scheme の重複は host 構築時に error とする。利用者が意図して置き換える場合だけ、対象 provider identity を指定する `Set.Override` を使う。暗黙の last-wins や registration order は使わない。

「Protocol」は catalog 上の capability/説明名として使えるが、public Go interface は `access.Provider` の方が適切である。local file、memory、object store は必ずしも通信 protocol ではないためである。

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

代表的な capability:

- sequential read
- position-independent random read
- stable known size
- reopen/clone
- finite、growing、live
- stable snapshot identity
- concurrent range read
- cancellation/close-unblocks-read

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
  sequential
  OR random + stable size

MP4 output:
  random-write
  OR sequential + fragmented-mode
```

planner は format mode、source/sink capability、policy をまとめて選ぶ。capability 不足を runtime panic/type assertion error にしない。

## cancellation と ownership

`io.Reader`/`io.Writer` adaptor は ownership を曖昧にしない。

- `access.Own(x)`: Host が session 終了時に閉じる。
- `access.Borrow(x)`: Host は閉じない。blocked I/O を cancel で解除できない可能性を capability に出す。
- `access.Factory(f)`: Prepare ごとに新しい owned session を作れる。

Close が blocked read/write を解除できる Provider はその保証を contract に含める。単なる `io.Reader` を context-aware と偽らない。non-cooperative borrowed handle は timeout 後も goroutine が残り得るため、Plan warning または policy rejection の対象にする。

input source の cursor を probe、inspect、run が暗黙共有しない。host が prefix replay、independent range view、reopen のいずれかを選び、chosen Format には定義済みの開始位置から渡す。

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

## package/distribution 案

[C13](decisions.md) の monorepo方針に従う概念配置:

```text
access/              Reference, Provider, Source/Sink capability, transaction
endpoint/            clock, realtime, topology and endpoint traits
job/                 typed Input/Output choices and policy
internal/probe/      bounded shared probe cache
internal/access/     provider binding, session and spool
internal/commit/     multi-output coordination

plugin/file/         local file Provider
plugin/http/         optional HTTP Provider
plugin/memory/       library/test adaptors if public value isある場合
plugin/oto/          optional playback Endpoint
plugin/<camera>/     optional capture Endpoint
```

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

M3 は Access と Endpoint の contract を foundation package として新設する milestone である。具体的な file/HTTP/S3/device 実装、prepared job の実行、spool の実挿入は M5/M6/M9 の担当であり、M3 には要求しない。media 側の条件は [media](media.md#m3-完了条件) を参照する。

- `access.Reference`、`access.Provider`、byte `Source`/`Sink` capability、transaction が別々の型として存在し、一つの汎用 protocol interface に潰れていない。
- source capability が巨大な boolean struct ではなく、sequential read、position-independent random read、stable size、reopen、snapshot identity、concurrent range read、cancel 等の小さな contract の組み合わせで表現される。
- Format が capability alternative を宣言でき、宣言した requirement を満たす narrow view だけを受け取る形になっている。不可能な operation を nil field として渡し plugin 内で type assertion させる余地がない。
- `Own`/`Borrow`/`Factory` で ownership が明示され、Host が閉じるかどうかと、blocked I/O を cancel で解除できるかが capability に現れる。
- Probe が immutable な bounded view を受け取り、source cursor を変更しない。候補ごとの独立 reader または immutable range を使い、byte budget と追加 range request を表現できる。
- sink が `io.Writer` ではなく transaction として表現され、Provider が transaction class（atomic replace、staged commit、rollbackable、append-only、live/no-commit）を宣言できる。
- spool が隠れた実装詳細ではなく、予測 bytes、memory/disk 使用、開始 latency、rollback class を伴う明示的な要素として表現できる。
- Endpoint が閉じた `Device` role registry ではなく通常の typed component として表現され、`FiniteStatic`/`LiveStatic`/`LiveDynamic` と realtime/offline の区別を宣言できる。
- Device が component identity、device reference、device descriptor を分けて表現でき、Host 構築や package import が device scan、permission prompt、network access を起こさない。
- snapshot identity、retry、reopen が同じ content を指す保証を型で表現でき、strong snapshot を持たない source がその事実を descriptor に残せる。
- credential が config schema の secret field または application 所有の handle として扱われ、[C16](decisions.md) のとおり path/scheme/CIDR 等の権限 DSL や Job ごとの authority engine を foundation に持たない。
- foundation package が filesystem、network、device の具体実装を import しない。
- 上記を unit/property test で検査する。第三者相当の Provider/Endpoint fixture を含め、公式 plugin と OS/network 依存を持ち込まない。

M3 では次を未完了事項として残す。prepared job の acquire/probe/inspect 実行順は M4、transaction の実行と rollback は M5、file/HTTP Provider と device Endpoint の実装は M6/M9、conformance testkit は M10 で扱う。

capability alternative は M6 の WAVE が最初の consumer になる（`data` chunk size の後追い patch が random-write と sequential の選択を要求する）。設計上の代表例である「random-write OR sequential + fragmented-mode」は M7 の MP4 が moov/mdat 順序として実際に通す。ここで spool 挿入の判断と Plan への表示も初めて実データで検証される。

**consumer を持たない contract は M3 で凍結しない。** clock domain と timestamp origin、latency/buffer range、block/drop/duplicate/conceal policy、reconnect/discontinuity semantics、exclusive/shared resource、hotplug event、multi-output の `AllOrNothing` 共同 transaction は、この文書に設計として記述するが、実装する Endpoint も Provider も現時点の roadmap に存在しない。M3 では最小の型だけを置き、詳細は実際の Endpoint を作る milestone で決める。現行 `MediaAttributes` が「使われないまま形だけ先に決めて後で作り直す」失敗をした構造を繰り返さないためである。

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
