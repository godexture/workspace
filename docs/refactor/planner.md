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

現行の `Spec[C, P, D]`、`Compiled[P, D]` と type-erased `Compilation` の利用例は
[plugin の Compile Example](../../plugin/example_test.go) を正本とする。

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
現行 API は `plugin.Suggest` が typed input descriptor と `Need[D]` を受け、schema で
検証済みの canonical config snapshot を返す。

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

config field の provenance は default、preset、explicit、planner、normalized を区別する。
planner が Suggest または自動 Format 選択から与えた値を explicit と表示しない。この provenance は
表示 metadata であり、同じ resolved value の config fingerprint と Plan fingerprint には影響させない。

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

## M4 完了条件

M4 は `Compile`、solver、graph validation、public `Plan`、private `Program` を新設する milestone である。実行、ownership、queue、cancel、Finalize は M5、実 Format/Codec は M6 の担当であり、M4 には要求しない。runtime 側の条件は [runtime](runtime.md#m5-完了条件) を参照する。

### 作業単位

M4 は新規 package 数が最も多く、descriptor fingerprint のような一つの設計誤りが複数 package を無効化する最初の milestone である。したがって着手前に単位を分ける。分割の判定規則は **各単位が walking skeleton を端から端まで green のまま残すこと** とする。

| 単位 | 内容 | 単位終了時の skeleton |
|---|---|---|
| M4-1 | foundation の構造是正（`media/key` の機構統合、`media/carrier` 分離、`media/format` の alias 再 export 削除、依存制約の test 固定）と walking skeleton の control plane 化 | descriptor を伴って流れる。planner はまだ無い |
| M4-1b | key identity の重複検出。`plugin.Declaration` の target 一般化と `internal/catalog` の検出 | 変わらない。宣言経路が増えるだけ |
| M4-1c | 実装済み foundation package の `Example` 整備と設計文書 code block の置換 | 変わらない。実装に変更を入れない |
| M4-2 | `job`、`resource`、`plugin.Component` の `Compile`/`Suggest`/動的 `Shape`、graph validation | pinned/explicit graph として compile され、検証を通って流れる |
| M4-3 | solver、descriptor fingerprint、budget、lexicographic rank、`Plan`、private `Program` | 自動挿入を含む `Plan`/`Program` 経由で流れる |
| M4-4 | 実 PCM component、Provider/Endpoint の planner binding、合成 filter chain の converter 数 gate | 実 PCM が planner 経由で流れる |

M4-1 を 3 分割したのは、当初 1 単位に置いていた三つの作業が、対象も受け入れ基準も互いに独立だったためである。構造是正（M4-1）は data plane の閉包を test で固定することが成否であり、key の重複検出（M4-1b）は `plugin.Declaration` の contract を広げる判断を含み、Example と文書整備（M4-1c）は実装に一切触れない。順序は依存関係で決まる。M4-1b は M4-1 が作る `media/key` の erased view を必要とし、M4-1c は両者が確定させた API を写すため最後に置く。

M4-4 は議論ではなく実規格が contract を制約する唯一の単位なので、圧縮しない。ここで判明した不足は M5 へ送らずこの milestone 内で直す。

### M4-1 の条件

M4-1 は新しい contract を作らず、M3 の成果に対する構造是正と skeleton の拡張だけを行う。

- marker 由来 typed key の機構（identity 導出、[C17](decisions.md#c17-config-snapshot-は-codec-clone-だけで構成する) の宣言 clone 規則、erased accessor、偽装 key を排除する非公開 method）が `media/key` に一つだけ存在する。`property.Set`、`metadata.Document`、`side.Data` はその上の容器であり、機構を各自で複製しない。
- `metadata.Document` と `side.Data` が `key.Key[T]` を共有し、一つの marker 宣言が両方で通る。`property.Key[T]` は canonical encoder を宣言必須とする別型であり、canonical 表現を持たない key を `property.Set` へ入れる経路が実行時ではなく宣言時に閉じている。区分の根拠は [media](media.md#key-機構は一つ容器は三つkey-型は二つ) を参照する。
- `media/key` が `internal/{marker,snapshot}` と stdlib しか import しない。`plugin` を import しない。
- carrier identity が marker 由来で `media/carrier` にあり、`media/format` の内部型ではない。`media/metadata` が carrier identity 一つのために `media/format` を import しない。carrier が owner field を持たない。
- `media/format` が `access` の capability 語彙を alias で再 export しない。同じ概念に import path が二つ存在しない。
- `media/packet` と `media/audio` の推移的依存が `media/{buffer,timing,side,key}` と `internal/{marker,snapshot}` に閉じている。data plane から `config`、`plugin`、`access`、`diagnostic`、`media/format` へ到達しない。この条件を test で固定する。
- **walking skeleton が control plane を通る。** 各 component の port が `stream.Descriptor`（schema identity、time base、`property.Set`、`metadata.Document`）を伴い、駆動 loop が item と descriptor を並走させる。M3 の skeleton は `media/stream` と `media/property` を一度も構築しておらず、両 package は repository 全体で consumer を持たない。M4-2 以降の planner はこの descriptor の上に載るため、solver を積む前に実際に流して検証する。
- skeleton を M3 の成果物ではなく、以後の全 milestone が自分の contract を通す恒久 harness として位置付ける。

### M4-1b の条件

- 同じ marker を異なる payload 型で宣言した key が `host.New` の aggregate diagnostic として報告される。検出は既存の `plugin.Declaration` と `internal/catalog` に載り、key 専用の registry を新設していない。宣言しない key も動作する。
- 宣言の構築子が `plugin` にあり、`media/key` は `plugin` を import しない。M4-1 が固定した data plane の閉包 test が引き続き green である。
- `plugin.Declaration` の target が「catalog に実在すべき component」と「payload を識別する Go 型」を区別でき、conflict 判定は両者で一つの経路を通る。codec Binding、metadata Binding、Provider scheme の意味が変わらない。
- namespace を `property` と `metadata`/`side` で共有し、容器をまたいだ重複を検出する。`property.Key[T]` と `key.Key[T]` が一つの構築子を共有する。
- `media/tag` の共通 vocabulary が宣言をまとめて公開する。`standard` composition への組み込みは M6。

### M4-1c の条件

- 実装済みの foundation package が `Example` 関数を持ち、[experience](experience.md) の「動く code は各 package の `Example` 関数を正本とする」規則が全 package で成立する。
- 設計文書中の Go code block のうち実装済み package を説明するものが Example への参照へ置き換わっている。未実装 contract の概念例は残す。
- この単位で production code の意味を変えない。API 変更が必要になった場合は Example を歪めず、該当 package を実装した単位へ差し戻す。

### M4 全体の条件

- `Compile` が I/O、goroutine、allocator、clock、global registry を使わず、同じ input/config/policy から同じ canonical result を返し、繰り返し呼べる。満たせない入力は文字列 error ではなく構造化 `Requirement` として返る。
- `Suggest` が deterministic order、有限、duplicate canonical config なし、宣言した上限以内で、I/O と instance 作成をしない。変換規則は `Compile` にだけあり、`Suggest` と `Open` に重複しない。
- 候補評価が component の `Open`/`Close` を呼ばない。現行 resolver の「Factory を試し起動して出力を調べる」経路（[F7](findings.md)）が構造的に不可能になっている。
- descriptor state が format 済み文字列ではなく canonical fingerprint で表され、未知の第三者 property も marker identity と canonical value を通じて state に参加できる。
- component が `Effect` と `Estimate` を返し、比較順は Host policy が決める。lossy generation、content/timeline/stream loss、numerical difference を単一の `QualityLoss` 整数へ潰さない。
- lexicographic rank が実装され、plugin が申告する一つの cost 数値で hard requirement、pinned request、stream/metadata 保持、copy 優先を逆転できない。
- 探索が priority queue と visited best rank を使い、catalog が accepted schema/property key で候補を索引する。同じ fingerprint を出す non-progress component を expansion しない。
- planning budget が Host policy にあり、budget exhaustion が unsupported と区別され、最も近い unmet Need、探索済み state、制限値を diagnostic に含む。
- graph validation が schema 不一致、required port 未接続、one port への重複接続、不正な fan-in/fan-out、許可されない cycle、到達不能 node、sink へ到達しない出力、duplicate mapping、finalizer を必要とする経路の欠落、time base 未解決をすべて compile 時に拒否する。
- 動的 `Shape` phase を持ち、config が port 数を決める component を表現できる。[checkpoint](checkpoint.md) が M3 から送った `plugin/audio` の mixer 相当（現在は空 config と実行時入力数）がこの形で表せることを確認する。実際の plugin 移行は M8。
- `Plan` が requested node/edge と auto-insert の区別、component canonical identity、canonical config、input/output descriptor、insertion reason、Effect/loss、expanded policy、budget 使用量、Plan fingerprint を持ち、versioned DTO へ変換できる。raw secret、pointer、function を含まない。
- `Program` が private で serialize されず、dense index と typed call path を持つ。public API から取り出せない。
- 同じ normalized Job、Catalog、input snapshot、platform snapshot なら、catalog insertion order、map seed、並行 Compile の完了順を変えても同じ Plan fingerprint になる。
- M4 の runtime-free 経路が Normalize/Bind → Shape/Compile → Solve/Validate → Describe/Build の順を明示し、operator や output transaction を開かない。dry-run が output を作成・truncate しない。Provider session の Acquire、共有 Probe、Format Inspect は M6 がこの前段へ追加する。
- M3 が declaration に留めた `access.Provider` と `endpoint` が planner に binding され、manifest が宣言した capability の不足を Open 後の type assertion ではなく Compile diagnostic にする。実 session capability の再検証は M6 が担当する。
- **walking skeleton が planner 経由で通る。** M3 の直結 test が planner の作る `Plan`/`Program` 経由に置き換わり、bytes、item 数、順序、timestamp が同じであることを検査する。
- **container を持たない実 PCM が通る。** trivial component は自分で作った要求にしか答えないため、skeleton だけでは contract が現実の規格に耐えるか分からない。raw PCM を最初の実 codec として planner 経由で流し、sample format、bit depth、channel layout、endian が `property.Set` と `stream.Descriptor` で表せること、Format が capability alternative を宣言して narrow view を受け取れること、Parser が identity として振る舞えることを実データで確認する。ここで判明した contract の不足は M5 と M6 の前に直す。M6 はこの PCM へ WAVE container を足す。
- **合成 filter chain で audio.md の設計仮定を測る。** [audio](audio.md#benchmark-contract) の受け入れ条件のうち「compatible な N filter region の sample format conversion が入口/出口の最大二回で N に比例しない」は、実 filter が揃う M8 まで検証できない。M4 では合成 filter を N = 1/4/16 で並べ、planner が挿入した converter の数と Plan 上の selected sample schema を数える。数が N に比例するなら converter 配置の設計が誤っているので、runtime を積む前に直す。速度ではなく挿入数を見る gate であり、[performance](performance.md#開発時の性能回帰方針) の 2 倍目安とは別である。
- erased schema descriptor と typed component registration から typed edge を組み立てる方式を確定する。M3 の仮 `schema.Queue`/`Fanout` は M5 で削除し、`plugin.WithReader` / `WithProcessor` / `WithWriter` が捕捉した `schema.Type[T]` から bounded edge と fan-out を runtime 内部で作る。planner は descriptor identity/payload と execution binding の一致を Open 前に検査する。
- **新規 export ごとに、呼び出し元を示すか、宣言のみとして [scope](scope.md) の分類節へ consumer を作る milestone とともに記載する。** どちらもできない export を残さない。
- 上記を unit/property test で検査する。公式 plugin を import しない。determinism は map iteration、catalog insertion、候補評価の完了順を意図的に乱して検査する。

M4 では次を未完了事項として残す。Provider session の acquire、共有 probe、Format inspect、実 capability の再検証、spool insertion と実 container/Format/Codec の駆動は M6。execution island、ownership の実行、queue と backpressure、cancel 伝播、Finalize/Commit、observability は M5。multi-stream の mapping と selector 解決は M7 が MP4 を consumer として確定する。variant selection の実装は M8 の family 移行に乗せる。

## 文書全体の完了条件

この節は planner contract の最終状態を示す gate であり、M4 単独の完了判定には上記「M4 完了条件」だけを用いる。

- semantic transformation が `Compile` にだけ実装され、plan 用と実行開始時の変換が重複しない。
- 自動挿入がすべて Plan に理由・入力・出力・effect とともに現れ、利用者が要求していない content transformation を planner が発明しない。
- 無指定出力で copy/remux が優先され、不可能な場合の codec 選択理由が diagnostic に出る。
- 候補探索が component instance を作らず、budget 内で決定的に終わる。
- 同じ入力 snapshot から同じ Plan fingerprint が得られ、`auto` 解決の差は入力 snapshot の差として説明できる。
- unsupported と budget exhaustion が区別され、最も近い unmet Need が示される。
- planning cost が component 数に対して索引で抑えられ、代表 catalog size で benchmark されている。
