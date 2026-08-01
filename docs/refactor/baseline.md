# M0 baseline artifact

この文書は、docs/refactor/checkpoint.md M0#7/M0-R5 が要求する「再現可能な比較条件」の正本である。生の pprof/benchmark 出力は [fixtures.md](fixtures.md) の方針に従い Git へ蓄積せず、この文書には再現手順・toolchain・意味上の正しさの結果・所見の要約だけを残す。toolchain と再現 command は [baseline.manifest.json](baseline.manifest.json) にも machine-readable な形で重複して持たせてあり、そちらを正本の入力として script/CI から直接読める。

## 固定した commit

- baseline commit: `924461d0e004fda41745bfcbebcdb9f1c0af03ab`
- 選定理由: 以前このファイルが固定していた `2a7ea1a` は `go.work`/`go.work.sum` が tracked になる **前** の commit で、この文書自体が要求する「clean checkout からの再現」が成立しなかった（`git ls-tree 2a7ea1a -- go.work` は空）。今回の commit は go.work が tracked 済みで、`plugins/<family>` から `plugin/<family>` への M1 path 移行も完了した後の状態であり、以下の再現手順をこの commit から直接実行できる。
- repository は 2026-08-01 の monorepo 統合（M1）および M1-1 の `plugin/<family>` 最終 path 移行後の状態。旧 16 submodule 構成・旧 `plugins/codec-*`/`plugins/format-*` path の baseline ではない。

## toolchain（[baseline.manifest.json](baseline.manifest.json) の要約）

| 項目 | 値 |
|---|---|
| Go | `go1.26.4 windows/amd64` |
| CPU | `13th Gen Intel(R) Core(TM) i7-13620H`（絶対時間の参考値。AGENTS.md の方針により過去 machine との単純比較はしない） |
| GOWORK | repository root `go.work`（root module + `tools`、`bindings/wasm`、`example/go`、`example/web/server` の4 nested module） |
| build mode | scalar（既定 `GOEXPERIMENT` なし）、SIMD（`GOEXPERIMENT=simd`）、SIMD 内 forced-scalar（`GODEC_FORCE_SCALAR=1`）の3種 |
| worker | `registry.NewWorkerPool(N)`、N ∈ {1, 4, 16} を明示指定（`auto` は使わない） |
| package 数 | 104（`go list ./...`、root module のみ。nested module 4件は別途 `go list` する） |

## 再現手順（human-readable summary の実行 command）

個別 build の正しさ:

```bash
go build ./... && go test ./...                                      # scalar
GOEXPERIMENT=simd go build ./... && GOEXPERIMENT=simd go test ./...  # SIMD
GOEXPERIMENT=simd GODEC_FORCE_SCALAR=1 go test ./...                 # SIMD build, forced scalar dispatch
```

代表 benchmark（paired、allocation、worker/block/depth 別）:

```bash
go test ./core/pipeline/... -bench BenchmarkPipelineObservationPaired64MiB
go test ./plugin/audio -bench BenchmarkGainChainPipeline
go test ./plugin/audio -bench BenchmarkGainChainPipelineOpen
go test ./plugin/flac/internal/codec/decoder/... -bench BenchmarkParallelDecodeThroughput
```

CPU/block/heap profile（observation off の例。variant を variant.md の各 mode 名に差し替えて再取得できる）:

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

