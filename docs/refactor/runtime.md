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
`Prepared.Close` は並行 caller に対しても barrier を持つ。最初の cleanup が child を解放している間、後続 caller は待機し、release callback を再実行せず、memoized な同じ cleanup result を受け取る。timeout で停止済みと偽ることはなく、provider callback が戻らない場合は cleanup bound の制約を Result に残す。observer の `Collector.Close` も同様に bounded wait と pending delivery の精算だけを行い、Ledger へ直接 failure を書かない。

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

誰かが consume すれば deferred な `Drop` は何もせず、誰も consume しなければ受け取った側が解放する。したがって「成功時／失敗時に誰が所有するか」を段階ごとに決める必要がない。panic 巻き戻し中も deferred `Drop` が走るため、runtime 側に item 用の panic cleanup を持たない。component が書くのはこの一行だけであり、cleanup error の join も recover も journal も見えない。

```go
defer input.Drop()

output.Own(&o.out, build(input.Value()))
defer o.out.Drop()
return output.Emit(ctx, &o.out)
```

- Reader は呼び出し元の cell を満たす。EOF では cell を空のまま返す。
- `Emit`/`Write` に cell を渡すことは、consume する機会を与えることであって、所有権の無条件移転ではない。
- linear path は所有権を move し、refcount を増やさない。
- fan-out でのみ `Fork` で二人目の owner を作る。
- queue 境界は cell 同士の `Move` で受け渡す。bounded ring は `flow.Item` を保持するので、生の値と drop trait を別々に持ち回す経路が存在せず、そこから二人目の owner を作れない。
- call stack の外へ payload を置く必要がある側 (collector、transport) は cell を heap に置き、`[]*flow.Item[T]` のように pointer で保持する。container が持つのは cell への参照であり、payload の二人目の owner ではない。その cell は `OpenContext.Owner()` へ bind する。呼び出しより長く生きる slot は、自分でその寿命を宣言する。
- mutable access は exclusive owner のみ。shared item を変更する場合は copy-on-write。
- public read path は backing `[]byte` / `[]T` を返さない。byte は immutable `buffer.Bytes`、typed sample は immutable `audio.Samples[S]` で読み、mutable slice は `buffer.Edit` / `audio.Editor` / `WriteLease` の明示 writer path だけから得る。

ownership の監査と panic provenance は private marker で境界を固定する。`flow.Item` が保持するのは一つの ownership slot だけで、audit の opt-in は `internal/ownership` の unexported `track` face を `ownership.Wrap` が付ける。したがって第三者の `flow.Reporter` が同名の公開 method を実装して監査へ紛れ込むことはない。panic の証拠も任意の `StackTrace` 実装を信用せず、`internal/errorx.MarkPanic` が作った private marker だけを `errorx.RecoveredPanic` が認識する。`flow.ReleaseError`、`journal.PanicError`、observation の panic error は recovered value を保持せず、安全な要約と stack だけを持つ。`host.VerifyOwnership()` を指定した Run だけが tracker を有効にし、既定の item loop に audit state を追加しない。

payload を別の item 型へ包み直すだけの段は `flow.Transfer` で move する。source cell を空にしてから `build` を呼ぶため、成功経路で retain や lease 確保は起きず、build が error または panic で終われば source 側の obligation を一度だけ解放する。build が成功した時点で obligation は target の `Emitter.Own` へ移り、target の既存 payload を `Drop` する際の panic は `Item.Drop` が recover して domain へ報告し、新しい payload は target に残る。Drop panic による新 payload の再解放や二重 cleanup は行わない。両方を生かす必要がある時だけ `Fork` を使う。`Share` が残ってよいのは schema の `Fork` trait と型自身の `Share` method だけで、hop ごとの retain は production code に存在しない。

`flow.Item` は `noCopy` を持つため、別変数への代入、container への追加、range copy、channel 送信といった所有権の複製を `go vet` が検出する。規則が文書ではなく tooling で強制される。

宣言された `Drop` は第三者 code であり、slot が保持物を解放している最中に panic しうる。その panic は `Drop` の内側で recover し、slot の domain へ報告する（上記「Drop は panic も error も返さない」）。したがって `Set` と `Fork` は「保持物の解放が失敗したら受け取り中の payload を解放してから panic を通す」という分岐を持たない。解放は何も巻き戻さないので、受け取り中の payload は必ず格納される。

第三者 `Drop` を runtime の mutex を保持したまま呼ばない。bounded ring の `Pop` は受け取り先 cell の解放を lock の外で先に行い、`Drain` は ring 全体を lock 下で取り出してから解放する。lock 中に panic すると mutex が解放されず、後片付けをするはずの `Drain` が同じ mutex で止まるため、panic が recovery boundary へ到達しない。

### slot と failure domain

`flow.Item` は payload の箱ではなく **ownership slot** である。slot は一度だけ `Bind` で「その slot が payload を所有する traits」と「解放できなかった時の報告先 (failure domain)」を宣言する。所有権は slot 間を移動し、domain を持ち歩かない。payload は常に、いま入っている slot の domain で解放され、報告される。

