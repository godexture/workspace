# planner の探索規則

## 現行実装から残すものと捨てるもの

現行 bridge resolver には、quality loss、work、step 数、canonical path の順で候補を比較し、unique state 数を制限する考え方がある。決定性と探索上限を意識している点は残す価値がある。

一方、現在は次の問題がある。

- stream state signature が audio の codec/rate/format/bits/layout を連結した文字列に固定される。
- 候補の出力を知るために Filter Factory を生成し、直後に Close する。
- 選択後、同じ Factory を再度生成し、出力が同じか `reflect.DeepEqual` で確認する。
- `QualityLoss` と `Work` の二整数を plugin が自由に設定する。
- 128 unique states が全 job/registry に対する固定上限である。
- bridge 候補は filter role に限定される。
- decoder/encoder/format/filter resolver が別々の優先規則を持つ。
- explicit user graph と auto conversion の境界が role ごとの処理順に埋め込まれる。

新 planner は、現行 algorithm を一般化して残すのではなく、pure `Compile` と typed descriptor を中心に作り直す。

## requested graph と resolved graph

利用者が指定した内容と、host が実行する内容を分ける。

- **Requested graph**: input/output mapping、明示 filter、明示/pinned component、policy。
- **Resolved graph**: parser、decoder、converter、encoder、metadata mapping、queue 等を追加し、全 port requirement を満たした graph。

自動挿入 component はすべて Plan に `AutoInserted`、理由、入力/出力、effect とともに記録する。利用者が要求していない content effect を planner が勝手に追加しない。

advanced API では fully pinned graph を指定できる。この場合も schema/ownership/lifecycle validation は省略しないが、自動 bridge を無効にできる。

## Compile contract

component の semantic transformation は `Compile` だけに実装する。

```go
type Compiled[P any] struct {
    Plan         P
    Outputs      flow.Outputs
    Requirements []Requirement
    Effects      []Effect
    Resources    resource.Request
    Estimate     Estimate
}

func Compile(
    ctx component.CompileContext,
    config Config,
    inputs flow.Inputs,
) (Compiled[Plan], error)
```

`Compile` は次を満たす。

- I/O、goroutine、allocator、clock、global registry を使用しない。
- input/config/policy が同じなら byte-equivalent な canonical result を返す。
- 繰り返し呼び出せる。
- incomplete/unsatisfied input は structured `Requirement` として返す。
- invalid config/input と plugin bug を区別する。
- output descriptor、effect、resource estimate を一度に返す。

`Open` は `Compiled.Plan` を受け、output descriptor を再計算しない。

## Suggest は変換処理を持たない

自動 bridge component は、必要条件に対する有限個の config 候補を提案できる。

```go
func Suggest(
    ctx component.SuggestContext,
    input stream.Descriptor,
    need component.Need,
) []config.Value
```

各候補の output/effect/cost は必ず同じ `Compile` を呼んで求める。Suggest と Open/Run に変換規則を重複させない。

contract:

- result は deterministic order
- finite
- duplicate canonical config を返さない
- declared maximum を越えない
- I/O/instance 作成をしない
- target と無関係な全 config space を列挙しない

通常 component は Suggest を持たなくてよい。

## 自動挿入してよい component

role 名ではなく、宣言された effect と job の明示 goal で決める。

### Structural

- container chunk → codec packet の Parser
- packet framing/extradata の bitstream transformation
- time-base representation
- lossless metadata carrier mapping
- cursor-free probe prefix replay と capability view adaptor

必要なら warning なしで自動挿入できる。ただし Plan には表示する。

source/sink capability を補う spool は structural だが、I/O、storage、開始 latency、final copy、rollback semantics を変えるため無条件には挿入しない。finite input/output で `AllowSpool` policy と resource budget が許す場合だけ候補にし、予測/上限 byte と Effect を Plan に出す。live/infinite source を seekable に見せるための spool は候補にしない。

### Representation

- sample/pixel format conversion
- planar/packed conversion
- resample、channel remix
- colorspace conversion
- subtitle encoding conversion

明示 goal/下流 requirement を満たす時だけ自動挿入する。loss/rounding/drop があれば diagnostic を出す。

### Compression

- decoder/encoder
- codec change

filter や target codec/format のために必要な時だけ挿入する。入力 codec を維持できる時は stream copy を優先する。lossy encode generation を Effect として記録する。

