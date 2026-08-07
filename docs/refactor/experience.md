# 三者の体験

拡張性は API の自由度だけで評価しない。通常の利用者、plugin 開発者、core 開発者のそれぞれに complexity budget を設ける。

> この文書の code は**目標の形**であり、現在の API と一致するとは限らない。実際に動く code は各 package の `Example` 関数を正本とする。文書に code を直書きすると API 変更で静かに嘘になるため、helper が目標形に追いついた時点で例を Example 関数へ移し、この文書からは参照する。M2 の `plugin/example_test.go` がその形である。
>
> この規則は設計文書全体に適用する。実装済み package を説明する Go code block は Example への参照へ置き換える。M4-1c で全 foundation package へ Example を用意し、以後は package を実装した milestone がその package の文書 code block を Example へ移す。未実装 contract の概念例は残してよい。

## 利用者

### 最短経路

公式 binary の利用者は plugin composition を意識しない。library の標準利用も一つの constructor で始められる。

```go
h, err := standard.NewHost()
```

入力と出力だけを指定した job は、入力 format、codec、stream mapping、metadata を可能な限り維持する。暗黙に全 stream を audio へ変換したり、未対応 stream/tag を黙って捨てたりしない。

path/URL は standard composition の Access Provider が解決し、既存 `io.Reader`/`io.Writer` を使う library 利用者は `Own`/`Borrow` adaptor で直接渡せる。custom storage のために必ず Provider plugin を実装させない。

### progressive disclosure

1. path/URL と output を指定
2. 必要な場合だけ codec/filter を指定
3. stream selector/mapping
4. Fast/Stable/Portable/Realtime preset
5. component の個別 config
6. accuracy/reproducibility/implementation/continuity/resource の詳細 policy
7. custom plugin Set/Host

下位段階を使わなければ上位概念を理解しなくてよい。

### 説明可能性

実行前の `Plan` で次を確認できる。

- input/output stream mapping
- copy/transcode と選択 codec/format
- 自動挿入された parser/converter/filter
- plugin/component identity と alias
- requested preset、実効 policy、selected implementation variant
- numerical/reproducibility contract と execution signature
- metadata conversion/loss warning
- seek/spool/temporary output
- Access Provider/Endpoint、snapshot、transaction/rollback class
- resource/latency estimate

error は「候補がない」だけでなく、どの port/property/capability が満たせないかを示す。

### 操作上の保証

- cancel が I/O、queue、component、temporary output へ伝播する。
- local file は transaction として commit される。
- warning と result は CLI/WASM/HTTP で同じ意味を持つ。
- default observation は安価で、詳細計測は opt-in。
- third-party plugin を使ったことと provenance を Plan/result から追跡できる。

## plugin 開発者

### 最小 component

plugin/component の衝突しない文字列 ID を考えず、marker type と config、Processor を定義する。

```go
type pluginID struct{}
type gainID struct{}

type Config struct {
    Gain float64
}

var Gain = plugin.Processor[gainID](
    audio.Frames,
    audio.Frames,
    config.Schema[Config](),
    newGain,
)

func Plugin() plugin.Definition {
    return plugin.Define[pluginID](Gain)
}
```

通常 Processor は次を実装しなくてよい。

- global registration
- collision-free string ID
- goroutine/channel
- scheduler
- manual `Release`
- CLI flag parser
- WASM/HTTP DTO
- metrics aggregation
- candidate routing

### 高度な component

codec、parser、mixer、session/device Endpoint 等は Operator、Shape、Host Tasks、Resource Request、Finalize を段階的に利用できる。簡単な component と別 runtime/API family にはしない。object storage の開発者は typed media を扱わず `access.Provider` と Source/Sink conformance だけを実装できる。

### 開発用の保証

