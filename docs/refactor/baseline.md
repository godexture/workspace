# M0 baseline artifact

この文書は M0 で固定した再現可能な比較条件の正本である。生の pprof/benchmark 出力は Git へ蓄積せず、再現手順、toolchain、意味上の正しさ、長く有効な所見だけを残す。script/CI 向けの入力は [baseline.manifest.json](baseline.manifest.json) を正本とする。

## 固定した commit

- baseline commit: `4429711a88481e6643ea2427a8192938737b1e9e`
- 選定理由: filter chain benchmark の lifecycle 境界、frame ownership、pipeline/direct-call の入力条件を揃えた M0 最終実装を含む。
- repository は 2026-08-01 の monorepo 統合（M1）および M1-1 の `plugin/<family>` 最終 path 移行後の状態。旧 16 submodule 構成・旧 `plugins/codec-*`/`plugins/format-*` path の baseline ではない。

## toolchain（[baseline.manifest.json](baseline.manifest.json) が正本）

| 項目 | 値 |
|---|---|
| Go | `go1.26.4 windows/amd64` |
| CPU | `13th Gen Intel(R) Core(TM) i7-13620H`（絶対時間の参考値。AGENTS.md の方針により過去 machine との単純比較はしない） |
| CPU feature | `avx2=true`、`avx2fma=true`。`sdk/dsp.HasAVX2`/`HasAVX2FMA` が SIMD 分岐の gate であり、これらが false の machine では `simd` variant も scalar path と同じ経路になる |
| GOWORK | repository root `go.work`（root module + `tools`、`bindings/wasm`、`example/go`、`example/web/server` の4 nested module） |
| build mode | 必須: scalar（既定 `GOEXPERIMENT` なし）、SIMD（`GOEXPERIMENT=simd`）の2種。任意診断: SIMD 内 forced-scalar（`GODEC_FORCE_SCALAR=1`、`sdk/dsp` の scalar dispatch path を SIMD build 内で検査する追加手段であり、baseline 再現の必須条件ではない） |
| worker | `registry.NewWorkerPool(N)`、N ∈ {1, 4, 16} を明示指定（`auto` は使わない） |
| package 数 | baseline commit 時点で 104（`go list ./...`、root module のみ。nested module 4件は別途 `go list` する）。後続 milestone が package を追加するため、HEAD で再取得した数と一致しなくてよい |
| input generator | `stereoBlock`（`plugin/audio/filters_bench_test.go`）は固定 seed の `math/rand/v2` PCG generator を使い、同じ size tier に対して常に同一 byte 列を生成する。仕様は manifest の `inputGenerators` を正本とする。FLAC/observation paired benchmark の fixture はいずれも hardcode/固定 size で乱数を使わない。 |

## 使い方と判定基準

manifest の command は M0 baseline 全体を再現するためのもので、日常変更の一律な必須 gate ではない。

- 日常は変更した package と直接影響する経路の correctness test を実行する。
- 性能に影響し得る変更では、まず代表 case を短く測る。同一条件で総時間または無視できない allocation が概ね 2 倍以上へ悪化した場合だけ、再測定と原因調査へ進む。
- 2 倍未満の timing 差や過去 machine の absolute timing は gate にしない。`processing-ns/op` は lifecycle の内訳を読む診断値であり、hard gate には使わない。
- full benchmark matrix と profile は baseline 更新、milestone/release、optimized variant の採用、runtime architecture の大幅変更、回帰調査に限定する。

たとえば filter chain の日常 smoke は、変更に近い1点だけを短く実行できる。判定には標準の `ns/op`/allocation を使う。

```bash
go test ./plugin/audio -run '^$' -bench '^BenchmarkGainChainPipeline$/^4stage$/^Medium$' -benchtime=200ms -count=2
```