- decode/encode/roundtrip（WAVE/PCM/MP3/FLAC）: 既存 test が green（`plugin/*/...` 配下の roundtrip 系 test）。
- worker/parallelism 数不変性: FLAC decoder/encoder（既存、parallelism 1 vs 8、`plugin/flac/internal/codec/decoder` 等）、convolver の partition build に加え、Engine の SendFrame/ReceiveFrame を通した end-to-end 出力そのもの（`TestEngineWorkerCountDoesNotChangeEndToEndOutput`、M0-R1 で追加、worker 1/4/16）で確認。**PCM codec と MP3 decoder は worker/parallelism 実装自体を持たず、比較対象がない**という事実を記録する。
- truncated/invalid input: WAVE で、期待される dataOffset/dataSize/payload byte 数を独立した ground truth と突き合わせる形で追加（`plugin/wave/internal/truncated_test.go`）。実際に3種の mutant（宣言 size の fabrication、有効な完全入力の誤 reject、header byte の payload への混入）を手動注入し検出を確認した上で revert 済み。
- lifecycle failure injection: WAVE/MP3/FLAC の mux/demux 各 I/O phase（`sdk/testutil/fault` 経由）、実 muxer + 実 source を組んだ pipeline レベルの「primary failure と Finalize failure の同時発生」（`plugin/wave/internal/failure_test.go`）に加え、M0-R4 で decoder/encoder の Flush 失敗（`sdk/engine/wrapper_test.go` の `TestEncoderAdapter_CloseAfterFlushError`/`TestDecoderAdapter_CloseAfterFlushError`）と、muxer の `SetMetadata`/`AddStream` 失敗時に全 node が確実に1回だけ close されること（`core/routing/negotiator_lifecycle_test.go` の `TestNegotiatorClosesAllNodesWhenMuxerSetMetadataFails`/`TestNegotiatorClosesAllNodesWhenMuxerAddStreamFails`）を追加。
- 現行 stream/metadata 経路: target codec 省略時も decoder/encoder が必ず開くこと（stream copy が存在しないこと）を固定（`sdk/conversion/passthrough_test.go`）。metadata は M0-R3 で単一 known key（title）の baseline から拡張し、multi-value の順序保持（`TestBuildPreservesOrderedMultiValueMetadataThroughOmittedCodecRoute`）、single-value の重複上書き（`TestBuildOverwritesDuplicateSingleValueMetadataThroughOmittedCodecRoute`、後勝ち）、未知 INFO tag の完全消失（`TestBuildDropsUnrecognizedMetadataThroughOmittedCodecRoute`、raw fallback すら存在しない明示的な loss）、raw chunk（`cue `）の保持（`TestBuildPreservesRawCueChunkThroughOmittedCodecRoute`）を固定した。

## allocation / profile 所見

- `BenchmarkPipelineObservationPaired64MiB`（core/pipeline）: plain/off/progress/metrics を同一 process 内で交互実行する paired 比較を持つ。baseline commit 上の実測では metrics-vs-off `-3.4%`、off-vs-plain `+2.7%`、progress-vs-off `-3.1%` で、observation の有無による系統的な悪化は見られない（絶対値は machine 依存のため比率のみを参照する）。
- `TestPipelineObservationDoesNotLeakGoroutines`: plain/off/progress/metrics いずれも green。
- `BenchmarkGainChainPipeline`（plugin/audio）: M0-R2 で construction を `b.StopTimer`/`b.StartTimer` により計測対象から除外し、steady-state Run のみを計測するよう修正済み。baseline commit 上の実測（Small block）では 1 段 7968B/106 allocs 〜 16 段 28166B/678 allocs。`BenchmarkGainChainPipelineOpen` は construction/Open 単体のコストを引き続き分離して持つ（1 段 7772B/40 allocs 〜 16 段 59377B/267 allocs）。
- `BenchmarkParallelDecodeThroughput`（plugin/flac/internal/codec/decoder）: parallelism 1/4/16 で処理時間はおおむね短縮し、allocs/op はわずかに増加する（1: 277 allocs、4: 290 allocs、16: 309 allocs）。

## 既知のギャップ（M0 完了時点で未解消、後続 milestone へ）

- Finalize/Close failure injection は WAVE を中心に代表実装した。MP3/FLAC は mux 側の主要 phase をカバーするが、node Close 自体が全 format で no-op のため「Close 自体が失敗するケース」は実質的に検証対象が存在しない（F50 と同根の現状把握）。
- metadata の unknown/raw/duplicate/order は M0-R3 で WAVE 経路について固定したが、一般化された open metadata.Document 契約は M3 待ち。他 format（FLAC/MP3/ID3）への同種拡張は本 baseline のスコープ外。
- PCM codec と MP3 decoder は worker/parallelism 実装自体を持たないため、その2 format には worker 不変性の比較対象がない。実装が追加され次第、同じ 1/4/16 パターンで test を追加する。
- 大容量 corpus（FLAC conformance、PCM/MP3 testdata）の tier 分離・digest manifest 化は [fixtures.md](fixtures.md) が定める通り M10 で行う。M0 baseline はこれらを対象に含めない。

## 完了条件との対応

quality.md の M0 完了条件 6 項目（decode/encode/roundtrip、stream passthrough/metadata、cancel/invalid/Finalize failure、1/4/16 段 filter chain、observation off/on profile、scalar/SIMD/worker semantic diff）はすべて対応する test/benchmark を持ち green である。raw profile と時系列 benchmark 結果は本文書に埋め込まず、再実行 command と最新の意味的所見だけを保つ。
