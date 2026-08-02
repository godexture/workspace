# M0 baseline artifact

この文書は、docs/refactor/checkpoint.md M0#7/M0-R5 が要求する「再現可能な比較条件」の正本である。生の pprof/benchmark 出力は [fixtures.md](fixtures.md) の方針に従い Git へ蓄積せず、この文書には再現手順・toolchain・意味上の正しさの結果・所見の要約だけを残す。toolchain と再現 command は [baseline.manifest.json](baseline.manifest.json) にも machine-readable な形で重複して持たせてあり、そちらを正本の入力として script/CI から直接読める。

## 固定した commit

- baseline commit: `4429711a88481e6643ea2427a8192938737b1e9e`
- 選定理由: 以前このファイルが固定していた `1b3a38e` は、`Snapshot().Elapsed` 方式への切り替え、`runChain` の frame release 修正、`BenchmarkGainChainDepths` の frame数/block size/Encode-Decode 境界の整合より **前** の commit だったため、当該条件をその commit から再現できなかった。今回の commit はそれらすべての修正を含む。以下の再現手順とこの文書の実測値は、この commit から直接得られる。
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
| package 数 | 104（`go list ./...`、root module のみ。nested module 4件は別途 `go list` する） |
| input generator | `stereoBlock`（`plugin/audio/filters_bench_test.go`）は固定 seed の `math/rand/v2` PCG generator を使い、同じ size tier に対して常に同一 byte 列を生成する。仕様は manifest の `inputGenerators` を正本とする。FLAC/observation paired benchmark の fixture はいずれも hardcode/固定 size で乱数を使わない。 |

## 再現手順（human-readable summary の実行 command）

正確な argv/env は [baseline.manifest.json](baseline.manifest.json) の `commands` を正本とする（shell string ではなく構造化済みなので、記録した OS 以外でも shell 構文の違いを気にせず実行できる）。以下は human-readable な要約。

個別 build の正しさ（必須2種 + 任意診断1種）:

```bash
go build ./... && go test ./...                                      # scalar (required)
GOEXPERIMENT=simd go build ./... && GOEXPERIMENT=simd go test ./...  # SIMD (required)
GOEXPERIMENT=simd GODEC_FORCE_SCALAR=1 go test ./...                 # SIMD build, forced scalar dispatch (optional diagnostic)
```

代表 benchmark（paired、allocation、worker/block/depth 別）:

