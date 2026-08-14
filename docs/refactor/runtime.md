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

これは完成時の pipeline である。M4/M5 が実装した現在の経路は宣言を Normalize/Bind した後の Shape/Compile/Solve/Validate/Describe/Build と runtime lifecycle で、段階 3〜5 と spool bridge の最初の実装は M6 の file/WAVE consumer が担当する。M6 以降の Prepare は input I/O を含むが、各 component の `Compile` は pure のままである。prepared session が Plan と input snapshot を所有し、Run まで同じ session を使う。output transaction は dry-run/Plan 時に開始しない。Access/Endpoint の詳細は [access と endpoint contract](access.md) に定義する。

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

候補 component の `Open` は行わない。M6 以降は input Access session を probe/inspect のため Prepare 中に取得するが、media operator、live Endpoint、output transaction は最終 `Program` が確定した後に依存順で一度だけ開く。M5 時点は application supplied direct resource と宣言済み boundary だけを扱い、session acquire 済みとは主張しない。

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

ownership は API の慣習でなく contract として固定する。**所有権は値ではなく cell (`flow.Item`) が表す。** cell は常に pointer で渡し、最初の `Drop` だけが解放する。payload が cell の外へ生の値として出る経路は無い。

規則は一つである。

> **自分が作った item と受け取った item は `defer ... Drop()` する。**

誰かが consume すれば deferred な `Drop` は何もせず、誰も consume しなければ作った側が解放する。したがって「成功時／失敗時に誰が所有するか」を段階ごとに決める必要がない。panic 巻き戻し中も deferred `Drop` が走るため、runtime 側に item 用の panic cleanup を持たない。

- Reader は呼び出し元の cell を満たす。EOF では cell を空のまま返す。
- `Emit`/`Write` に cell を渡すことは、consume する機会を与えることであって、所有権の無条件移転ではない。
- linear path は所有権を move し、refcount を増やさない。
- fan-out でのみ `Fork` で二人目の owner を作る。
- queue 境界は cell 同士の `Move` で受け渡す。bounded ring は `flow.Item` を保持するので、生の値と drop trait を別々に持ち回す経路が存在せず、そこから二人目の owner を作れない。
- call stack の外へ payload を置く必要がある側 (collector、transport) は cell を heap に置き、`[]*flow.Item[T]` のように pointer で保持する。container が持つのは cell への参照であり、payload の二人目の owner ではない。
- mutable access は exclusive owner のみ。shared item を変更する場合は copy-on-write。
- public read path は backing `[]byte` / `[]T` を返さない。byte は immutable `buffer.Bytes`、typed sample は immutable `audio.Samples[S]` で読み、mutable slice は `buffer.Edit` / `audio.Editor` / `WriteLease` の明示 writer path だけから得る。

payload を別の item 型へ包み直すだけの段は `flow.Transfer` で move する。source cell を解放せずに空にし、変換結果で target を作るため、retain も lease 確保も起きず、どの時点でも owner は一人である。解放義務は変換の成否で移る。build が失敗すれば元 payload を、成功後に target が保持物を解放できず panic すれば新 payload を、それぞれ一度だけ解放する。両方を生かす必要がある時だけ `Fork` を使う。`Share` が残ってよいのは schema の `Fork` trait と型自身の `Share` method だけで、hop ごとの retain は production code に存在しない。

`flow.Item` は `noCopy` を持つため、別変数への代入、container への追加、range copy、channel 送信といった所有権の複製を `go vet` が検出する。規則が文書ではなく tooling で強制される。

宣言された `Drop` は第三者 code であり、cell が保持物を解放している最中に panic しうる。その時点で受け取ろうとしていた payload はまだ owner を持たないため、`Set`/`Fork` は unwind の途中でその payload を解放してから panic を通す。捕まえずに素通しすると、panic 一つで payload が一つ消える。したがって `Set` の不変条件は「渡された payload を保持するか解放するかのどちらかを必ず行う」であり、caller は Set へ渡した後の payload を二重に守らない。

第三者 `Drop` を runtime の mutex を保持したまま呼ばない。bounded ring の `Pop` は受け取り先 cell の解放を lock の外で先に行い、`Drain` は ring 全体を lock 下で取り出してから解放する。lock 中に panic すると mutex が解放されず、後片付けをするはずの `Drain` が同じ mutex で止まるため、panic が recovery boundary へ到達しない。