- config schema から validation/default/CLI/wire description が得られる。
- `testkit.Plugin` で identity、Compile purity、lifecycle、ownership、cancel を検査できる。
- format/codec/metadata 固有 conformance fixture がある。
- Access Provider/Endpoint の cancel、snapshot、transaction、clock 用 conformance fixture がある。
- scalar/SIMD/variant を同じ component identity の implementation として宣言できる。
- core internal package を import せずに必要な buffer/bits/DSP utility を利用できる。
- invalid definition は利用時に消えるのではなく Host build/test で失敗する。

## core 開発者

### 変更可能な内部

public contract は schema、component、plugin Set、Job、Plan、Host に限定する。次は private に保つ。

- candidate index と solver
- dense node/edge layout
- queue implementation
- execution island/fusion
- scheduler
- pool/allocator
- metrics accumulator
- panic boundary

channel-per-node を fused pipeline や別 scheduler へ変えても plugin を変更しない。

### 一つの source of truth

- semantic transformation: `Compile`
- runtime construction: `Open`
- config: config schema
- component set: immutable Host catalog
- execution description: `Plan`
- external reporting: diagnostic/event snapshot

Transform/Start、core/SDK、CLI/runtime 等に同じ判断を重複実装しない。

### repository experience

- contract と全公式 plugin/surface/integration test を atomic に変更できる。
- package は責務で分け、互換 shim を残さない。
- cross-plugin dependency は Binding/standard/integration に集約する。
- root command から generate/test/license/API を検証できる。
- 代表 benchmark で大きな architecture regression を検出し、必要時だけ paired comparison/profile で原因を調べられる。
- test/native dependency は production graph と分離される。

## complexity budget

| 機能 | 通常利用者 | plugin 開発者 | core 開発者 |
|---|---|---|---|
| 標準 plugin 集合 | 見えない | definition を返す | standard composition を管理 |
| identity | alias を見る | marker type 一つ | canonicalization/duplicate 検査 |
| concurrency | cancel/progress のみ | 通常は意識しない | scheduler/task group |
| ownership | 意識しない | borrowed Input/Emitter | pipe、fan-out、drop |
| I/O | path/URL/handle を渡す | Provider/Endpoint だけ capability を宣言 | prepared session、probe、transaction |
| routing | Plan を確認 | Compile で変換を一度宣言 | solver/graph |
| metadata loss | warning を受ける | Mapping/Encoding を宣言 | report/strict policy |
| performance | preset を選ぶ | variant/traits を宣言 | specialize/fuse/measure |
| surface | CLI/library を選ぶ | 依存しない | Host DTO/event contract |

新機能によってこの表の左列・中央列へ core 内部の複雑性が漏れる場合、API を追加する前に設計を見直す。

## 受け入れ test

三者の体験を主観だけにしない。

- 初めての利用者が input/output だけで same-format/same-codec job を実行できる。
- custom host example が通常の短い Go `main` で完結する。
- third-party fixture が marker、config、Processor、test 一つで追加できる。
- third-party video/subtitle schema を core の switch/enum 変更なしで通せる。
- third-party Access Provider と realtime Endpoint を surface/core 無変更で追加できる。
- scheduler/queue の代替実装で公式 plugin source を変更しない。
- CLI/WASM/HTTP が同一 Job から同一 Plan を得る。
- observation off の linear path が hot-path 性能契約を満たす。

## M6 完了条件

M6 は最初の実 container 経路が動く milestone であり、利用者と plugin 開発者の体験が初めて実測できる時点である。この節はその 2 者を対象にする。core 開発者向けの gate は各領域文書の完了条件が担う。

M5 の切断により、M6 の開始時点で repository には利用 surface が存在しない。したがって M6 は library と CLI の最短経路を新しく書く。全 flag、WASM、web editor への拡張は M9 が担当する。

