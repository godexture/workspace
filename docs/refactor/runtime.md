# runtime と lifecycle

この文書は graph 実行、ownership、resource、cancel、Finalize、error、observability を定義する。候補探索と自動挿入は [planner](planner.md)、数値・再現性 policy は [performance](performance.md) を正本とする。

## routing の重複をなくす

「plan 用の変換」と「実行開始時の変換」を別々に実装すると drift する。一方、factory を候補ごとに起動して結果を調べる方式では、探索が副作用、I/O、resource 消費、Close の正しさに依存する。

解決策は、変換処理を `Compile` に一度だけ実装し、その結果を `Open` が消費することにある。

```go
func Compile(ctx CompileContext, cfg Config, in Inputs) (
    plan ComponentPlan,
    out Outputs,
    err error,
)

func Open(ctx OpenContext, plan ComponentPlan) (flow.Operator, error)
```

- `Compile` は純粋で、候補探索中に何度呼んでもよい。
- `Compile` が出力 descriptor、requirements、cost、latency、resource request をすべて返す。
- `Open` は変換規則を再計算せず、選ばれた `ComponentPlan` を実行 object にする。
- `Run` は compile 済み port と function を使い、routing logic を持たない。

これにより Transform/Start の重複と、候補 Factory の試し起動を同時に解消できる。

## planner pipeline

planner は次の段階を明示する。

1. **Normalize Job**: default と selector を解決し、曖昧な mapping を diagnostic にする。
2. **Bind Access/Endpoints**: Reference と endpoint request を Provider/component definition と policy へ純粋に binding する。
3. **Prepare Inputs**: byte source session を acquire し、device/session endpoint capability を read-only inspect する。
4. **Probe Sources**: bounded shared cached view で format 候補を採点する。
5. **Inspect Topology**: 選択 format/endpoint が stream、program、metadata carrier を読み取る。
6. **Shape Graph**: demux/mux/mixer 等の動的 port を確定する。
7. **Compile Components**: 各 component の semantic output と requirement を得る。
8. **Solve Gaps**: decoder、encoder、parser、converter、resampler、mapping、spool 等の bridge を挿入する。
9. **Validate Graph**: 型、multiplicity、接続、cycle、reachability、finalization を検証する。
10. **Optimize**: stream copy、component elimination、fusion、queue placement、batching を行う。
11. **Describe Plan**: 選択理由、loss、policy、resource、cost を public `Plan` に固定する。
12. **Build Program**: dense index と typed call path を持つ private `Program` を生成する。

solver の tie-break は deterministic にする。catalog identity、component priority、cost vector、canonical config の順序を固定し、map iteration order に依存させない。

bridge の自動挿入範囲、Effect と Host policy、探索 budget、default copy 優先の詳細は [planner の探索規則](planner.md) に定義する。

Prepare 全体は input I/O を含むが、各 component の `Compile` は pure のままである。prepared session が Plan と input snapshot を所有し、Run まで同じ session を使う。output transaction は dry-run/Plan 時に開始しない。Access/Endpoint の詳細は [access と endpoint contract](access.md) に定義する。

## graph model

Node の固定 role を列挙するのではなく、typed input/output port と phase behavior で表す。

port は少なくとも次を宣言する。

- schema identity
- required/optional
- multiplicity: one、optional、many
- fan-in policy
- ordering/timing requirement
- dynamic shape の可否

compile 時に以下をすべて拒否する。

- schema 不一致
- required port 未接続
- one port への重複接続
- 不正な fan-in/fan-out
- 許可されない cycle
- source から到達不能な node
- sink へ到達しない出力
- duplicate endpoint/stream mapping
- finalizer を必要とする経路の欠落
- time base を解決できない接続

cycle を将来 filter feedback 等で許可する場合も、delay/queue semantics を明示した component だけに限定する。一般 graph の accidental cycle は error とする。

## Plan と Open transaction

候補 component の `Open` は行わない。input Access session は probe/inspect のため Prepare 中に取得済みだが、media operator、live Endpoint、output transaction は最終 `Program` が確定した後に依存順で一度だけ開く。