複数 owner の後片付けは runtime 内部の `release.All` を使い、一件が panic しても残りを解放してから failure をまとめて返す。fan-out の branch、fan-in の batch、queue の drain がこれに当たる。cell を同時に複数持つのは runtime だけであり、第三者は一度に一つの cell を `defer ... Drop()` するため、この helper は public contract に出さない。cleanup は recovery boundary を失った経路で走るため、panic ではなく error で伝える。

cleanup failure は握り潰さない。`release.All` の結果は、それを呼んだ task の答えの一部として返る。fan-in は batch ごとの解放失敗をその場で返し、次の batch へ進まない。`Execution.Discard` は全 task を訪ねてから failure を結合し、Host は通常終了でも cleanup 経路でも Result へ載せる。

task の結末は一箇所で組み立てる。ただし panic は task が返そうとしていた値を捨てるため、戻り道の defer で走った cleanup の failure は、その値に join した瞬間に消える。そこで task の `Scope` が「戻り値では境界へ届かないもの」を運ぶ。`Scope` は node identity と cleanup failure を持ち、一つの task とその task が駆動する delivery 連鎖が共有する。書き手はその task だけ、読み手は同じ goroutine の panic recovery だけなので atomic を使わない。

記録は無条件、読み出しは条件付きとする。cleanup を行う側は failure を `Scope` へ記録したうえで、通常どおり戻り値へも join する。境界は panic を recover した時だけ `Scope` を読む。正常に return した task は自分で持ち出しているので、両方を無条件に読むと同じ failure が二重に出る。`internal/task` が要求するのはこの `Scope` interface だけであり、plugin task には scope が無いので従来どおり panic だけが報告される。

runtime が queue から取り出した item は、その処理が終わるまで edge の `active` に数えられ、barrier はそれが 0 になるのを待つ。item を終えたと言えるのは、consumer に `Emit` で渡す機会を与え、consumer が受け取らなかった payload の解放まで成功した時である。error、panic、宣言された `Drop` の panic のいずれも、そこへ届かなかったという同じ一つの事実であり、error を軽い場合として扱わない。drain task は保持中かどうかを task 単位の flag で持ち、戻り道の defer で必ず精算する。精算は `Complete` ではなく `Abandon` で行う。処理中の item は無くなるので count は正しくなるが、その item は下流で完了していないため、edge は quiescent にならない。ここで idle を返すと、data path が死んだ後の Finalize と Flush を通してしまい、failure の phase も本来の run から後段へずれる。item を abandon するのは失敗した consumer だけであり、その失敗が barrier の context を cancel するため、待ちはその failure で終わる。

fan-in の quiescence は edge の idle 状態ではなく task 自身の結末で決まる。一つの batch が全 input にまたがるためである。input を EOF まで drain し、**かつ戻り道の cleanup がすべて成功した時だけ** quiesce したものとする。EOF に達しても、join が保持していた未 join の batch を解放できなければ task は失敗しており、失敗した task は quiesce していない。error や panic で止まった join も同じで、barrier は idle を返さず cancel を待つ。

したがって `Complete` と `Abandon` の区別は、barrier が `WaitIdle` である edge にだけ意味を持つ。join は正常終了でも未 join の batch を保持したまま終わる（一つの input の EOF が全体の終わりになる）ため、input の `active` は fan-in の quiescence を表せない。fan-in edge の slot は全経路で `Complete` により返し、その呼び出しは capacity の返却だけを意味する。

多数の item を emit する段は cell を一つ保持して `Set` で再利用する。cell が item ごとに escape しないため hop あたりの heap allocation が 0 になる。item ごとに新しい cell を作る書き方も正しいが、その場合は 1 allocation を伴う。

fan-out が一つなら refcount atomic を通らない設計にする。複数 consumer の時も、一 item につき必要最小限の retained handle だけを作る。