- **未 bind の slot は所有権を拒否する。`Move`/`Fork` は `false` を返し、送り手は payload を持ったままになる。** 送り手の宣言を暗黙に引き継ぐと、その payload の寿命が「たまたま誰が渡したか」で決まる。呼び出しより長く保持する slot は、自分で domain を宣言しなければならない。
- 未 bind の slot へ `Set` で所有権を渡すことは programming error であり、panic する。解放も報告もできない payload を静かに失うより、その場で言う方がよい。runtime は bound な slot だけを配り、`Emitter.Own` は take の前に bind するため、contract に従う限り到達しない。
- 通常の component は slot を自分で宣言しない。runtime から渡された `into`/`input` はすでに bound で、出力は `Emitter.Own(&slot, value)` で受け取る。domain 無しで valid な item を作る API は無い。
- 呼び出しを跨いで payload を保持する component だけが、`Open` 時に得た `flow.Owner` へ自分の slot を bind する。これが persistent ownership の唯一の入口であり、`Owner` は component と同じだけ生きる。
- queue の ring slot は edge を drain する task の domain に属する。producer が push した payload はその瞬間から consumer のものなので、所有権移動に伴う rebind は要らない。ring は `queue.New` の引数として domain を受け取るので、bind を忘れた ring は存在しない。

ownership slot の種類ごとに、誰が bind するかは構造で決まる。

| slot | bind する者 |
|---|---|
| task が持つ transient slot（source の item、drain の item、fan-in の batch、fan-out の branch） | task constructor が owner domain から |
| output slot (`Emitter.Own`) | その stage の Site |
| queue の ring slot | `queue.New` に渡した edge の Site |
| component が保持する persistent slot | `OpenContext.Owner()` |
| Discard が解放する slot | それを所有していた task の domain（変更しない） |

`host.VerifyOwnership()` は一回の Run にだけ有効な slot 監査である。`Item` の `Set`/`Drop`/`Move`/`Fork`/`Transfer` が Site ごとの signed live count を更新し、全 task・queue・operator・resource と observation sink の cleanup が終わった後、Ledger を collect する直前に監査を seal する。live count が 0 でない node、または途中で負になった node は payload を一切保持しない count-only の `ResourcePhase` cleanup failure として一度だけ記録される。node 間の `Move` は source の `-1` と target の `+1` なので、通常 pipeline、queue、fan-out、`OpenContext.Owner()` に bind して Close で `Drop` する persistent slot はすべて node ごとに 0 へ戻る。testkit の typed case は success、expected failure、rejected emit、active cancel の全 Run でこの監査を有効にする。

監査は optional Reporter face として実装し、`Item` は Bind 時にその有無を既存の末尾 padding に収まる一つの bool として固定する。無効時の item loop はこの bool branch だけで、tracker、atomic、allocation は無く、`Item[int]` は 48 byte のままである。有効時だけ run-local tracker を作り、node ごとの更新を同期する。

監査対象は **`Item` が表す slot transition だけ**である。plugin が raw payload に対して schema の `Drop`/`Fork` callback を直接呼ぶ、`Item` を `unsafe` 等で複製する、Host に登録しなかった goroutine を cleanup 完了後も動かす、のはいずれも plugin contract 違反である。slot の複製は後続の `Drop` が負の count になる限り検出できるが、`Item` の外で直接呼ばれた schema callback は観測できず、そのために payload wrapper や global tracker を hot path へ追加しない。監査 seal 後に未登録 plugin goroutine が行う transition も結果へ後付けせず、Run の task/join contract を破った plugin bug とする。

### Drop は panic も error も返さない

**宣言された `Drop` の panic は `Item.Drop` の内側で recover し、slot の domain へ報告する。** 解放は戻り道の defer で走り、そこには戻り値も recovery boundary も無く、しかも別の panic が既に巻き戻っていることがある。そこで panic を通すと、**実際に仕事を止めた failure を後片付けの failure が置き換える。**

`Drop` は error も返さない。返せば「domain へ記録し、呼び出し側が戻り値へ join する」二重経路が復活し、適用漏れと二重報告を生む。cleanup failure の出口は domain だけである。domain 自身も第三者 code なので、**報告もまた raise しない** ── `flow` は Reporter 呼び出しを recover で包み、報告先が panic しても解放の path を巻き込ませない。

`Drop` が返り値を持たない結果として、宣言された `Drop` が表現できる失敗は raise だけである。error 値を raise しても、任意の値を raise しても、`flow` はどちらも `*ReleaseError`（発生地点の stack 付き、raise された値そのものは保持しない）として domain へ報告する。`Kind` としてはどちらも cleanup panic になる。cleanup error は、component や queue が domain へ直接渡した plain error の側である。

1 item の処理は、consumer に渡す機会を与え、consumer が受け取らなかった payload の解放まで成功して初めて終わる。source、bounded edge、fan-in はいずれもその場で settle し、**解放が失敗したら次の値へ進まない。** 進めば次の payload も同じ壊れた trait で漏れる。

この結果として、`Set`/`Fork` の「保持物の解放が panic したら受け取り中の payload を解放してから panic を通す」という分岐が不要になった。解放は失敗しても巻き戻さないので、受け取り中の payload は必ず格納される。`release.All` も全件試行を保証するだけでよく、error 集約も個別 recover も持たない。単一 slot、queue、fan-out、fan-in が同じ経路になる。