```text
begin output transactions
  -> open source/sink Endpoints
  -> open operators
  -> start host tasks
```

各 step を scope に登録し、失敗時は逆順に rollback する。temporary output は commit 前に公開しない。Open の途中で作った goroutine は host task group に属し、rollback で cancel/join する。

成功時は codec/format/Endpoint の Finalize、sink Flush/Sync、全 output の PrepareCommit、Commit の順とする。いずれかが失敗したら未 commit sink を Abort し、既に commit 済みの output と outcome unknown を構造化 result に残す。

`Close` の error は最初の error だけに潰さず、primary failure と cleanup failures を構造化して返す。

## execution island

node ごとに goroutine と buffered channel を必須にしない。同期的な一入力一出力 Processor の linear chain は一つの execution island に fuse する。

queue を置くのは次の境界だけである。

- source I/O と CPU work を並行させる
- parallelism を持つ codec/filter
- fan-out/fan-in
- latency または rate が大きく異なる component
- explicit buffer/delay
- sink I/O

最適化前:

```text
demux -> parse -> decode -> convert -> gain -> encode -> mux
```

最適化後の一例:

```text
[demux] => queue => [parse + decode + convert + gain + encode] => queue => [mux]
```

island 内は direct typed call とし、edge ごとの interface dispatch を compile 時に可能な限り消す。必要に応じて specialized SPSC/MPSC queue を内部実装として選ぶが、plugin contract に channel を露出しない。

## ownership

ownership は API の慣習でなく contract として固定する。

- Reader が item を返した時点で consumer に ownership が移る。
- Writer が成功した時点で writer に ownership が移る。
- Writer が失敗した場合に誰が所有するかを API 全体で統一する。推奨は「呼び出し元が保持」である。
- linear path は所有権を move し、refcount を増やさない。
- fan-out でのみ `Fork`/retain を行う。
- drop/cancel/queue drain は owner が `Drop` を呼ぶ。
- mutable access は exclusive owner のみ。shared item を変更する場合は copy-on-write。
- plugin が call を越えて保持する時だけ `Share` を明示する。

`Processor` は call 中に `Input` を借用し、成功を返す直前にだけ consume する。失敗時は caller が保持するため、runtime が drop できる。低水準 `Operator` には ownership rule を conformance test する。

fan-out が一つなら refcount atomic を通らない設計にする。複数 consumer の時も、一 item につき必要最小限の retained handle だけを作る。

## state の所有者

mutable state は、必要な最小 scope の owner に置く。

```text
process
└─ Host
   └─ Job
      └─ component instance / worker
         └─ item lease
```

- plugin catalog、CPU feature、implementation policy は Host construction 時に snapshot 化する。
- source/sink transaction、queue、temporary storage、memory budget は Job が所有する。
- parser、codec、filter のstream stateとscratchはcomponent instanceまたはgranted worker slotが所有する。
- packet/frame payloadは一時的なleaseとしてowner間をmoveする。

package-level mutable stateを暗黙のownerにしない。global registry/factory、WASM job map、書換可能なCPU feature、process-wide runtime pool、mutable default/config/descriptorは削除する。testがglobal function/feature flagを書き換える方式も使わず、Host snapshot、constructor dependency、明示variantを注入する。

ただし、package-level dataを一律にheap objectへ移すわけではない。次は許容する。

- scalar/string `const`
- unexportedで生成後に変更せず、mutable backingを外へ返さない固定array/lookup table
- 標準的なsentinel error
- 一度closeしたread-only notification channel等、型の操作で状態を後戻りさせられない共有値
- private fieldだけを持ち、read APIがcopyを返すことでimmutabilityを強制するinterned descriptor

codecのCRC、Huffman、G.711、window coefficient等の固定tableはhot pathのためpackageに一つだけ保持し、呼出しごとにcloneしない。slice/mapを公開したり、testから書き換えたりしない。配列を`const`にできないというGoの制約だけを理由にruntime allocationへ置換しない。

現在の`Default*Config`、channel-layout preset、`SupportedFormats`のようなexported variableは、fresh valueを返すfunctionまたはimmutable schema/descriptorへ置換する。特にslice/map/functionを含むstructの単純代入はsnapshotではないため、control planeで一度deep copyする。固定capabilityの列挙はCompile/Catalog時にcopyしてよく、item loopでは参照しない。