fan-out 後の sibling isolation は「書き換えないという慣習」ではなく API surface で強制する。read view から作った mutable copyを変更しても backing は変わらず、変更 branch が `Edit` した場合だけ exclusive backing を再利用するか、その branch 用の copy を allocator から得る。read-only/shared handle に raw backing slice を返す compatibility API は置かない。

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
- **component が宣言する payload memory は「in-flight 1 item 分」であり、同時保持数の乗算は Host が行う。** payload は下流へ move されるため、生成した component の allocator は「その component が同時に生かしうる item 数」分だけ必要になる。その数は Host policy に属する topology と queue 容量で決まり、component は知らない（`plugin.CompileContext` は Compile を pure に保つため policy を渡さない）。したがって Host が Plan から上限を導き、`宣言値 × (そのノードから到達可能な node 数 + 下流に到達可能な queue 容量の合計)` を予約する。component 側の宣言は 1 item 分のままでよい。
- **payload を保持するのは queue slot だけではない。** 到達可能な各 node の operator は処理中の item を一つ保持し、zero-copy 段はその item を持つ間 producer の storage を生かし続ける。queue slot だけを数えると、pipeline が深いほど producer が自分の grant を先に使い切り、grant を超える入力の変換が入力終端ではなく途中で停止する。node 数の項はこの保持分を賄う（F54）。
- **grant は最大 in-flight bytes 以上でなければならない。** payload allocator には backpressure が無く、上限超過は待機ではなく即時 error になる。queue が item 数で backpressure を効かせている間に allocator が byte で失敗すると、正常な流量が resource error として現れる。したがって「同時に保持されうる最大 item 数を確保できる」ことを予約側が保証する。
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

`job.ResourcePolicy.Queue` が per-edge policy を Plan/Program へ固定する。既定の Fast/Stable/Portable は 4 item の item-only queue、Realtime は 2 item、16 MiB、250 ms を既定とする。schema が安価な `Size` trait を提供する場合だけ byte limit を、`Time` trait と stream time base がある場合だけ time window を有効にする。wall-clock duration は planning 時に stream-local tick へ変換し、item loop では変換しない。使えない dimension は無視して item limit を必ず残す。

fan-in は接続 edge の limit/time base が一致する場合だけ compile し、timestamp/zip policy に必要な watermark を同じ tick 単位で Plan に投影する。runtime queue は items/bytes/time と watermark を同じ policy snapshot から強制する。M5 では `QueuePolicy.Window` を edge queue の timestamp span 上限と zip fan-in の許容ずれの両方へ暫定利用し、超過を `ErrWatermark` で fail-closed にする。M7 の MP4/multi-stream consumer を入れる前に、物理的な buffering 上限と media semantics 上の alignment tolerance を別 policy へ分け、late input の待機、失敗、drop、conceal のどれを選ぶかを fan-in policy ごとに明示する。

resource manager は Open 時に codec workspace、worker 数、大きな ring 等の粗粒度 grant を与える。M6 で spool consumer を入れる時に temporary storage の quota と cleanup authority を同じ manager へ追加する。packet/frame ごとの acquire/release を中央 manager に送らない。

局所 counter は cache line を意識し、metrics export 時に集約する。resource tracking を無効にした経路では追加 atomic を発生させない。

## host service

component に渡す service は用途別の narrow interface にする。M5 の `OpenContext` が実際に渡すものは次である。

- `Buffers`: allocator、blob、workspace grant
- `Tasks`: cancel と join が追跡され、node の worker grant を超えて開始できない task starter
- `Diagnostics`: structured event の sink
- `Boundary`: planner が選んだ一つの source/sink/Endpoint capability view

`Temp` は spool/file の実 consumer と同じ M6、realtime component 用 `Clock` は Endpoint の実 consumer と同じ M9 で追加する。使えない service に resource request だけを先行させない。

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