### Content、Timeline、Topology

- gain/equalizer/noise reduction
- trim/retime/fade
- mix/overlay/concat
- stream duplication/drop

利用者が Requested graph/Mapping で指定した場合だけ追加する。component が `Automatic` と自己申告しても、host は勝手に artistic/content transformation を挿入しない。

metadata の vocabulary Mapping は target encoding が必要とする時に自動適用できるが、lossy/ambiguous mapping は warning と provenance を残す。

## descriptor state

visited key を format 済み文字列にしない。canonical descriptor fingerprint を使う。

含めるもの:

- schema marker identity
- time base
- canonical property key/value
- codec/format parameter schema
- stream epoch/scope
- policy に関係する provenance

含めないもの:

- display alias
- map iteration order
- runtime pointer
- buffer/instance
- diagnostic text

property value は schema ごとの canonical encoder/hash を持つ。fingerprint collision は equality check で確認する。未知 third-party property も marker identity と canonical value を通じて state に参加できる。

## cost ではなく effect + host policy

plugin が `QualityLoss: 0` と申告するだけで候補を勝たせられる設計にしない。component は構造化 `Effect` と物理 estimate を返し、比較順は Host policy が決める。

```go
type Effect struct {
    Kind      EffectKind
    Scope     stream.Scope
    Loss      Loss
    Detail    diagnostic.Code
}

type Estimate struct {
    CPU        resource.Work
    Memory     resource.Bytes
    Latency    timing.Duration
    Buffering  timing.Duration
    Confidence Confidence
}
```

Effect の例:

- stream/metadata discard
- lossy encode generation
- bit depth/sample-rate/channel reduction
- colorspace/range reduction
- timestamp rounding
- unknown raw metadata loss
- declared bounded numerical difference
- schedule-dependent numerical result
- semantic-exact but byte-different encoding
- requested portability domain を満たさない implementation

plugin は input/output に基づく事実を返す。Host は値の範囲と整合性を検証する。CPU/latency estimate は選択の hint であり correctness の根拠にしない。

## lexicographic rank

default policy は、単一の恣意的な重み付き score ではなく lexicographic rank を使う。

1. hard requirement を満たすこと
2. pinned/explicit request に一致すること
3. stream、metadata、content を捨てないこと
4. stream copy と入力 format/codec の維持
5. lossy generation/semantic loss が少ないこと
6. requested accuracy/reproducibility/implementation/platform policy との一致
7. resource goal: throughput/latency/memory
8. auto-insert step と conversion が少ないこと
9. user/standard component preference
10. canonical component identity + canonical config

1〜6 を、plugin が申告する一つの `Work` 数値で逆転させない。たとえば少し高速な候補が metadata discard や追加 lossy encode を正当化しない。

`Fast` preset は展開後の policy により 6〜7 の variant 選択を変えるが、3 の stream loss や timestamp/order correctness、lossless semantics を下げない。

異なる codec の主観的品質を planner が根拠なく比較しない。default は入力 codec 維持、明示 target がある場合はその codec 内の implementation preference を比較する。

## search algorithm

auto bridge は非負の lexicographic rank を持つため、priority queue を使う Dijkstra 型探索を基本にする。現行の pending slice から線形に best を探す方式は使わない。

```text
start descriptor
  -> indexed candidate components
  -> Suggest finite configs
  -> Compile
  -> canonical output descriptor
  -> rank and predecessor
  -> target Need satisfied
```

catalog は accepted schema/property key で candidate を索引し、全 component を各 state で走査しない。

visited は descriptor fingerprint ごとの best rank を保持する。policy が Pareto frontier を必要とする項目を導入した場合だけ bounded frontier を保持し、無条件に全 path を残さない。

同じ fingerprint を出す non-progress component は expansion しない。

## explicit graph の worklist

multi-input component の requirement は他 input に依存し得る。すべてを一度に global path search せず、requested graph を worklist で収束させる。

1. source inspect と explicit shape を確定
2. compile 可能な explicit node を Compile
3. `Unsatisfied{Port, Need}` を収集
4. 各 edge に bridge path を挿入
5. 影響 node だけ再 Compile
6. output/mux requirement まで収束
7. graph 全体を validation/optimization

一つの input への bridge が別 input requirement を変える mixer 等では同じ Compile を再度呼ぶ。Transform と Start に別ロジックを置かない。

