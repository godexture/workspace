# 品質と検証

この文書は test の層、public testkit、repository-wide CI、generator/test runner の方針を定義する。数値・性能の判定は [performance](performance.md)、corpus は [fixtures](fixtures.md)、build/license/release は [supply](supply.md)、browser 固有 test は [web](web.md) を正本とする。

## 基本方針

大規模な置換では、旧 API との互換性ではなく次の contract を固定する。

- media semantics と default mapping
- graph/type/lifecycle correctness
- ownership、cancel、rollback、cleanup
- plugin openness と Host isolation
- performance/reproducibility
- pure-Go production boundary
- wire compatibility
- artifact provenance

test-only CGO、FFmpeg、native reference implementation は production purity を損なわない。ただし foundation/standard の通常 dependency と default test commandには混ぜず、明示的な integration/reference tier へ隔離する。

## M0: 実装前の baseline

リファクタリング開始前に、現行の代表経路を凍結する。

1. WAVE/PCM、MP3、FLAC の decode/encode/roundtrip。
2. stream passthrough と metadata preservation。
3. cancel、invalid input、Finalize/Close failure。
4. 1/4/16段の軽量 audio filter chain。
5. observation off/on の allocation、CPU、block/goroutine profile。
6. scalar/SIMD と worker 1/N の意味上の差。

絶対時間だけを将来 gate にしない。同じ process で旧新を交互に測る paired benchmark、allocation、correctness counter、profile を保存する。M0 の詳細な測定対象は [performance](performance.md) に従う。

## test の層

| 層 | 主な対象 | dependency |
|---|---|---|
| foundation unit/property/fuzz/race | identity、schema、config、time、graph、ownership、planner、runtime | foundation のみ |
| plugin conformance | component lifecycle と宣言 contract | public `testkit` |
| plugin 固有 | codec/format/metadata/algorithm の仕様 | 対象 plugin |
| integration | 公式 plugin 間、CLI、WASM、reference implementation | 最上位 integration module |
| surface end-to-end | library/CLI/browser/demo の利用経路 | 対象 distribution |

small hermetic fixture、cross-plugin fixture、full conformance corpus、benchmark corpus を同じ directory/command に混在させない。詳細な tier と manifest/cache は [fixtures](fixtures.md) を参照する。

## foundation test

最低限、次を unit/property/fuzz/race test する。

- marker identity、duplicate、explicit override
- config default/preset/patch、canonicalization、fresh snapshot
- typed schema registration と unknown schema
- checked timestamp rescale、overflow、rounding
- graph schema/multiplicity/cycle/reachability/finalizer validation
- planner の canonical ordering、budget、same-input Plan fingerprint
- ownership move、fan-out、write failure、drop、cancel drain
- allocator の zeroed/overwrite lease と Job 終了時の解放
- transactional Open、reverse rollback、Finalize/Commit outcome
- primary failure と cleanup failure の集約
- metadata raw preservation、Mapping、loss report
- codec/metadata Binding conflict
- 複数 Host の catalog/CPU/resource/default state isolation
- wire version、unknown field、size/depth limit

map iteration、catalog insertion、goroutine timing、candidate evaluation completion order を意図的に乱す。`auto` resource が異なる snapshotへ解決された場合は、同じ Plan と誤認せず fingerprint 差として検証する。

## public plugin testkit

第三者と公式 plugin が同じ public testkit を使う。

```go
func TestPlugin(t *testing.T) {
    testkit.Plugin(t, acme.Plugin())
}
```

共通 contract:

- identity、descriptor、config schema
- `Compile` purity/repeatability と bounded `Suggest`
- selected component だけを `Open` する lifecycle
- cancel、EOF、Flush、Finalize、Close
- ownership leak、double drop、declared schema と実 item
- variant accuracy/repeatability/platform declaration
- panic/error boundary
- empty、truncated、oversized input

専門 testkit:

| 対象 | 追加 contract |
|---|---|
| Format | bounded Probe、capability alternative、unknown carrier、topology |
| Codec/Parser | chunk boundary、invalid packet、delay/Flush、timestamp、final parameters |
| Metadata Encoding | parse/marshal、duplicate/order、unknown raw、Mapping/loss |
| Access Provider | capability、snapshot/retry、Own/Borrow、commit/abort |
| Endpoint | clock、overflow/underrun、cancel/join、reconnect/topology event |

plugin author が scheduler、queue、manual `Release`、surface DTO を再実装しなくても contract を検証できることを testkit の usability gate にする。