M5 は execution island、ownership、queue、cancel、Finalize、既に bound 済み operator/output の transactional Open/Close を実装する milestone である。planner と `Plan`/`Program` の生成は M4、Provider session の acquire、共有 probe、Format inspect、spool と実 Format/Codec は M6 の担当であり、M5 には要求しない。planner 側の条件は [planner](planner.md#m4-完了条件) を参照する。

> **2026-08-08 完了。** 下記条件を ownership/queue/task/Host failure matrix、PCM public Host walking skeleton、hot-path allocation test、同一 process paired benchmark で逐条確認した。その後に最終 cut を適用し、scalar/SIMD 全 package test、対象 race/vet、generator、docs check を新 stack だけで通した。性能証拠は [performance](performance.md#m5-runtime-performance-gate)、切断結果は [inventory](inventory.md#m5-の切断) を正本とする。

- 同期的な一入力一出力 Processor の linear chain が一つの execution island に fuse され、node ごとの goroutine と buffered channel を要求しない。queue が置かれるのは「queue と backpressure」に列挙した境界だけである。
- plugin contract に channel、scheduler、queue 実装が露出しない。runtime internal を交換しても公式・第三者 plugin の public API が変わらない。
- ownership 契約が conformance test される。Reader 返却で consumer へ move、Writer 成功で writer へ move、Writer 失敗で呼び出し元が保持、drop/cancel/queue drain は owner が破棄。linear path で refcount increment が起きず、fan-out のときだけ `Fork`/retain を通る。
- M3 が値型として置いた `flow.Input`/`Owned`/`Shared` の上で、linear 1 hop の allocation がゼロであることを test で固定する。M3 で失った double `Take`・use-after-`Take` の検出を conformance testkit が担当し、既定 build の hot path に検出用 state を持たせない。
- payload allocator が Host/Job の grant に属し、`sync.Pool` を resource manager や correctness の根拠にしない。`Overwrite` lease は Commit 前に read/publication できず、error/cancel で破棄される。
- `resource.Request.Workers` が node-local task starter の同時実行上限として消費され、component が grant を超えて worker task を開始できない。consumer の無い temporary dimension を resource request/grant に残さない。
- 実 queue が bounded で、`job.QueuePolicy` から Plan/Program に固定した `Limit` の items/bytes/time を扱う。byte/time は対応 trait がある edge だけで有効にし、window は planning 時に stream-local tick へ変換する。Fast の既定は item-only、Realtime は byte/time も有効にする。M3 の仮 `schema.Queue`/`Fanout` は削除し、typed component execution binding が traits を private runtime の queue/fan-out factory へ渡す。
- resource accounting が packet/frame ごとに中央 manager を呼ばない。局所 counter に蓄積し、metrics export 時に集約する。
- job context cancel が source、queue、operator、sink、host task group へ伝播し、block 中の read/write を解除する。edge close が idempotent で、send-after-close と double-close を plugin の責任にしない。join できない task を「停止した」と偽らず diagnostic にする。
- EOF が data sentinel ではなく edge close で表される。decoder flush が input close を受けて `Flush` を呼ぶ。最終 codec parameters は `Finalize` の明示 contract で渡り、data packet に混ざらない。
- Open が transaction として行われ、途中失敗で既に開いた component/Endpoint/resource/output transaction を逆順に閉じ、sink を Abort し、Open 中に作った goroutine を cancel/join する。
- 成功時に Finalize → Flush → Sync → PrepareCommit → Commit の順で進み、失敗時は未 commit sink を Abort して、committed / aborted / outcome unknown / rollback attempted を構造化 result に残す。
- primary failure と cleanup failure が分けて集約される。`Close`、`Abort`、output rollback、shutdown の error を `_ =` で捨てる経路がない（[F50](findings.md)）。cleanup は cancel 済み context ではなく bounded cleanup context で全対象へ試行する。M6 の temporary storage も同じ集約へ接続する。
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

M5 では次を未完了事項として残す。Provider session の acquire、候補間で共有する bounded probe、Format inspect、実 capability の再検証、spool insertion、temporary quota/cleanup、実 Format/Codec の駆動と `standard`/`integration`/`testkit` の最小形、`standard.Convert` と `cmd/godec` の最短経路は M6。multi-stream、metadata loss report、seek plan、MP4 は M7。Plan snapshot からの variant selection と並列 codec の移行は M8。device/session Endpoint、Clock と surface の完成は M9。

## 文書全体の完了条件

この節は runtime contract の最終状態を示す gate であり、M5 単独の完了判定には上記「M5 完了条件」だけを用いる。

- plan 用の変換と実行開始時の変換が重複せず、`Compile` の結果を `Open` が消費する。
- node ごとの goroutine/channel を要求せず、queue が必要な境界にだけ現れる。
- ownership、cancel、rollback、cleanup が contract として test され、plugin author が手動 refcount を扱わない。
- mutable state が Host → Job → component/worker → item lease の最小 owner に置かれ、package-level mutable state を暗黙の owner にしない。
- Finalize、flush/sync、commit/abort が一つの failure-safe lifecycle になり、primary と cleanup の failure を区別して報告する。
- observability が同じ event model から集約され、observation off が hot path に追加コストを持ち込まない。
- runtime internal を交換しても公式・第三者 plugin の public API を変更しない。