### failure の証拠は Ledger・Domain・Span の三層で持つ

failure の記録先は、それ自身より寿命の短い object であってはならない。panic が捨てる戻り値、誰も読まない `Outcome`、二度目に誰も seal しない journal は、いずれも「記録はされたが回収されない」を作る。そこで責務を三層に分ける。

```text
Run Ledger                  … 一回の Prepared Run 全体。append-only、concurrency-safe
└─ Task / Owner Domain      … 自分に bind された全 ownership slot 以上の寿命
   ├─ Run Span              … 一つの lifecycle operation。単一 goroutine
   ├─ Flush Span
   ├─ Close Span
   └─ Discard Span
```

**Ledger (`journal.Ledger`)** は一回の Run の failure evidence そのものである。Host が Open より前に開き、全 task の join、全 operator の Close、全 resource の release が終わった後で一度だけ読む。append-only であり、記録した failure が clear されることも、上書きされることも、boundary の終了によって到達不能になることもない。書き込みは failure path でしか起きないので、ここの mutex は item loop に乗らない。

**Domain (`journal.Domain`)** は ownership slot の報告先であり、Run 全体を生きる。これは「自分に bind された全 slot 以上の寿命」を満たせる唯一の寿命である。slot は、contract が許す範囲で自分を埋めた呼び出しより長く生きる ── collector/transport が cell を跨いで保持する、component が retain した payload を Flush や Close で解放する、queue が Discard まで payload を持つ ── ので、domain を lifecycle operation の object にはできない。operation は Span であり、一つの domain がそれを順に複数持つ。

**Span (`journal.Span`)** は一つの lifecycle operation の範囲・attribution・recovery boundary である。operation を実行するのは常に単一 goroutine なので、Span の writer は一つである。発生時点で Ledger へ書き、Span 自身は単一の `stopping` provenance だけを持つ。Cleanup が先に stopping になった場合だけ後から届く Work が置き換え、それ以外の後続 failure は stopping を変えない。これは End が返す自己完結 Cause の provenance であり、evidence list ではない。したがって Span を閉じても証拠は到達不能にならず、閉じた後に届いた failure も同じ Ledger に入る。

Span は入れ替わるのではなく入れ子になる。bounded edge の drain task は、自分がまだ実行中の Run の内側で、EOF を見た時点の下流 close という本物の Flush を行う。そこで Flush Span を開いて閉じれば、relabel も、別 goroutine の介入も無しに、その failure が属する operation を正しく名乗れる。開いた者が閉じる。

### slot が報告する先は Site である

domain の報告面は `Domain.At(node) *Site` である。ownership slot が bind するのはこの Site であり、`flow.Reporter` と `flow.Owner` を満たす。Site は immutable で、あらゆる Span より長く生きる。

- Run の間に bind された slot は、Flush・Close・join 後のいずれで解放されても、Run が collect する場所へ報告する。
- node は bind 時点で固定する。「domain の goroutine が今どこにいるか」ではなく「その slot を宣言した stage」を記録する。宣言された `Drop` が失敗したのは、その payload を宣言した stage の責任だからである。
- work failure（`Span.Fail`/`Panicked`）は domain の immutable な home node を使う。callback の途中で別の Site に入ったという暗黙の goroutine 状態を持たず、outer task、Reader、Joiner の panic も同じ domain home に帰属する。
- `Site.Perform` の callback error/panic だけは、ownership slot と同じく bind 時に固定された Site node を使う。Site は Span を relabel せず、nested operation の attribution は operation を開いた domain の home に留まる。

`Span.Clean()` は「今行った解放は成功したか」を、記録内容を読まずに答える。domain 上の record 件数を span 開始時点と比べるだけである。item ごとに走るのはこの atomic load 一つで、hop ごとの fused な直呼び出し path には現れない。

### 一つの failure は一つの event である

`EventID{Run, Seq}` は Ledger だけが採番する。`Run` は ledger を、`Seq` はその ledger 内の位置を表す。task 名、group、operation、attempt、retry のどれも identity に関与しない。したがって data task group と plugin task group は同じ counter から採番され、表示名を共有する二つの task も衝突しない。ledger を跨いだ identity は解決されないので、ある Run から漏れた `EventID` が別の Run の event と誤認されることもない。

`Failure` は `ID`/`Kind`/`Operation`/`Task`/`Node`/`Err`/`Stack` を持つ。`Operation` は phase 写像用の metadata であり、identity には関与しない。`Kind` は「仕事を止めたもの」と「解放・close できなかったもの」を分け、それぞれの error 形と panic 形を区別する。

`Operation` はこの codebase で唯一の lifecycle 語彙である。`journal.Operation` に `Prepare`/`Open`/`Run`/`Observation`/`Finalize`/`Flush`/`Sync`/`PrepareCommit`/`Commit`/`Abort`/`Close`/`Join`/`Discard`/`Resource` を置き、`host.Phase` はその public な射影に過ぎない。Host はこの対応を一つの pair table で両方向に検索し、未知の値を Run として黙って扱わない。bounded overflow group のように operation 自体を保持していない集約だけは `UnknownPhase` として返す。

