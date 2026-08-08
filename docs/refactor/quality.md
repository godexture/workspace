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

## 開発時の検証 tier

検証は変更範囲とリスクに合わせ、日常開発の待ち時間を full repository gate に固定しない。

- 日常の変更では、変更した package と直接影響する contract の correctness test を優先する。無関係な module、browser、full corpus まで毎回実行しない。
- M5 の切断以降、`go build ./...` と `go test ./...` の対象は新 stack だけになる。`_legacy/` は `_` 始まりの directory として go tool が無視するため build も test もされない。移植参照として読む対象であり、検証対象にしない。
- benchmark/profile は hot path、allocation、並行処理、runtime 構造に影響し得る変更、または性能回帰の調査時だけ行う。代表 case による短い smoke から始める。
- milestone の完了確認、release 前、広範な contract/module 変更では repository-wide gate を実行する。新しい optimized variant の採用時は、その variant に必要な differential/benchmark を追加する。
- 選択した gate の失敗や skip は成功扱いにしない。一方、日常 tier で選択していない full tier は「未検証」と記録すればよく、日常 job 自体を失敗にしない。

性能の判定基準は [performance](performance.md#開発時の性能回帰方針) を正本とする。correctness、resource leak、panic、data race は性能の許容幅とは無関係に失敗である。

## M0: 実装前の baseline

リファクタリング開始前に、現行の代表経路を比較の基準点として記録する。

正式 release 前のため、新経路が現行と同じ出力を返すことは要求しない。より良い API や挙動へ変えてよい。したがってこの baseline は保存契約ではなく、差異を見つけた時に「意図的な変更か bug か」を判断するための diagnostic である。意図的な変更は [capability](capability.md#挙動変更の記録) へ記録し、記録のない差異は回帰として原因を調べる。新経路の正しさそのものは、旧実装ではなく仕様と conformance corpus で確認する。

1. WAVE/PCM、MP3、FLAC の decode/encode/roundtrip。
2. input metadata の output への伝播と、現行 stream 経路。現行実装に stream copy がなければ、decoder/encoder を開く事実を baseline として記録する。
3. cancel、invalid input、Finalize/Close failure。
4. 1/4/16段の軽量 audio filter chain。
5. observation off/on の allocation、CPU、block/goroutine profile。
6. scalar/SIMD build 間と worker 1/N の意味上の差。

M0 では入力仕様・digest、実行 command、toolchain、correctness summary、allocation、profile summary を保存する。raw profile/result は Git に蓄積せず CI artifact とする。比較可能な現行 variant は同じ process で交互に測り、リファクタリング後も同じ fixture/harness を再利用できるようにする。この full baseline の再取得は baseline 更新や回帰調査のためのもので、日常変更の必須 gate ではない。詳細な測定対象は [performance](performance.md) に従う。

### M0 完了条件

- WAVE/PCM、MP3、FLAC の代表 decode/encode/roundtrip が small hermetic fixture で再現できる。
- metadata の既知項目と opaque/raw 項目について、現行の伝播・欠落挙動を検査する。stream copy 自体の実装は M7 の完了条件とする。
- cancel、invalid/truncated input、Finalize/Close と primary+cleanup failure の集約を、代表 pipeline と format lifecycle で検査する。
- 1/4/16段 filter chain は実 pipeline と同じ workload shape の sequential direct-call 経路を分け、cold construction と steady-state processing を分けて測る。並行実行条件が異なるため、direct-call を厳密な overhead 下限とはみなさない。
- observation off/on の allocation と CPU/block/goroutine profileについて、再現 command、入力、correctness counter、要約を保存する。
- 同一入力に対する scalar/SIMD 実装間の semantic output と、worker 1/N の output/order/count を、対象 package ごとの differential test で検査する。repository 全体を横断する単一 gate は要求しない（実行コストが高く、実用的な baseline/CI gate にならないため）。
- baseline manifest と test/benchmark source だけで比較条件を再構成でき、特定開発者の未追跡 file や手順に依存しない。

## test の層

| 層 | 主な対象 | dependency |
|---|---|---|
| foundation unit/property/fuzz/race | identity、schema、config、time、graph、ownership、planner、runtime | foundation のみ |
| plugin conformance | component lifecycle と宣言 contract | public `testkit` |
| plugin 固有 | codec/format/metadata/algorithm の仕様 | 対象 plugin |
| integration | 公式 plugin 間、CLI、WASM、reference implementation | 最上位 integration module |
| surface end-to-end | library/CLI/browser/demo の利用経路 | 対象 distribution |

small hermetic fixture、cross-plugin fixture、full conformance corpus、benchmark corpus を同じ directory/command に混在させない。詳細なtierとdata submoduleまたはmanifest/cacheによる取得方針は [fixtures](fixtures.md) を参照する。

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
├─ mp4
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
- small fixture だけを product repository/module の通常配布対象に置き、full corpus は任意取得のdata submoduleまたは外部manifest/cacheを使う。
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

この generator は恒久修理して残さず、typed [config contract](config.md) への移行後に M5 cut で次を同時に削除した。

- `tools/cmd/config-generator`
- `tools/internal/config-generator`
- config 用 `go:generate`
- checked-in `config_options.go`

切断前に再生成を必要としなかったため、廃止 generator の修理や大規模 test suite は追加していない。

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
| structure | dependency direction、module graph、source code submodule不在、data submodule policy、API snapshot | [architecture](architecture.md) |
| build | supported target、CGO-off standard、semantic build tag | [supply](supply.md) |
| correctness | unit/property/fuzz/race、integration、failure injection | この文書 |
| performance | 代表 smoke、必要時の scalar/SIMD differential、worker/chunk variation、paired benchmark | [performance](performance.md) |
| corpus | small/full tier、digest、license、size budget | [fixtures](fixtures.md) |
| wire/browser | Go/TS compatibility、real browser lifecycle | [web](web.md) |
| generation | deterministic output、compile、clean tree | この文書 |
| supply/release | network-off build、SBOM/NOTICE/provenance、release plan | [supply](supply.md) |
| documentation | link、snippet compile、public package/example consistency | [experience](experience.md) |

`documentation` gate のうち link と anchor の検査だけは M3 から実行する。設計文書が正本である以上、参照が壊れたまま milestone を跨ぐと判断の根拠を辿れなくなる。M2 の期間だけで package 数、API の形、削除済み method、descriptor の扱いの 4 件が文書と実装で drift し、anchor も複数回手で修正している。残りの snippet compile と example consistency は M10 で揃える。

CI matrix は root の machine-readable manifest から生成し、日常の change-scoped tier と milestone/release 用の full tier を区別する。各 tier 内では skip、未実行、失敗を区別する。`tools/cmd/test-runner`（`./test-runner.exe --simd` 相当）は指定した1 variant を走らせるだけで、それ単独を full repository verification の成功条件にはしない。

## performance と reproducibility の検証

この文書では test の配置だけを定める。exact/bounded、Repeatable/Variable、Stable/Portable、variant selection、execution signature、benchmark採用条件は [performance](performance.md) を正本とする。

最低限、公式 optimized variant は reference/scalar との differential testを持ち、lossless codec は logical output exact、Stable は同じ signature の artifact exact、Portable は宣言 domain の cross-target artifact exact を検査する。`Fast` でも parser、CRC、timestamp、ordering、item count、bounds validation を緩めない。

## 文書全体の完了条件

この節は品質・検証基盤の最終状態を示す。M0 単独の完了判定には、上記「M0 完了条件」だけを用いる。

- M0 固有の完了条件を満たす。
- foundation の通常 test が公式 plugin/native/full corpus を必要としない。
- 公式・第三者 plugin が同じ public testkit を利用する。
- test-only CGO/reference dependency が production/standard graphへ入らない。
- config generator/生成 options が削除され、残す generatorだけが deterministic/compile gateを通る。
- test runner が長い/malformed output、process crash、cancel、partial resultを正しく失敗として扱う。
- full tier の root CI が structure、build、correctness、performance、wire、corpus、supply、docs の結果を一つの reportに集約する。
- 選択した required gate の skip/未実行を成功扱いせず、日常 tier と full tier のどちらを実行したかを report から判別できる。