詳細は [quality.md](quality.md#開発時の検証-tier) と [performance.md](performance.md#開発時の性能回帰方針) に従う。

## 再現手順（human-readable summary の実行 command）

正確な argv/env は [baseline.manifest.json](baseline.manifest.json) の `commands` を正本とする（shell string ではなく構造化済みなので、記録した OS 以外でも shell 構文の違いを気にせず実行できる）。以下は human-readable な要約。

baseline の source は固定した commit にだけ存在する。現在の HEAD worktree や `_legacy/` から command を実行せず、既存 worktree を checkout/reset しない。別の一時 directory に detached worktree を作り、その root で以下の command を実行する。対応する machine-readable argv は manifest の `executionRoot.setupArgv` である。

```bash
git worktree add --detach <temporary-worktree> 4429711a88481e6643ea2427a8192938737b1e9e
cd <temporary-worktree>
```

個別 build の正しさ（必須2種 + 任意診断1種）:

```bash
go build ./... && go test ./...                                      # scalar (required)
GOEXPERIMENT=simd go build ./... && GOEXPERIMENT=simd go test ./...  # SIMD (required)
GOEXPERIMENT=simd GODEC_FORCE_SCALAR=1 go test ./...                 # SIMD build, forced scalar dispatch (optional diagnostic)
```

代表 benchmark（paired、allocation、worker/block/depth 別）:

```bash
go test ./core/pipeline/... -bench '^BenchmarkPipelineObservationPaired64MiB$'
go test ./plugin/audio -bench '^BenchmarkGainChainPipeline$'        # reports processing-ns/op alongside built-in ns/op
go test ./plugin/audio -bench '^BenchmarkGainChainPipelineOpen$'
go test ./plugin/audio -bench '^BenchmarkGainChainDepths$'          # same workload shape as the pipeline benchmark
go test ./plugin/flac/internal/codec/decoder/... -bench '^BenchmarkParallelDecodeThroughput$'
```

CPU/block/heap profile（observation off の例。末尾の subbenchmark 名は `Plain`、`Progress`、`Metrics` に差し替えられる）:

```bash
go test ./core/pipeline/... -run '^$' -bench 'BenchmarkPipelineObservation/64MiB/Off$' -benchtime=20x \
  -cpuprofile=cpu.prof -blockprofile=block.prof -memprofile=heap.prof -blockprofilerate=1
go tool pprof -top cpu.prof
```

goroutine leak baseline:

```bash
go test ./core/pipeline/... -run TestPipelineObservationDoesNotLeakGoroutines -v
```

## 意味上の正しさの結果

- WAVE/PCM/MP3/FLAC の代表 decode/encode/roundtrip test は green である。
- FLAC の parallelism 1/8 と convolver の worker 1/4/16 は semantic output が不変である。PCM codec と MP3 decoder は並列実装を持たないため比較対象がない。
- WAVE の truncated/invalid input は独立した payload expectation で検査する（`plugin/wave/internal/truncated_test.go`）。
- mux/demux I/O、Flush、Finalize、`SetMetadata`、`AddStream` の代表 failure と cleanup は failure-injection test で固定した。
- codec 省略時も decoder/encoder を開く現行挙動と、WAVE metadata の multi-value order、duplicate、unknown loss、raw chunk preservation を `sdk/conversion/passthrough_test.go` で固定した。

## allocation / profile 所見

- observation benchmark は plain/off/progress/metrics を同一 process で比較でき、goroutine leak test は全 mode で green である。小さな timing 差は baseline gate としない。
- filter chain benchmark は construction/Prepare、steady-state processing、Run 全体を区別する。標準 `ns/op`/allocation を粗い回帰検出に使い、`Snapshot().Elapsed` 由来の `processing-ns/op` は内訳の診断にだけ使う。
- pipeline と direct-call benchmark は depth、block size、frame 数、Encode/Decode 境界を揃えている。ただし pipeline は複数 frame を並行実行できるため、両者の差は純粋な abstraction overhead ではなく、scheduling overhead と並行実行利得の純計である。
- FLAC parallel decode benchmark は worker 数による throughput/allocation の傾向を確認する診断用で、単発の小差を gate にしない。

## 既知のギャップ（M0 完了時点で未解消、後続 milestone へ）

- Close 自体が失敗する format node は現行実装にない。新 lifecycle で失敗可能になる場合に contract test を追加する。
- open `metadata.Document` と WAVE 以外の unknown/raw/duplicate/order は M3 で一般化する。
- PCM/MP3 に並列 variant を追加した時点で worker 不変性 test を追加する。
- large corpus の tier/digest 管理は [fixtures.md](fixtures.md) に従い M10 で行う。

## 完了条件との対応

[quality.md](quality.md#m0-完了条件) の6項目は対象 test/benchmark で満たしている。scalar/SIMD/worker の semantic diff は対象 package の test で検証し、repository 全体の `tools/cmd/differential` には依存しない。raw profile と時系列 timing は保存せず、必要時に manifest から再取得する。