### allocator と pool

`sync.Pool`はGCが任意に内容を捨てるcacheであり、上限、retention、tenant、zeroingを表現できない。したがってresource manager、ownership contract、correctnessの根拠にはしない。

- payload allocatorはHost/Jobのgrantに属し、予約capacityをqueue・block・worker slot単位でaccountする。
- codec workspaceはsequential instanceに一つ、parallel pathではgranted concurrency分をOpen時に用意する。worker contextまたはbounded instance cacheで再利用し、process-wide poolへ逃がさない。
- cache可能な最大capacityと総retained bytesをgrantに含め、Job終了時にreleaseする。process cacheを設ける場合もHostが明示所有し、上限とisolation domainを持つ。
- central trackerをpacket/frameごとに呼ばない。edge/local allocatorがgrant内のcounterを更新し、観測時だけ集約する。

一般向けallocationはzeroed memoryを返す。全域上書きするofficial hot pathにはprivateなwrite leaseを提供できる。

```text
lease := allocator.Overwrite(size)
writer writes exactly size bytes
item := lease.Commit(size)
```

`Overwrite`のmemoryはCommit前にread/publicationできず、全域の初期化を確認できない経路では使わない。error/cancel時はleaseを破棄する。これにより大きなaudio/video bufferを毎回zero clearするcostを避けながら、以前のJobのbyteが部分writeを通じて露出することを防ぐ。異なるtrust/isolation domain間でcacheを共有するHostは、release時zeroingまたはpool分離をpolicyとして強制する。

## queue と backpressure

queue policy は一つの「resource tracker」に全 item を報告させず、edge ごとに局所管理する。

```go
type Limit struct {
    Items int
    Bytes int64
    Time  timing.Duration
}
```

schema が安価な `Size` trait を提供する場合だけ byte limit を使う。timestamp のある stream は time window を使える。どれも指定されなければ item count の固定 queue を使う。

resource manager は Open 時に codec workspace、worker 数、大きな ring、temporary storage 等の粗粒度 grant を与える。packet/frame ごとの acquire/release を中央 manager に送らない。

局所 counter は cache line を意識し、metrics export 時に集約する。resource tracking を無効にした経路では追加 atomic を発生させない。

## host service

component に渡す service は用途別の narrow interface にする。

- `Buffers`: allocator、blob、workspace grant
- `Tasks`: cancel と join が追跡される task group
- `Temp`: host 管理の temporary storage
- `Diagnostics`: structured event の sink
- `Clock`: realtime component 用 clock
- source/sink I/O: component の宣言に合う capability view

全 service や catalog を自由に取得できる `Host`/service locator は渡さない。component が別 component を runtime に検索・生成することも許可しない。依存 component は planner が graph に表す。

頻繁な buffer alloc 等は interface call を item ごとに繰り返さず、Open 時に得た allocator/function を operator に保持する。

## cancel、EOF、finalization

### cancel

- job context cancel は source、queue、operator、sink、host task group に伝播する。
- queue の block 中 read/write は cancel で解除される。
- cancel で残った item は queue owner が drain/drop する。
- edge close は idempotent にする。
- send-after-close/double-close を plugin の責任にしない。
- non-cooperative in-process code を強制停止できない事実は timeout API と分けて説明する。

host は timeout 後に「停止した」と偽らず、join できない task を diagnostic にする。強制終了が必要なら別 process boundary が必要である。

### EOF

通常 EOF は data sentinel ではなく edge close で表す。decoder flush は input close を受けて `Flush` を呼ぶ。動的な stream add/remove は専用 control event schema とする。

### Finalize

- encoder delayed packet の排出
- final codec parameter/statistics
- muxer index/footer/header patch
- metadata flush
- output fsync/atomic commit

を明示した依存順で行う。`Close` に成功処理を押し込まない。

## multi-input と ordering

現在の複数 input から先に届いた値を処理する方式は scheduler timing に結果が左右される。mixer、sidechain、subtitle overlay、A/V sync では欠落、ずれ、早い EOF の原因になる。

