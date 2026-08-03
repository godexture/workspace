# 三者の体験

拡張性は API の自由度だけで評価しない。通常の利用者、plugin 開発者、core 開発者のそれぞれに complexity budget を設ける。

> この文書の code は**目標の形**であり、現在の API と一致するとは限らない。実際に動く code は各 package の `Example` 関数を正本とする。文書に code を直書きすると API 変更で静かに嘘になるため、helper が目標形に追いついた時点で例を Example 関数へ移し、この文書からは参照する。M2 の `plugin/example_test.go` がその形である。

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