Span を持たない domain ── component が lifecycle 全体で持つ Owner が、二つの step の間で retain していた payload を解放する場合 ── も、bind/open 時に固定した Site/Home attribution と、所有側が明示した operation の下に記録される。mutable な stage、`Enter`/`Leave`、`Span.node` を暗黙の provenance として持ち回らないため、記録されないことも node が後から変わることも無い。

### Ledger は bounded だが loss-aware である

「全 failure object を永久に保存する」ことと「何も失わない」ことは同じではない。component が大量の payload を retain し、Flush で全ての解放が失敗する経路は、occurrence 数に比例して memory を使う。Ledger は **詳細を有界にし、事実は失わない**。

`journal.Budget` が上限を決める。既定値は `DefaultBudget()` 一箇所にあり、test は `NewBoundedLedger` で極小 budget を注入する。

上限に達しても次は必ず残る。

- 発生した事実と**総発生回数**（saturating。wrap させない）
- 最初と最後の `EventID`
- 代表 sample（budget に空きがある場合）
- 省略された occurrence 数
- 詳細が budget により省略されたという明示

上限は二段構えである。一段目は group ごとの sample 数、二段目は Ledger 全体の **全 occurrence 共通** sample 数・group 数・stack 数/bytes である。work occurrence も例外にしない。`errors.Join` 一つが任意数の独立 work occurrence を持てるためである。停止理由の provenance だけは sample とは別の一件として保持する。片方だけでは、class が occurrence ごとに変わる error に対して class table そのものが肥大する。group 数の上限を超えた class は metadata を残さない **一つの global overflow group** へ畳み込み、そこでも総数・最初/最後・畳み込んだ class 数・truncation を残す。

**aggregation は storage 上の圧縮であって identity ではない。** 各 occurrence には引き続き固有の `EventID` を採番する。echo の dedupe、同一 event の再観測判定、Primary の provenance、`errors.Is` の代用のいずれにも aggregation を使わない。それらは `EventID` だけで判断する。

aggregation key (`journal.Class`) は **安全で有限な構造情報だけ**から作る: Task/Owner、Node、Operation、Kind、失敗の class（diagnostic code、無ければ Go type）、call-site の `StackID`。`err.Error()` の全文は使わない — payload や秘密を含みうるし、動的な値で cardinality が跳ね上がって aggregation を回避でき、長い string 自体が memory 攻撃になる。

stack は Ledger 内の depot で intern する。同じ call site の stack は一度だけ保存し、event と group は共有する。budget を超えた stack は保存せず、`Truncated` により「stack が無かった」と区別できる状態を残す。raw panic value は stack metadata にも class key にも入らない。

**cancellation cause は自己完結している。** `Cause` は EventID、元の error、Kind/Operation/Task/Node と安全な Stack metadata を同時に運ぶため、sample から event 全体を引き直す pin table を持たない。同じ Ledger の EventID を運ぶ Cause は、representative sample が省略されても元 occurrence の attribution で echo として解決する。Stack は Ledger の immutable な depot slice を共有し、Cause ごとの複製で budget を迂回しない。これは全 sample の hard cap と identity dedupe を両立させる。停止理由の snapshot は、sample budget とは別の固定一件の provenance である。

**正確に保持できない値は、正確なふりをしない。** overflow group の distinct class 数は bounded set で数えられる間は正確で、それを超えたら `ClassesTruncated` により lower bound であることを明示する。`Budget.Groups` は「別々に追跡する class 数」であり、それに加えて global overflow group が高々一つ存在する。overflow group は operation も失うため、`host.Suppressed.Phase` は `UnknownPhase` として返す（Run と誤表示しない）。`host.Suppressed` と diagnostic もこの lower-bound bit を公開し、rendering は「少なくとも N classes」と表現する。

aggregation は cleanup loop を止めない。budget に達しても全 payload の解放は試行し、全件を数える。report は failure 一件あたり amortized O(1) で、Ledger lock の内側で第三者 code（error chain の `Unwrap` を含む）を呼ばない。

Host は `Result.Suppressed []Suppressed` としてこれを返す。diagnostic にだけ隠さない。`Suppressed.Retained`/`Omitted` は representative sample の予算会計であり、Primary/Cleanup に現れる一件の stopping provenance を二重に数えない。`resultError` は代表 error を一度だけ join し、発生回数は構造で表す — 同じ error を occurrence 数だけ join しない。

```text
release failure: 100,000 occurrences
2 detailed samples retained
99,998 occurrences aggregated
17 distinct classes omitted by ledger budget
stack details truncated: true
```

### cancellation cause は event への参照である

`journal.Cause` は Ledger identity、元の error、元 occurrence の安全な attribution metadata を運ぶ自己完結した値である。`Span.End()` がこれを返し、`task.Group` が `context.CancelCauseFunc` へ渡す。`context.Cause` はこの値をそのまま返すので、cancellation の下流にいる全員が同じ identity・attribution・元の error を運ぶ。sample が省略されても、Cause 自身で echo 判定と Host projection に必要な情報は失わない。