各 multi-input component は fan-in policy を宣言する。

- timestamp join
- zip/lockstep
- latest side input
- primary-driven
- unordered merge
- windowed aggregation

host は policy に必要な buffering と watermark を plan に含める。「deterministic」は単に毎回同じ順序という意味ではなく、media semantics を保つ ordering rule が明示されていることを意味する。

## execution policy

`Fast`、`Stable`、`Portable`、`Realtime` は Run が分岐する mode ではない。Host が Compile 前に policy vector へ展開し、variant、worker、block/partition、seed を Program に固定する。Run の item loop は preset、CPU feature、catalog を参照しない。

offline の既定は [C15](decisions.md) の `Fast + Repeatable + ArtifactNone` とする。全 preset で timestamp/order、欠落・重複なし、lossless semantics、validation を維持する。正確な policy vector、variant contract、Stable/Portable domain、differential test は [performance](performance.md) を参照する。

## observability

metric、progress、trace は同じ event model から集約するが、data path へ常時 map/atomic を挿入しない。

- observation off: hot path に追加 counter を置かない。
- basic: island/edge ごとの local counter を batch 集約する。
- detailed: timestamp/size sample を明示 opt-in で取得する。
- trace: sampling と bounded buffer を必須にする。

metric name は component display alias でなく stable plan node ID を使う。外部 surface は snapshot DTO を受け取り、live mutable runtime object を読まない。

## panic と error

panic recovery は execution island/task の最上位に一度だけ置き、次を記録する。

- plugin/component identity
- plan node ID
- phase
- stack
- primary/cancel status

error は sentinel 文字列ではなく phase-aware structured error とする。plugin error、invalid input、unsupported mapping、resource exhausted、cancel、host bug を分類し、CLI/WASM/HTTP が同じ意味を別表現に変換できるようにする。

### error を捨てない

現在は `Seek`、metadata number parse、DSP conversion、`Close`、temporary file remove、server shutdown、`rand.Read` 等の error を `_ =` で捨てる経路がある。すべてを同じ扱いにはせず、失敗の意味で分類する。

- input/parse/seek/write/commit error: primary failure として処理を停止する。
- Close/Abort/remove/shutdown error: primary failure を上書きせず cleanup failure として集約する。成功処理中なら job failure になり得る。
- output/progress renderer error: broken pipe、client disconnect、terminal failure 等の surface policy に従い、必要なら job cancel へ接続する。
- optional metadata/property parse error: invalid と absent を区別し、raw preservation、warning、strict failure のいずれかにする。黙って zero value にしない。
- internal validated invariant: 失敗不能な private API に型を狭める。error を返す API を呼んで無視することで「到達不能」を表現しない。

cleanup は cancel 済み job context とは別の bounded cleanup context/resource grant で全対象へ試行する。`context.Background()` で無期限 shutdown せず、timeout 時は resource/outcome unknown を result に残す。

```text
Result {
  primaryFailure?
  cleanupFailures[]
  diagnostics[]
  outputOutcomes[]
}
```

同じ resource の Close/Abort error を二重報告しないよう、ownership と一度限りの finalization state を Host が持つ。third-party plugin error は component/node/phase を付け、原文を残しつつ stable diagnostic code で surface へ投影する。

## hot-path 性能契約

新設計は次を非交渉条件とする。

1. item ごとの reflection、schema serialize、文字列 component lookup をしない。
2. hop ごとの必須 heap allocation をしない。
3. observation off で metric 用 atomic、clock read、size 計算をしない。
4. node ごとの必須 goroutine/channel を要求しない。
5. linear ownership transfer で refcount increment をしない。
6. immutable stream property/metadata map を item に複製しない。
7. timestamp rescale に arbitrary precision arithmetic を使わない。
8. resource accounting を中央 lock/atomic の item loop にしない。
9. panic recovery の `defer` を item loop に置かない。
10. generic schema support のために `any` map を item ごとに走査しない。
11. compatible audio filter ごとに PCM decode/encode を繰り返さず、schema region の境界にだけ converter を置く。
12. exclusive audio frame は in-place で ownership move し、fan-out 後の変更 branch だけ copy-on-write する。