- **利用者の最短経路が一行である。** [surfaces](surfaces.md#最短経路の-convenience) の `standard.Convert(ctx, "in.wav", "out.wav")` 相当が動く。path から Reference への解決は convenience が行い、URL 構文を要求しない。この convenience は `Job` を組み立てて同じ `Host` を呼ぶだけとし、別経路の planner や既定を持たない。
- **`cmd/godec` が新 Host の上で動く。** WAVE/PCM の範囲に限り、入力と出力を指定した変換が公式 binary から実行できる。`cli.Run(ctx, h, args)` の形で `Host` を注入され、CLI layer に planner、registry、plugin factory を持たない。この時点の CLI が持つのは入出力指定、`Plan` preview、progress 表示、cancel、exit code の分類だけでよい。
- **2 段目へ連続的に移行できる。** codec 指定、filter、mapping、policy、custom Set へ進む時に、1 段目で書いた code を捨てずに `Job` を露出させて拡張できる。[progressive disclosure](#progressive-disclosure) の段差が「作り直し」にならない。
- **plugin 開発者の最小 component を実測し、目標との差を記録する。** M6 で実 WAVE/PCM component を書いた時点で、gain 相当の最小 processor に必要な概念数を数える。[最小 component](#最小-component) が目標とする水準に対して差が大きい場合、helper を追加するか、追加しない理由を記録する。放置しない。
- **通常 Processor が実装しなくてよいものを実際に実装していない。** global registration、衝突しない文字列 ID、goroutine/channel、scheduler、手動 `Release`、CLI flag parser、wire DTO、metrics 集約、candidate routing のいずれも、公式 WAVE/PCM component の source に現れない。
- **識別子を手で考えさせない。** 公式 plugin を書く過程で、第三者が一意性を保証しなければならない文字列が新たに必要にならなかったことを確認する。必要になった箇所は marker 由来へ移すか、一意性が不要である理由を記録する。
- **error が最初の利用者を助ける。** 存在しない component selector、範囲外の config 値、満たせない mapping に対し、最も近い候補または有効な範囲が示される。[config](config.md#validation-と-diagnostic) の構造化 diagnostic が実 plugin でも成立する。

## M9 完了条件

M9 は M6 で書いた最短経路を全 surface へ広げる milestone であり、三者の体験が揃う時点である。移行ではなく完成であり、library、CLI、WASM、demo が同じ Host/Job/Plan/Result を使う状態にする。

- **[受け入れ test](#受け入れ-test) の各項目が自動 test として存在する。** 特に「初めての利用者が input/output だけで same-format/same-codec job を実行できる」「custom host example が通常の短い Go `main` で完結する」「third-party fixture が marker、config、Processor、test 一つで追加できる」を実行可能な形にする。
- **[complexity budget](#complexity-budget) の左列と中央列が実測で満たされている。** 通常利用者が cancel/progress 以外の並行性を意識せず、plugin 開発者が ownership、scheduler、surface DTO を書かない。表の項目ごとに、実際の利用側 code を根拠として示す。
- **同じ Job から CLI、WASM、library が同じ Plan を得る。** 表現だけが違い、既定や解決結果が surface ごとにずれない。
- **説明可能性が surface に届く。** [説明可能性](#説明可能性) の一覧が `Plan` から実際に読め、CLI と WASM の両方で表示できる。
- **利用者向け文書が新経路と一致する。** README と godoc の example が現在の API で compile し、旧 global registry や audio-only model を示さない（[F45](findings.md)）。
- **新規 export ごとに、呼び出し元を示すか、宣言のみとして [scope](scope.md) の分類節へ consumer を作る milestone とともに記載する。**

## 文書全体の完了条件

この節は三者の体験の最終状態を示す gate であり、個別 milestone の完了判定には各 milestone 固有の条件を用いる。

- 通常利用者が input/output だけで開始でき、必要になった段階だけ上位概念を学べる。
- 実行前に `Plan` で何が起きるかを確認でき、error がどの port/property/capability を満たせないかを示す。
- plugin 開発者が marker、config、Processor と test だけで component を追加でき、衝突しない文字列 ID を考えない。
- 高度な component が同じ port/schema/lifecycle の上で段階的に機能を足せ、別の runtime/API family にならない。
- core 開発者が scheduler、queue、allocator、fusion を交換しても公式・第三者 plugin の source を変更しない。
- complexity budget 表の左列と中央列へ core 内部の複雑性が漏れていない。