**Ledger が受け取る単位は「error 値」ではなく「独立した一 failure occurrence」である。** `errors.Join` は最終表示のためのものであり、証拠を記録する前には使わない。複数の component が独立に失敗したなら、それぞれが自分の failure をその場で記録し（`Site.Fail`）、制御用には最初の参照だけを返す。第三者の Flush/Close callback は `Site.Perform` が node ごとの panic boundary として呼ぶ。panic もその node の独立 event にし、downstream component と fan-out sibling の close は続行する。Ledger より前で join すると、二つの独立した failure が一つの event になり、consumer が二度と分解できない。

`Ledger.Record` は受け取った error graph を occurrence へ分解する。`errors.Join` の枝は、single wrapper の内側にあっても別々の occurrence として扱い、single wrapper の連鎖だけは一つとして扱う（`fmt.Errorf("%w")` は一つの failure の文脈であって二つ目の failure ではない）。inspection は ledger/domain lock の外で行い、panic する `Unwrap` や上限を超える循環的な chain は opaque な一 occurrence へ縮退する。in-process plugin が `Unwrap` 内で永久 block することまで Host が強制停止できないのは、Flush/Process の永久 block と同じ plugin contract 上の限界である。

`Ledger.Record` が既存 event を返して何も append しないのは、**その枝がその event の再伝播そのものである場合だけ**である。判定は identity であり、内容の類似ではない。graph のどこかに `*Cause` があるという理由で error 全体を echo とみなすことはしない — cause を運ぶ枝と独立した failure の枝が同居しうるためである。したがって、

- 二つの task が同じ sentinel error を独立に返せば、二つの event になる。
- 一つの failure が四つの boundary で観測されても、一つの event のままである。
- 「Primary が既にある」と「context cause が今の failure と一致する」を根拠に、独立した failure を echo と誤認して握り潰す推論は存在しない。判定に Primary も context も参加しないためである。

peer が bare な `context.Canceled` を返す場合は、`task.Group.settle` が「run の停止理由を指す参照」へ置き換えるか、まだ何も記録されていなければ何も返さない。run が外部から cancel された理由は、cancel した boundary が一度記録するものであって、それに気付いた全 task が各自報告するものではない。同様に、bounded edge の producer が閉じた queue に出会うのも失敗ではなく結果なので、`bufferDelivery.Emit` は run の停止理由を名乗る。そうしないと、producer がどこまで進んでいたかで report が変わる。

### 誰がどの Span を開いてよいか

**別 goroutine の domain 上に Span を開いてよいのは、その goroutine がその domain へ書き終えたと言える時だけ**である。「task の work function が返った」だけでは足りない。`Domain.Perform` は `work` が返った後に `span.Fail(...)`・`span.End()` を実行するため、`work` の中から送るどんな完了信号も、この最後の書き込みより先に他の goroutine へ届く。source の完了通知と join の barrier signal はどちらも実際にこの形で `-race` の data race だった。

そこで `task.Group.StartDomain` は、Span が閉じた**後**にだけ呼ばれる `sealed func(error)` を受け取る。`Execution.Start` は source の完了信号をここからだけ送り、`zipState` も `done` をここから閉じる。`WaitSources` と barrier が読む信号は、これで happens-before の内側に入る。

その条件を満たす相手にだけ、`Execution.Finish` は Flush Span を開く。source は `WaitSources` がその信号を観測済みなので該当し、join は barrier が同じ信号を待ってから Quiesce が成功するので該当する。bounded edge は該当しない ── その barrier は ring が idle だとしか言わず、drain task は Finalize が生む遅延 Flush 出力を同じ queue で受け取るために意図的に生き続ける。したがって bounded edge の下流 close は、その drain task 自身が EOF を見た時に自分の domain 上で行う。

Flush が**同じ domain**の上で行われることが、retain された cell にとって本質的である。`Emitter.Own` で埋められて `Process` を跨いで保持される cell は、埋められた時点の Site を憶えており、payload を持つ slot は rebind されない。Flush が別 domain を使えば、その cell は Run が終われば誰も読まない場所へ報告し続けることになる。同じ domain を使う限り、解放がどの step で起きるかは「どの operation の下に記録されるか」だけを決め、「記録されるかどうか」は決めない。

**report は開始時に span ticket を取得し、その Span へ原子的に commit する。** ticket は対象 Span、operation、span-local dirty state を同じ線形化点で固定する。よって Span 開始前に claim した report は新 Span の `Clean` を dirty にできず、Span 内に claim した report は解析中でも直ちに dirty になる。記録は error 自身の chain を歩く — domain が所有しない code である — ため lock の外で行う。`Span.End` は自分に属する in-flight ticket を待ってから閉じる。ticket の release は `defer` で必ず行うので、`Unwrap` の panic が pending を残すことはない。永久 block する plugin code は in-process execution 全般と同じく強制停止できず、その report を開始した operation が待つ。

`Domain.Perform(operation, work)` が唯一の boundary である。panic を recover して記録し、どう返っても Span を一度だけ閉じ、停止理由への参照だけを返す。task 自身の Run も、run 主導の Flush も、Discard も、bounded edge の内側 Flush も、すべてここを通る。`Execution.Finish` はこの参照だけを返し、それとは別に error を並行して返さない。読む経路が一つしかないので、戻り値と別途 recover した panic が競合しない。