この契約を満たすため、拡張性は control plane の typed registration に置き、runtime は `Program` で specialization する。

## M5 完了条件

M5 は execution island、ownership、queue、cancel、Finalize、transactional Open/Close を実装する milestone である。planner と `Plan`/`Program` の生成は M4、実 Format/Codec は M6 の担当であり、M5 には要求しない。planner 側の条件は [planner](planner.md#m4-完了条件) を参照する。

> **2026-08-08 完了。** 下記条件を ownership/queue/task/Host failure matrix、PCM public Host walking skeleton、hot-path allocation test、同一 process paired benchmark で逐条確認した。その後に最終 cut を適用し、scalar/SIMD 全 package test、対象 race/vet、generator、docs check を新 stack だけで通した。性能証拠は [performance](performance.md#m5-runtime-performance-gate)、切断結果は [inventory](inventory.md#m5-の切断) を正本とする。

- 同期的な一入力一出力 Processor の linear chain が一つの execution island に fuse され、node ごとの goroutine と buffered channel を要求しない。queue が置かれるのは「queue と backpressure」に列挙した境界だけである。
- plugin contract に channel、scheduler、queue 実装が露出しない。runtime internal を交換しても公式・第三者 plugin の public API が変わらない。
- ownership 契約が conformance test される。Reader 返却で consumer へ move、Writer 成功で writer へ move、Writer 失敗で呼び出し元が保持、drop/cancel/queue drain は owner が破棄。linear path で refcount increment が起きず、fan-out のときだけ `Fork`/retain を通る。
- M3 が値型として置いた `flow.Input`/`Owned`/`Shared` の上で、linear 1 hop の allocation がゼロであることを test で固定する。M3 で失った double `Take`・use-after-`Take` の検出を conformance testkit が担当し、既定 build の hot path に検出用 state を持たせない。
- payload allocator が Host/Job の grant に属し、`sync.Pool` を resource manager や correctness の根拠にしない。`Overwrite` lease は Commit 前に read/publication できず、error/cancel で破棄される。
- 実 queue が bounded で、`Limit` の items/bytes/time を扱う。byte limit は schema が安価な `Size` trait を提供する場合だけ使う。M3 の仮 `schema.Queue`/`Fanout` は削除し、typed component execution binding が traits を private runtime の queue/fan-out factory へ渡す。
- resource accounting が packet/frame ごとに中央 manager を呼ばない。局所 counter に蓄積し、metrics export 時に集約する。
- job context cancel が source、queue、operator、sink、host task group へ伝播し、block 中の read/write を解除する。edge close が idempotent で、send-after-close と double-close を plugin の責任にしない。join できない task を「停止した」と偽らず diagnostic にする。
- EOF が data sentinel ではなく edge close で表される。decoder flush が input close を受けて `Flush` を呼ぶ。最終 codec parameters は `Finalize` の明示 contract で渡り、data packet に混ざらない。
- Open が transaction として行われ、途中失敗で既に開いた component/Endpoint/resource/output transaction を逆順に閉じ、sink を Abort し、Open 中に作った goroutine を cancel/join する。
- 成功時に Finalize → Flush → Sync → PrepareCommit → Commit の順で進み、失敗時は未 commit sink を Abort して、committed / aborted / outcome unknown / rollback attempted を構造化 result に残す。
- primary failure と cleanup failure が分けて集約される。`Close`、`Abort`、temporary file 削除、shutdown の error を `_ =` で捨てる経路がない（[F50](findings.md)）。cleanup は cancel 済み context ではなく bounded cleanup context で全対象へ試行する。
- multi-input component が fan-in policy を宣言し、goroutine の到着順で入力を選ばない（[F22](findings.md)）。必要な buffering と watermark が Plan に現れる。
- panic recovery が execution island または長寿命 task の最上位に一度だけ置かれ、item loop に `defer` が入らない。plugin/component identity、plan node ID、phase、stack、primary/cancel status を記録する。
- observation off で hot path に metric 用 atomic、clock read、size 計算が現れない。observation の各段階が同じ event model から集約される。
- `Fast`/`Stable`/`Portable`/`Realtime` が Run の分岐にならず、Host が Compile 前に policy vector へ展開する。item loop が preset、CPU feature、catalog を参照しない。
- **hot-path 性能契約の 12 条を代表 benchmark と test で確認する。** 特に hop ごとの必須 allocation、linear ownership の refcount、node ごとの goroutine/channel、observation off の atomic を数値で示す。
- **旧 pipeline と新 runtime の paired benchmark を同一 harness へ接続する。** M0 baseline は旧 pipeline を測っており、旧 contract 層を切断した後では比較対象が失われる（[refactor.md](../refactor.md#実装ロードマップ)）。この benchmark を取り終えることが次項の切断の前提条件である。
- **walking skeleton が新 runtime で通る。** M4 が planner 経由にした経路を、island、queue、cancel、Finalize を含む実行で流し、bytes、item 数、順序、timestamp が同じであることを検査する。
- M3 専用の `host.Open(identity)` が残っていない。M4 で置き換えられていなければここで削除する。
- **新規 export ごとに、呼び出し元を示すか、宣言のみとして [scope](scope.md) の分類節へ consumer を作る milestone とともに記載する。** どちらもできない export を残さない。
- 上記を unit/property/race test で検査する。cancel、panic、partial Open、fan-out、Finalize/Commit failure で item、goroutine、resource、temporary output が leak しないことを含める。

### M5 最終単位: 旧 contract 層の切断

上の全条件を満たした後、最後の作業単位として旧 contract 層を一括削除する。範囲と判定規則は [inventory](inventory.md#m5-の切断) を正本とする。この単位は新機能を作らず、削除と移動だけを行う。

- 旧 contract 層が削除され、`core` と `sdk/{engine,conversion,catalog,config,cliflag,buffer,timer,profiling,testutil,optional,pool,date,audio}` が repository に存在しない。
- 未移植の algorithm が `_legacy/` にあり、`go list ./...` に現れない。`go build ./...` と `go test ./...` が新 stack だけを対象にして成功する。
- 同じ概念の実装が二つ compile される箇所が残っていない。`flow` と `core/node`、`plugin.Set` と `core/registry`、`media/*` と `core/domain/*`、`config` と `sdk/config` のような対が消えている。
- 旧 contract に依存しない utility（`sdk/{bits,dsp,dsp/fft,parallel,hash}`、`plugin/pcm/internal/{adpcm,g711}`）が現在地で compile できる。この単位では配置換えをしない。
  - [findings](findings.md) の M5 cut 対象を完了へ更新し、複数 milestone にまたがる行は残件を明示する。
- この時点で repository は WAVE、MP3、FLAC、audio filter、CLI、WASM、demo web の機能を持たない。M6 以降が `_legacy/` から順に移す。未 release 製品として意図した状態であり、回帰ではない。

M5 では次を未完了事項として残す。実 Format/Codec の駆動と `standard`/`integration`/`testkit` の最小形、`standard.Convert` と `cmd/godec` の最短経路は M6。multi-stream、metadata loss report、seek plan、MP4 は M7。variant selection と並列 codec の移行は M8。device/session Endpoint の実装と surface の完成は M9。

## 文書全体の完了条件

この節は runtime contract の最終状態を示す gate であり、M5 単独の完了判定には上記「M5 完了条件」だけを用いる。

- plan 用の変換と実行開始時の変換が重複せず、`Compile` の結果を `Open` が消費する。
- node ごとの goroutine/channel を要求せず、queue が必要な境界にだけ現れる。
- ownership、cancel、rollback、cleanup が contract として test され、plugin author が手動 refcount を扱わない。
- mutable state が Host → Job → component/worker → item lease の最小 owner に置かれ、package-level mutable state を暗黙の owner にしない。
- Finalize、flush/sync、commit/abort が一つの failure-safe lifecycle になり、primary と cleanup の failure を区別して報告する。
- observability が同じ event model から集約され、observation off が hot path に追加コストを持ち込まない。
- runtime internal を交換しても公式・第三者 plugin の public API を変更しない。
