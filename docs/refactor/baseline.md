# M0 baseline artifact

この文書は、docs/refactor/checkpoint.md M0#7 が要求する「再現可能な比較条件」の正本である。生の pprof/benchmark 出力は [fixtures.md](fixtures.md) の方針に従い Git へ蓄積せず、この文書には再現手順・toolchain・意味上の正しさの結果・所見の要約だけを残す。

## 固定した commit

- baseline commit: `2a7ea1a`（このファイルを追加する直前の HEAD。以降このハッシュを新設計側との比較基準にする）
- repository は 2026-08-01 の monorepo 統合（M1）後の状態。旧 16 submodule 構成の baseline ではない。

## toolchain（machine-readable input manifest 相当）

| 項目 | 値 |
|---|---|
| Go | `go1.26.4 windows/amd64` |
| CPU | `13th Gen Intel(R) Core(TM) i7-13620H`（絶対時間の参考値。AGENTS.md の方針により過去 machine との単純比較はしない） |
| GOWORK | repository root `go.work`（root module + `tools`、`bindings/wasm`、`example/go`、`example/web/server` の4 nested module） |
| build mode | scalar（既定 `GOEXPERIMENT` なし）と SIMD（`GOEXPERIMENT=simd`）の2種 |
| worker | `registry.NewWorkerPool(N)`、N ∈ {1, 4, 16} を明示指定（`auto` は使わない） |
| package 数 | 104（`go list ./...`、root module のみ。nested module 4件は別途 `go list` する） |

## 再現手順（human-readable summary の実行 command）

正しさ（scalar/SIMD 横断、104 package、一つの report）:

```bash
go run ./tools/cmd/differential ./...
```

期待結果: `104 packages: 104 agree, 0 mismatched (scalar failures: 0, SIMD failures: 0)`。

個別 build の正しさ:

```bash
go build ./... && go test ./...                              # scalar
GOEXPERIMENT=simd go build ./... && GOEXPERIMENT=simd go test ./...  # SIMD
```

代表 benchmark（paired、allocation、worker/block/depth 別）:

```bash
go test ./core/pipeline/... -bench BenchmarkPipelineObservationPaired64MiB
go test ./plugins/filter-audio -bench BenchmarkGainChainPipeline
go test ./plugins/filter-audio -bench BenchmarkGainChainPipelineOpen
go test ./plugins/codec-flac/internal/decoder -bench BenchmarkParallelDecodeThroughput
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

- `go run ./tools/cmd/differential ./...`: **104/104 package が scalar/SIMD で一致、mismatch 0、failure 0**（2026-08-01 実測）。
- decode/encode/roundtrip（WAVE/PCM/MP3/FLAC）: 既存 test が green（`plugins/*/test/roundtrip_test.go` 等）。
- worker 数不変性: FLAC decoder/encoder（既存、parallelism 1 vs 8）、convolver の partition build（本セッションで追加、worker 1/4/16）で確認。**codec-pcm と codec-mp3 は worker/parallelism 実装自体を持たず、比較対象がない**という事実を記録する。
- truncated/invalid input: WAVE で新規に、期待される dataOffset/dataSize/payload byte 数を独立した ground truth と突き合わせる形で追加（`plugins/format-wav/internal/truncated_test.go`）。実際に3種の mutant（宣言 size の fabrication、有効な完全入力の誤 reject、header byte の payload への混入）を手動注入し検出を確認した上で revert 済み。
- lifecycle failure injection: WAVE/MP3/FLAC の mux/demux 各 I/O phase（`sdk/testutil/fault` 経由）と、実 muxer + 実 source を組んだ pipeline レベルの「primary failure と Finalize failure の同時発生」を検証（`plugins/format-wav/internal/failure_test.go`）。
- 現行 stream/metadata 経路: target codec 省略時も decoder/encoder が必ず開くこと（stream copy が存在しないこと）と、known metadata（title）が経路を通ることを固定（`sdk/conversion/passthrough_test.go`）。

## allocation / profile 所見

- `BenchmarkPipelineObservationPaired64MiB`（core/pipeline）: plain/off/progress/metrics を同一 process 内で交互実行する paired 比較を持つ。2026-08-01 実測では off は plain 比で大きな増加なし、metrics は off 比で観測可能な増分があるが、絶対値は machine 依存のため比率のみを参照する。
- `TestPipelineObservationDoesNotLeakGoroutines`: plain/off/progress/metrics いずれも 5 回連続実行後に新規 goroutine stack が残らないことを確認。processed packet 数も期待値と一致。
- `BenchmarkGainChainPipeline`（filter-audio）: 1/4/16 段 × small/medium/large block で、深さに応じた線形に近い allocation 増加（1 段 19KB/160 allocs 〜 16 段 95KB/972 allocs、Small block 時）。`BenchmarkGainChainPipelineOpen` により construction/Open だけのコストを分離済み（1 段 6.4us/38 allocs 〜 16 段 26.5us/265 allocs）。

## 既知のギャップ（M0 完了時点で未解消、後続 milestone へ）

- Finalize/Close failure injection は WAVE を中心に代表実装した。MP3/FLAC は mux 側の主要 phase をカバーするが、node Close 自体が全 format で no-op のため「Close 自体が失敗するケース」は実質的に検証対象が存在しない（F50 と同根の現状把握）。
- metadata の unknown/raw/duplicate/order 一般化された伝播契約は M3 の open metadata.Document 契約待ち。今回は known key（title）1本の baseline のみ。
- 大容量 corpus（FLAC conformance、PCM/MP3 testdata）の tier 分離・digest manifest 化は [fixtures.md](fixtures.md) が定める通り M10 で行う。M0 baseline はこれらを対象に含めない。

## 完了条件との対応

quality.md の M0 完了条件 6 項目（decode/encode/roundtrip、stream passthrough/metadata、cancel/invalid/Finalize failure、1/4/16 段 filter chain、observation off/on profile、scalar/SIMD/worker semantic diff）はすべて対応する test/benchmark を持ち green である。raw profile と時系列 benchmark 結果は本文書に埋め込まず、再実行 command と最新の意味的所見だけを保つ。