### Run の結末は一箇所で collect する

Host の lifecycle failure も task の failure も同じ Ledger に入る。二重経路が無いので、「どちらに載ったか」を突き合わせる bookkeeping も、既報告集合も要らない。全 task が join し全 operator が close した後、`runner.collect` が Ledger を一度読んで `host.Result` を作る。

task group の `Report` は failure 本体を複製せず、join が観測した `Running` の child task 名と `WaitErr` だけを持つ。Host はそれぞれを `Join` phase の Cleanup event として記録し、期限後も動いている child を Primary または「停止済み」として投影しない。task 自身が記録した failure は元の task/domain の EventID のまま一度だけ collect する。

```go
type Result struct {
    Primary   *Failure   // run を代表して停止理由となった failure
    Secondary []Failure  // 同時に起きた独立した別の work failure
    Cleanup   []Failure  // 解放・close・rollback・discard できなかったもの
    ...
}
```

分類は「それが何であるか」で決まり、「どの boundary が気付いたか」では決まらない。

- `Kind.Cleanup()` は `Cleanup` へ。run が既に止まりかけている間に失敗した解放は、止まった理由を説明しない。
- 残りのうち最も早い event が `Primary`。最も早いことが「停止理由」の定義である。後続はすべてそれより後に起きており、単なる echo は event ですらない。
- それ以外の work failure は `Secondary`。二つの component が、互いの結果ではなく同時に失敗しうる。片方だけ報告する、cleanup と偽る、diagnostic にだけ隠す、のいずれもその run を実際と違うものとして記述することになる。

`Failure` は由来の `EventID` を持つ。`journal.Cause` しか見ていない consumer も、sample lookup には依存せず identity・元 occurrence の attribution・元の error を得る。sample cap が stopping event を省略した場合、Host は別保持の一件 provenance snapshot を Primary/Cleanup に投影する。`resultError` は Primary・Secondary・Cleanup を全て join する。どれも他の代用にはならない。