## integration

foundation が公式 plugin を importして testする構成をやめ、dependency graph の最上位に integration module を置く。

```text
integration/
├─ wave_pcm
├─ mp3
├─ flac
├─ metadata
├─ multistream
├─ cancellation
├─ cli
└─ wasm
```

- native/CGO/FFmpeg reference は integration または明示 reference tier に置く。
- third-party 相当の video/subtitle/custom schema fixture を含める。
- small fixture だけを repository/module に置き、full corpus は外部 manifest/cache を使う。
- foundation と plugin 固有 unit test は integration asset に依存しない。

## generator

### config generator

`tools/internal/config-generator/generator/options.go` の単一 preset 分岐は、constructor が

```go
func NewX(...) (X, error)
```

を返すにもかかわらず、

```go
*c = NewX(WithPreset(level))
```

を生成する。これは compile 不能であり、既存生成物との差がある場合は generator source の bug/drift である。

目標設計ではこの generator を恒久修理して残さない。typed [config contract](config.md) へ移した plugin から、次を同時に削除する。

- `tools/cmd/config-generator`
- `tools/internal/config-generator`
- config 用 `go:generate`
- checked-in `config_options.go`

移行中に再生成が必要な場合だけ bug を最小修正し、temporary package の compile test を付ける。廃止予定 generator の大規模 test suite は作らない。

### 残す generator

enum/table/SIMD data/wire codec 等、生成に意味があるものは次を満たす。

- 入力・tool version・対象順序が固定される。
- 同じ入力から同じ出力を得る。
- golden だけでなく生成先を compile/typecheck する。
- partial output を publishせず、失敗を集約する。
- checked-in artifact drift を CI で検出する。
- 生成元と license/provenance を記録する。

## test runner

現在の runner は `bufio.Scanner` の既定64 KiB token 上限に依存し、`scanner.Err()` を run failure として十分に扱わない。次を修正する。

- streaming decoder または明示的で検証済みの token limit
- process failure と output parse failure の分離
- module/test/variant identity を持つ structured result
- cancel 時の child process tree 停止
- malformed/partial output fixture
- 64 KiB超の failure fixture
- partial success を全体成功にしない exit status

root runner は test semantics を独自に再実装せず、manifest から必要な module/variant command を orchestration する。

## repository-wide CI

| Gate | 検証内容 | 詳細 |
|---|---|---|
| structure | dependency direction、module graph、Git submodule不在、API snapshot | [architecture](architecture.md) |
| build | supported target、CGO-off standard、semantic build tag | [supply](supply.md) |
| correctness | unit/property/fuzz/race、integration、failure injection | この文書 |
| performance | scalar/SIMD differential、worker/chunk variation、paired benchmark | [performance](performance.md) |
| corpus | small/full tier、digest、license、size budget | [fixtures](fixtures.md) |
| wire/browser | Go/TS compatibility、real browser lifecycle | [web](web.md) |
| generation | deterministic output、compile、clean tree | この文書 |
| supply/release | network-off build、SBOM/NOTICE/provenance、release plan | [supply](supply.md) |
| documentation | link、snippet compile、public package/example consistency | [experience](experience.md) |

CI matrix は root の machine-readable manifest から生成し、skip、未実行、失敗を区別する。`./test-runner.exe --simd` 一回だけを repository 全体の成功条件にはしない。

## performance と reproducibility の検証

この文書では test の配置だけを定める。exact/bounded、Repeatable/Variable、Stable/Portable、variant selection、execution signature、benchmark採用条件は [performance](performance.md) を正本とする。

最低限、公式 optimized variant は reference/scalar との differential testを持ち、lossless codec は logical output exact、Stable は同じ signature の artifact exact、Portable は宣言 domain の cross-target artifact exact を検査する。`Fast` でも parser、CRC、timestamp、ordering、item count、bounds validation を緩めない。

## 完了条件

- M0 baseline が意味・allocation・profile・paired benchmark を含む。
- foundation の通常 test が公式 plugin/native/full corpus を必要としない。
- 公式・第三者 plugin が同じ public testkit を利用する。
- test-only CGO/reference dependency が production/standard graphへ入らない。
- config generator/生成 options が削除され、残す generatorだけが deterministic/compile gateを通る。
- test runner が長い/malformed output、process crash、cancel、partial resultを正しく失敗として扱う。
- root CI が structure、build、correctness、performance、wire、corpus、supply、docs の結果を一つの reportに集約する。
- skipped/未実行 gate を repository success と扱わない。