planner は利用者が要求していない multi-input node、mixer、overlay を探索で発明しない。multi-input component は Requested graph に存在し、その各 input edge だけを自動補完する。

## default transcode の探索

出力指定なし:

1. 入力 Format を target 候補にする。
2. mapping 対象全 stream について packet copy と format Binding を検査する。
3. metadata raw carrier preservation を検査する。
4. 全 requirement を満たせれば remux/copy Plan を選ぶ。
5. explicit filter がある stream だけ decode/filter/encode path を探索する。
6. 入力 codec を同 format へ再 encode できるなら優先する。
7. 不可能な場合だけ standard codec preference を適用し、理由を diagnostic にする。

明示 output format/codec がある場合も、変更不要な stream は copy 候補を残す。

## planning budget

128 states のような一つの magic number ではなく、Host policy に planning budget を持つ。

```go
type Budget struct {
    States             int
    Compiles           int
    SuggestionsPerNeed int
    FixpointIterations int
    Duration           timing.Duration
}
```

Duration は defensive cancel budget であり、tie-break に wall clock を使わない。同じ state/compile budget なら同じ結果または同じ structured exhaustion error を返す。

budget exhaustion は「unsupported」と区別し、最も近い unmet Need、探索済み state、制限値を diagnostic に含める。利用者は budget を増やすか component を pin できる。

third-party plugin が大量 Suggest を返しても host が上限で拒否する。trusted plugin model でも accidental explosion を防ぐ。

## cache

同一 planning run 内で次を key に Compile result を memoize する。

- component identity + implementation version
- canonical config
- ordered input descriptor fingerprints
- expanded relevant policy
- immutable platform/CPU feature snapshot
- resolved resource constraint

cross-run persistent cache は初期実装に含めない。plugin build/config schema/provenance による invalidation が必要だからである。

## deterministic behavior

- immutable sorted catalog
- canonical component/config/property identity
- stable priority queue comparison
- map iteration を結果へ使わない
- Suggest/Compile purity
- fixed planning budget
- canonical diagnostic order

を conformance/unit test する。

同じ normalized Job、Catalog、input snapshot、platform/resource snapshot なら、catalog insertion order、map seed、parallel Compile evaluation の完了順を変えても同じ Plan fingerprint を得る。

Compile を並行評価する場合も結果を canonical order で merge し、最初に完了した候補を勝者にしない。

`parallelism=auto` が GOMAXPROCS/resource grant により異なる worker 数へ解決された場合は、異なる実効 Plan と fingerprint になる。これは planner の非決定性ではなく、記録された入力 snapshot の差である。planner の決定性と、runtime output の Stable/Portable 再現性を混同しない。

## Plan の説明

Plan は selected path だけでなく、少なくとも次を持つ。

- requested node/edge と auto-insert node/edge の区別
- component canonical identity、alias、implementation version
- canonical config
- Access Provider/Endpoint identity、redacted reference、input snapshot
- input/output descriptor
- insertion reason と満たした Need
- Effect/loss/warning
- requested preset と expanded policy
- selected variant identity、accuracy/reproducibility contract、schedule dependence
- relevant build/toolchain/platform/CPU feature
- resolved worker、block/batch/partition/fusion、seed
- resource estimate/request
- source/sink capability、spool、transaction/rollback class
- tie-break に使った preference
- planning budget 使用量
- canonical execution signature または portability domain
- Plan fingerprint

すべての rejected candidate を保存すると大きくなるため、通常は保存しない。unsupported/exhaustion 時だけ、各 Need の代表 rejection reason を bounded diagnostic として返す。debug planning trace は明示 opt-in にする。

variant の hard filter、preset 展開、fallback、execution signature、artifact cache の規則は [性能と再現性](performance.md) に従う。

## performance contract

planning は cold path だが、component が増える ecosystem では UX に影響する。

- candidate index により無関係 component を Compile しない。
- Factory/Open/Close を候補評価に使用しない。
- descriptor/config canonicalization を memoize する。
- priority queue と visited best rank を使う。
- trace/全 rejection 保存は opt-in。
- 10/100/1000/10000 component catalog を benchmark する。
- simple copy job、one bridge、long bridge、unsatisfied、budget exhaustion、multi-input fixpoint を測る。

planning benchmark は wall time だけでなく、Compile 回数、expanded state、allocation、diagnostic size を report する。