task の外で走る cleanup ── `Queue.Drain`、`Execution.Discard` ── は domain を引数で受け取らない。解放される payload は、それを所有していた task の domain のものだからである。`Execution.Discard` はその domain 上に Discard Span を開き、それが lifecycle 上の位置を与えると同時に、他の owner を巻き込む declared `Drop` の panic を recover する。

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
    Span  int64
}
```

`job.ResourcePolicy.Queue` が per-edge の physical policy を Plan/Program へ固定する。既定の Fast/Stable/Portable は
4 item の item-only queue である。Realtime は 2 item、16 MiB、250 ms の physical `Span` を持ち、それとは
独立して `job.Policy.Alignment.Zip` に 250 ms の semantic tolerance を持つ。schema が安価な `Size` trait を
提供する場合だけ byte limit を、`Time` trait と stream time base がある場合だけ `Span` を有効にする。
wall-clock duration は planning 時に stream-local tick へ変換し、item loop では変換しない。使えない physical
dimension は無視して item limit を必ず残す。

physical queue は `Items`、利用可能なら `Bytes` と stream-local tick の `Span` だけを強制する。Plan の
`Buffer.Limit` と `FanIn.Limit` はこの同じ physical limit を投影する。Zip alignment は別の
`job.AlignmentPolicy.Zip` から tick へ変換し、Plan の `FanIn.Tolerance` と private Zip execution だけに投影する。
有効な `Span` または Zip tolerance を持つ fan-in は接続 edge の time base が一致する場合だけ compile する。
Zip は各 input から一 item ずつ待ち、batch の timestamp spread が tolerance を超えれば `ErrTolerance` で
fail-closed にする。physical queue は tolerance を強制せず、Zip は queue span を alignment として解釈しない。

> **歴史的注記:** M5 完了時点では旧 `QueuePolicy.Window` を physical timestamp span と Zip tolerance の両方へ
> 暫定利用し、`ErrWatermark` で失敗させていた。この契約は M7-1 で上記の `Span` / `Alignment.Zip` へ置き換え済みで、
> 現行 API、Plan、runtime の説明ではない。late/drop/conceal policy は M9 の realtime consumer まで追加しない。

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

Run は caller context に `cancel.Link` した phase child を作り、その phase child から job child を作る。`internal/cancel.Link` は boundary 内の cause を comparable carrier へ包み、`Normalize` は停止済み context と pure single unwrap chain の trusted Cause だけを復元する。live context、joined error、malformed chain、独立した provider error は cancellation として正規化せず、その error を保持する。Flush failure は job child だけを止めて peer Flush を継続し、non-Flush failure は phase child を先に止めて delayed Flush を抑止する。

host は timeout 後に「停止した」と偽らず、join できない task を diagnostic にする。強制終了が必要なら別 process boundary が必要である。

queue の終端には成功の `Seal` と停止の `Abort` という別状態を置く。`Seal` だけが、既に入った item を drain した後の `io.EOF` と downstream の `Flush` を許す。cancel/failure cleanup は `Execution.Abort` を通じて未終端 edge を `Abort` し、`Pop` は EOF ではなく context cause を返す。cause がまだ無い内部停止だけは task が消費する control value であり、Ledger の Secondary にはしない。いったん successful finalization が `Seal` した edge は `Abort` が書き換えない。これは fan-out の一枝が Flush に失敗しても、既に EOF を受け取った sibling を独立に close して両方の failure を残すためである。

`Push`/`Pop` は lock を取得した直後にも `context.Cause` を検査し、terminal state、利用可能な item、sealed-empty の EOF より先に cause を返す。したがって pre-canceled context が graceful EOF や queued delivery に化けることはなく、cause の無い `Abort` だけが `ErrAbandoned` になる。正常な finish では close transaction が phase context を bounded edge に先に宣言してから `Seal` する。peer の Flush failure は job context を止めても phase context を止めず、準備済み sibling は全件 Flush を試行する。一方 caller の cancel、Run/Finalize の failure、または準備前の停止は phase context も止め、EOF/Flush を開始しない。prepare は context の記録だけを processor/fan-out/observed wrapper 越しに先行させ、各 buffer の close が delayed output を受け入れた後に `Seal` する。

### EOF

通常 EOF は data sentinel ではなく edge `Seal` で表す。decoder flush は sealed input を drain して EOF を受けた時だけ `Flush` を呼ぶ。abort/cancel は EOF ではなく停止であり、Finalizer と delayed output の成功 flush を起動しない。動的な stream add/remove は専用 control event schema とする。

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

Host は physical buffering を `Limit`、policy 固有の semantic constraint を別 field として Plan に含める。現行 Zip の constraint は `Tolerance` であり、queue `Span` とは共有しない。「deterministic」は単に毎回同じ順序という意味ではなく、media semantics を保つ ordering rule が明示されていることを意味する。late/drop/conceal は realtime consumer が入る M9 まで policy に追加しない。

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

observation sink の failure は collector の `Config.Fail` callback から一度だけ Host の failure boundary へ渡す。これが observer failure の Ledger への唯一の ingress であり、callback はその node/phase を保ったまま job/phase cancellation と Result projection へ接続する。`Collector.Close` は新しい event を受け付けず、bounded wait と queued delivery の drop を完了させるだけで、failure を別 ledger 経路へ記録しない。

## panic と error

panic recovery は execution island/task の最上位に一度だけ置き、次を記録する。

- plugin/component identity
- plan node ID
- phase
- stack
- primary/cancel status

error は sentinel 文字列ではなく phase-aware structured error とする。plugin error、invalid input、unsupported mapping、resource exhausted、cancel、host bug を分類し、CLI/WASM/HTTP が同じ意味を別表現に変換できるようにする。

CLI の `ExitCanceled` は caller context の `Err` が停止している時だけ選ぶ。live caller context に plugin/runtime error が `context.Canceled` や `DeadlineExceeded` と join されても、sentinel は plugin evidence であり `ExitRuntime` とする。

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
  primary?        … run を停止させた failure
  secondary[]     … 同時に起きた独立した別の work failure
  cleanup[]       … 解放・close・rollback・discard できなかったもの
  diagnostics[]
  outputOutcomes[]
}
```