```bash
go test ./core/pipeline/... -bench BenchmarkPipelineObservationPaired64MiB
go test ./plugin/audio -bench BenchmarkGainChainPipeline        # reports processing-ns/op alongside built-in ns/op
go test ./plugin/audio -bench BenchmarkGainChainPipelineOpen
go test ./plugin/audio -bench BenchmarkGainChainDepths          # same shape as above; compare its ns/op against processing-ns/op
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
- worker/parallelism 数不変性: FLAC decoder/encoder（既存、parallelism 1 vs 8、`plugin/flac/internal/codec/decoder` 等）、convolver の partition build に加え、Engine の SendFrame/ReceiveFrame を通した end-to-end 出力そのもの（`TestEngineWorkerCountDoesNotChangeEndToEndOutput`、worker 1/4/16、frame 数・各 frame の PTS・sample data を比較。入力 frame の Release と Engine の Close も検査する）で確認。**PCM codec と MP3 decoder は worker/parallelism 実装自体を持たず、比較対象がない**という事実を記録する。
- truncated/invalid input: WAVE で、期待される dataOffset/dataSize/payload byte 数を独立した ground truth と突き合わせる形で追加（`plugin/wave/internal/truncated_test.go`）。実際に3種の mutant（宣言 size の fabrication、有効な完全入力の誤 reject、header byte の payload への混入）を手動注入し検出を確認した上で revert 済み。
- lifecycle failure injection: WAVE/MP3/FLAC の mux/demux 各 I/O phase（`sdk/testutil/fault` 経由）、実 muxer + 実 source を組んだ pipeline レベルの「primary failure と Finalize failure の同時発生」（`plugin/wave/internal/failure_test.go`）に加え、M0-R4 で decoder/encoder の Flush 失敗（`sdk/engine/wrapper_test.go` の `TestEncoderAdapter_CloseAfterFlushError`/`TestDecoderAdapter_CloseAfterFlushError`）と、muxer の `SetMetadata`/`AddStream` 失敗時に全 node が確実に1回だけ close されること（`core/routing/negotiator_lifecycle_test.go` の `TestNegotiatorClosesAllNodesWhenMuxerSetMetadataFails`/`TestNegotiatorClosesAllNodesWhenMuxerAddStreamFails`）を追加。
- 現行 stream/metadata 経路: target codec 省略時も decoder/encoder が必ず開くこと（stream copy が存在しないこと）を固定（`sdk/conversion/passthrough_test.go`）。metadata は M0-R3 で単一 known key（title）の baseline から拡張し、multi-value の順序保持（`TestBuildPreservesOrderedMultiValueMetadataThroughOmittedCodecRoute`）、single-value の重複上書き（`TestBuildOverwritesDuplicateSingleValueMetadataThroughOmittedCodecRoute`、後勝ち）、未知 INFO tag の完全消失（`TestBuildDropsUnrecognizedMetadataThroughOmittedCodecRoute`、raw fallback すら存在しない明示的な loss）、raw chunk（`cue `）の保持（`TestBuildPreservesRawCueChunkThroughOmittedCodecRoute`）を固定した。

## allocation / profile 所見

- `BenchmarkPipelineObservationPaired64MiB`（core/pipeline）: plain/off/progress/metrics を同一 process 内で交互実行する paired 比較を持つ。baseline commit 上の実測では metrics-vs-off `+13.7%`、off-vs-plain `+4.8%`、progress-vs-off `+4.5%`（共有 machine 上の実行で run ごとの分散が大きく、絶対値・符号とも参考値。系統的な悪化の有無は比率の桁で見る）。
- `TestPipelineObservationDoesNotLeakGoroutines`: plain/off/progress/metrics いずれも green。
- `BenchmarkGainChainPipeline`（plugin/audio）: construction は `b.StopTimer`/`b.StartTimer` で計測対象から除外し、`Pipeline.Prepare` も同じ除外区間で明示的に呼ぶことで、timer 内の `Run` 呼び出し自体は Prepare を no-op として通過する。built-in の ns/op と allocs/op は `Pipeline.Run` の teardown（全 node の Close）を引き続き含む（`Pipeline` に teardown を伴わない公開 API がないため）。`Pipeline.Run` は node 処理開始直前に `startedAt`、終了直後・teardown 開始前に `finishedAt` を記録するため、`Snapshot().Elapsed`（plain `pipeline.New` のままで、追加の公開 API 不要）がすでに steady-state-only の値であり、これを Run 後に読み出して `processing-ns/op` という custom metric として追加報告する。baseline commit 上の実測（Small block）では built-in ns/op が 1 段 54103ns/7416B/100 allocs 〜 16 段 211915ns/25255B/673 allocs、processing-ns/op は 1 段 37772ns 〜 16 段 189972ns。`BenchmarkGainChainPipelineOpen` は construction + `Prepare`（cold lifecycle）のコストを分離して持つ（Close は timer 外。1 段 7942B/43 allocs 〜 16 段 62078B/270 allocs）。
- `BenchmarkGainChainDepths`（chain_test.go、direct な SendFrame/ReceiveFrame chain）: `BenchmarkGainChainPipeline` と同じ chainDepths × chainBlockSizes、同じ chainFrameCount(8) frame/op、frame ごとに Encode/Decode する形へ揃えたことで、processing-ns/op と直接比較できるようになった。baseline commit 上の実測（Small block）では 1 段 16100ns、4 段 31009ns、16 段 76252ns。ただし depth/block size が大きいほど `BenchmarkGainChainPipeline` の方が速い逆転が起きる（例: 16段/Large で Depths 153318588ns、Pipeline processing-ns/op 11597499ns、13倍の差）。これは `pipeline.Link` の既定 buffer（100）により、8 frame が source Encode・各 gain stage・sink Decode をまたいで並行にオーバーラップ実行できるためで、単一 goroutine で逐次実行する direct-call benchmark には原理的に再現できない。したがって両者の差は「pipeline のオーバーヘッドのみ」ではなく、オーバーヘッドと並行実行による利得の純計として読む。
- `BenchmarkParallelDecodeThroughput`（plugin/flac/internal/codec/decoder）: parallelism 4 が 1/16 より速く、allocs/op はわずかに増加する（1: 229 allocs、4: 238 allocs、16: 245 allocs）。

## 既知のギャップ（M0 完了時点で未解消、後続 milestone へ）

- Finalize/Close failure injection は WAVE を中心に代表実装した。MP3/FLAC は mux 側の主要 phase をカバーするが、node Close 自体が全 format で no-op のため「Close 自体が失敗するケース」は実質的に検証対象が存在しない（F50 と同根の現状把握）。
- metadata の unknown/raw/duplicate/order は M0-R3 で WAVE 経路について固定したが、一般化された open metadata.Document 契約は M3 待ち。他 format（FLAC/MP3/ID3）への同種拡張は本 baseline のスコープ外。
- PCM codec と MP3 decoder は worker/parallelism 実装自体を持たないため、その2 format には worker 不変性の比較対象がない。実装が追加され次第、同じ 1/4/16 パターンで test を追加する。
- 大容量 corpus（FLAC conformance、PCM/MP3 testdata）の tier 分離・digest manifest 化は [fixtures.md](fixtures.md) が定める通り M10 で行う。M0 baseline はこれらを対象に含めない。

## 完了条件との対応

quality.md の M0 完了条件 6 項目（decode/encode/roundtrip、stream passthrough/metadata、cancel/invalid/Finalize failure、1/4/16 段 filter chain、observation off/on profile、scalar/SIMD/worker semantic diff）はすべて対応する test/benchmark を持ち green である。scalar/SIMD/worker の semantic diff は、対象 package ごとの differential test（`sdk/dsp` の scalar/SIMD 比較、FLAC decoder/encoder の parallelism 1 vs 8、convolver の worker 1/4/16 end-to-end 比較 等）で満たしており、repository 全体を横断する `tools/cmd/differential` の実行結果には依存しない（同 tool は required gate ではない。上記のとおり）。raw profile と時系列 benchmark 結果は本文書に埋め込まず、再実行 command と最新の意味的所見だけを保つ。