同じ failure を二重報告しないための機構は、「同じ resource か」を判定する bookkeeping ではなく、[failure の証拠](#failure-の証拠は-ledgerdomainspan-の三層で持つ) の event identity である。全 failure が一つの Ledger に一度だけ入り、collect が一度だけ読む。third-party plugin error は component/node/phase を付け、原文を残しつつ stable diagnostic code で surface へ投影する。

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

### failure 機構が hot path に持ち込んではならないもの

三層化そのものは遅くないが、Ledger/Domain/Span を item path に載せると遅くなる。境界は次で固定する。

- **Ledger へ書くのは failure 発生時だけ。** 成功した run は event を一つも持たない。成功した `Drop` は domain に触れないので、lock も event 採番も append も起きない。
- **失敗した failure 一件あたりの記録は amortized O(1)** で、保持量は occurrence 数に比例しない（[bounded aggregation](#ledger-は-bounded-だが-loss-aware-である)）。
- **EventID の採番は failure ごと。** item ごとではない。
- **Span は operation ごとに一つ。** item ごとに作らない。
- **`Item` は payload、宣言された `Fork`/`Drop`、reporter、`bound`/`valid`、`noCopy` を保持し、failure metadata（Task、Operation、Node、EventID、Ledger）は持たない。** ownership audit を opt-in した slot だけが reporter に audit hook があることを示す `audited bool` を一つ cache する。tracker/atomic は Ledger 側にあり、無効時の Item loop はその bool branch 以外の audit state を持たない。
- **node attribution に per-item lookup を使わない。** Site の node と Domain の home は bind/open 時に固定した文字列であり、goroutine-local な `Enter`/`Leave` state は持たない。
- **`Move`/`Fork`/`Emit` の成功 path に mutex も atomic も無い。**
- 追加コストは run 数・task 数・operation 数・failure 数に比例し、item 数には比例しない。

例外は一つだけである。source、bounded edge の drain、fan-in の各 task loop は、settle した item ごとに `Span.Clean()` を一度呼ぶ。これは span-local dirty bit の atomic load 一回であり、fused な hop path には現れない。別 goroutine の ownership slot が report を開始できるため、plain read にすると data race になる。

性能 gate は次を固定する。

- linear `Move`/`Drop`、fused hop: 0 alloc/item
- queue Push/Pop: 0 alloc/item
- 成功した `Drop`: Ledger への append も lock も無し
- observation off で metric 用 atomic 無し
- `Item`／queue slot の size を benchmark 出力に記録する
- failure benchmark を成功 benchmark と分離し、片方の regression をもう片方に隠さない
- retained ownership を使わない component は、node ごとの Owner domain 一つ分より多く払わない
- direct path の throughput が同一条件で概ね 2 倍以上悪化しない
- failure storm で Ledger の保持量が occurrence 数に比例して増えない

## M5 完了条件

M5 は execution island、ownership、queue、cancel、Finalize、既に bound 済み operator/output の transactional Open/Close を実装する milestone である。planner と `Plan`/`Program` の生成は M4、Provider session の acquire、共有 probe、Format inspect、spool と実 Format/Codec は M6 の担当であり、M5 には要求しない。planner 側の条件は [planner](planner.md#m4-完了条件) を参照する。

> **2026-08-08 完了。** 下記条件を ownership/queue/task/Host failure matrix、PCM public Host walking skeleton、hot-path allocation test、同一 process paired benchmark で逐条確認した。その後に最終 cut を適用し、scalar/SIMD 全 package test、対象 race/vet、generator、docs check を新 stack だけで通した。性能証拠は [performance](performance.md#m5-runtime-performance-gate)、切断結果は [inventory](inventory.md#m5-の切断) を正本とする。

- 同期的な一入力一出力 Processor の linear chain が一つの execution island に fuse され、node ごとの goroutine と buffered channel を要求しない。queue が置かれるのは「queue と backpressure」に列挙した境界だけである。
- plugin contract に channel、scheduler、queue 実装が露出しない。runtime internal を交換しても公式・第三者 plugin の public API が変わらない。
- ownership 契約が conformance test される。Reader 返却で consumer へ move、Writer 成功で writer へ move、Writer 失敗で呼び出し元が保持、drop/cancel/queue drain は owner が破棄。linear path で refcount increment が起きず、fan-out のときだけ `Fork`/retain を通る。
- `flow.Item` pointer の linear 1 hop が allocation zero であることを test で固定する。所有権は `Move`、fan-out は `Fork`、release は `Drop` に統一し、double consume/use-after-consume の検査は conformance testkit と opt-in `VerifyOwnership` に置く。既定 build の hot path に検出用 tracker state を持たせない。
- payload allocator が Host/Job の grant に属し、`sync.Pool` を resource manager や correctness の根拠にしない。`Overwrite` lease は Commit 前に read/publication できず、error/cancel で破棄される。
- `resource.Request.Workers` が node-local task starter の同時実行上限として消費され、component が grant を超えて worker task を開始できない。consumer の無い temporary dimension を resource request/grant に残さない。
- M5 時点の実 queue は bounded で、`job.QueuePolicy` から Plan/Program に固定した `Limit` の items/bytes/time を扱った。byte/time は対応 trait がある edge だけで有効にし、当時の window は planning 時に stream-local tick へ変換した。この完了記録の window は現行 API ではなく、上の「queue と backpressure」に記した physical `Span` と Zip `Alignment` に置き換え済みである。M3 の仮 `schema.Queue`/`Fanout` は削除し、typed component execution binding が traits を private runtime の queue/fan-out factory へ渡す。
- resource accounting が packet/frame ごとに中央 manager を呼ばない。局所 counter に蓄積し、metrics export 時に集約する。
- job context cancel が source、queue、operator、sink、host task group へ伝播し、block 中の read/write を解除する。edge close が idempotent で、send-after-close と double-close を plugin の責任にしない。join できない task を「停止した」と偽らず diagnostic にする。
- EOF が data sentinel ではなく edge close で表される。decoder flush が input close を受けて `Flush` を呼ぶ。最終 codec parameters は `Finalize` の明示 contract で渡り、data packet に混ざらない。
- Open が transaction として行われ、途中失敗で既に開いた component/Endpoint/resource/output transaction を逆順に閉じ、sink を Abort し、Open 中に作った goroutine を cancel/join する。
- 成功時に Finalize → Flush → Sync → PrepareCommit → Commit の順で進み、失敗時は未 commit sink を Abort して、committed / aborted / outcome unknown / rollback attempted を構造化 result に残す。
- primary failure と cleanup failure が分けて集約される。`Close`、`Abort`、output rollback、shutdown の error を `_ =` で捨てる経路がない（[F50](findings.md)）。cleanup は cancel 済み context ではなく bounded cleanup context で全対象へ試行する。M6 の temporary storage も同じ集約へ接続する。
- multi-input component が fan-in policy を宣言し、goroutine の到着順で入力を選ばない（[F22](findings.md)）。M5 時点では必要な buffering と暫定 watermark を Plan に投影した。現行 Plan では physical `Limit` と semantic `Tolerance` を別々に投影する。
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
