# performance と reproducibility policy

## 結論

`Fast`、`Stable`、`Portable`、`Realtime` は component config や一つの enum にしない。利用者向けの named preset とし、Host が直交する policy vector へ展開する。

> Decision: offline既定は`Fast + Repeatable + ArtifactNone`で確定した。`Variable`は明示opt-inとする。

```go
type Policy struct {
    Goal          Goal
    Accuracy      Accuracy
    Repeatability Repeatability
    Artifact      ArtifactPolicy
    Implementation ImplementationPolicy
    Continuity    ContinuityPolicy
    Resources     ResourcePolicy
}
```

- `Goal`: throughput、latency、memory の優先関係
- `Accuracy`: exact または schema/variant ごとの bounded numerical difference
- `Repeatability`: 同じProgram/inputでtask scheduleによる数値差を許すか
- `Artifact`: final bytesを要求しない、execution signature内で固定、portability domain内で固定
- `Implementation`: pure-Go、CGO/native、SIMD、unsafe、device 等の許可
- `Continuity`: deadline/overflow 時の block、fail、drop、conceal
- `Resources`: worker、memory、queue、temporary storage の上限

planner は展開後の vector だけを扱う。preset 名を plugin が switch しない。Plan は preset と実効 vector、選ばれた variant、execution signature を両方記録する。

offline job の推奨 default は `Fast` とする。ただし、これは media ordering、timestamp、frame 数、stream/metadata preservation、bounds、安全性を緩めるものではない。許可するのは、明示された numerical contract 内の差と、同じ semantic output を作る implementation/bitstream 選択の差だけである。

## presetをruntime modeにしない

複数presetを、各componentが`if fast`、`if stable`のように解釈してはならない。presetは利用者向けの入力syntaxであり、HostがCompile前に要求へ展開して消費する。

```text
user preset
  -> normalized policy requirements
  -> compatible variantをfilter/rank
  -> 一つのprivate Programを生成
  -> Runは選択済みkernelだけを呼ぶ
```

componentが登録するのはpreset別実装ではなく、実際に異なるalgorithm/implementation variantである。

```go
type Contract struct {
    Accuracy      Accuracy
    Repeatability Repeatability
    Artifact      ArtifactSupport
    Platform      PlatformRequirement
    Resources     ResourceEstimate
}

type Variant[C, P any] struct {
    ID       marker.Type
    Contract Contract
    Compile  func(CompileContext, C) (P, Effect, error)
    Open     func(OpenContext, P) (Operator, error)
}
```

一つのvariantは複数presetの要求を同時に満たせる。たとえばscalarと完全一致するAVX2 integer kernelはFast、Stable、対応domainのPortableすべてで再利用する。`Realtime`も主にscheduler/queue/resource policyであり、codecをRealtime用に複製しない。preset数と実装数の直積を作らない。

実装共有の境界:

- parser、validation、buffer ownership、bit writer、partition、係数table等は共通化する。
- 差がreduction orderだけなら、partition処理を共有し、repeatable reducerとvariable reducerだけを分ける。
- 差がSIMD kernelだけなら、frame/block wrapperを共有してinner kernelだけを分ける。
- artifact安定化のseed、metadata ordering、timestamp field等はHost/muxerのcompile済みpolicyへ集約し、全codecへ分岐を散らさない。
- boolean modeをsample loopへ渡して巨大な共通関数にするより、短い専用kernelを選択する。hot loopの小さな重複は、分岐除去とcompiler最適化に必要なら許容する。

選択結果はfunction field/interface越しでもframe/block単位で一度だけdispatchし、sample/pixel/symbol loop内ではdirect concrete kernelを実行する。execution islandのspecializationで隣接converterもまとめ、itemごとのpolicy lookup、reflection、map lookup、feature判定を残さない。

避けられるcostと避けられないcostを区別する。

- 避けられる: itemごとのmode branch、複数variantの同時Open、重複state、preset数に比例する実装/testの直積。
- control planeだけに残る: variant filtering、cost comparison、Plan記録。jobごとに一度であり、immutable catalog indexとfingerprint cacheでboundedにする。
- 本質的に避けられない: repeatable reduction、portable reference algorithm、追加variantのbinary size等、その保証自体に必要なalgorithmic cost。

plugin authorに全policyの実装を要求しない。一つの正しいvariantだけを提供してもよく、Hostはそのcontractを満たすJobで使用する。公式pluginも、代表 benchmark で意味のある差がないpreset専用variantを追加しない。

## 開発時の性能回帰方針

日常の性能検証は微小な差の追跡ではなく、開発を止める価値がある明白な回帰を早く見つけるために行う。

- 性能に影響し得る変更だけを対象とし、同じ machine・電源条件・input で変更前後または基準実装を比較する。まず代表的な depth/size/worker の少数 case を使う。
- user-visible な総時間の `ns/op` と、絶対量が無視できない `B/op`・`allocs/op` のいずれかが概ね 2 倍以上へ悪化した場合を review trigger とする。小さな絶対値の 1→2 allocation のような比率だけの変化は含めない。
- 2 倍未満の timing 差は、同じ傾向の累積、resource limit 超過、明確な hot-path 変更がない限り routine gate にしない。correctness、leak、panic、data race の失敗にはこの許容幅を適用しない。
- trigger を超えた時だけ再測定し、再現するなら paired benchmark や profile で原因を調べる。小さな差を根拠に optimized variant を採否する場合も、そこで初めて精密測定を行う。
- `processing-ns/op` のような custom metric は lifecycle の内訳を理解する診断値であり、短い run で 0 になり得る値を hard gate にしない。粗い回帰検出には benchmark 全体の標準 metric を使う。
- depth/size/worker/variant の full matrix と CPU/block/heap profile は、baseline 更新、milestone/release、optimized variant の採用、runtime architecture の大幅変更、回帰調査に限定する。

2 倍は自動 reject の厳密な統計閾値ではなく、追加調査を始める目安である。異なる machine の absolute timing や、過去の単発値との比較から失敗を決めない。

## M0 baseline への適用

M0 は最終 policy/variant architecture の完成を要求しない。現行実装から将来の回帰を判定できる測定契約を固定する。

- input generatorまたはfixtureの仕様・digest、実行command、Go/OS/arch/build tag、CPU feature、worker数を記録する。
- throughputだけでなく、item/frame/sample数、順序、timestamp、output digestまたは数値誤差を同時に記録する。
- scalar build と SIMD build は別々にpassさせるだけでなく、同一inputのsemantic outputを横断比較する。
- 比較可能な現行kernel/variantは同じprocess内でAB/BAを反転する。旧runtimeと新runtimeのpaired比較は、新経路導入後に同じharnessへ追加する。
- filter chainは実pipelineと同じ workload shape の sequential direct-call 経路を分け、cold construction/Openとsteady-state Runを分ける。並行実行条件が異なるため、direct-callを厳密なoverhead lower boundとはみなさない。
- raw CPU/block/goroutine/heap profileと時系列resultはCI artifactに置き、Gitにはinput manifest、command、correctness summary、profile summaryを保存する。

M0 の完了判定は [quality](quality.md) の「M0 完了条件」とこの節を用いる。以下の最終 policy、Plan fingerprint、Portable/Realtime gate の完成は後続 milestone の責務である。

## M5 runtime performance gate

M5 の切断直前に、旧 `core/pipeline` と新しい specialized runtime を同じ test binary の AB/BA harness へ接続した。workload は 32,768 個の整数 item を `source -> processor -> processor -> sink` に通し、両 processor が 1 ずつ加算する。各 sample は件数と総和を検査してから採用し、静的な graph compile は測定外、one-shot execution の構築・task/queue lifecycle・全 item transfer は測定内とした。旧 edge と新 edge の item limit はともに 4 である。

切断前に使った command は次である。旧/new の両 contract が同時に存在した最後の source は Git commit `f11fc1a` に固定されており、current tree で旧 contract を復活させて再実行するものではない。

```bash
go test ./internal/run -run '^$' -bench 'Benchmark.*Paired|BenchmarkLinear' -benchmem -benchtime=10x -count=5
```

2026-08-08、`go1.26.4 windows/amd64`、M0 と同じ `13th Gen Intel(R) Core(TM) i7-13620H` 上の同一 process 比較では、5 run の `current/legacy` median ratio は `0.8974`〜`0.9523` だった。絶対時間は保存せず、この範囲を将来の machine と比較しない。`testing.AllocsPerRun` は旧 63、新 72 alloc/run で、差の 9 allocation は execution ごとの固定 lifecycle cost である。queue notification の初期実装で検出した 6 alloc/item は固定通知へ置換し、`TestQueueTransferAllocatesZero` と linear hop test を 0 allocation で固定した。したがって M5 の判定は「2 倍 trigger に該当する architecture regression なし、item/hop に比例する allocation なし」である。小さな速度差を新 runtime 採用の根拠にはしない。

hot-path 12 条の証拠は次を正本とする。benchmark は総合的な回帰検出、test は各構造契約の決定的な gate を担当する。

| 条項 | gate |
|---|---|
| 1, 10: item ごとの reflection/lookup/serialize/`any` map なし | `internal/run/drive.TestTypedSourceProcessorSinkComposeWithoutPerItemErasure`。type assertion と `reflect.Type` は binding 時だけで、delivery は型付き closure のまま通す |
| 2: hop ごとの heap allocation なし | `flow.TestLinearInputTakeHasNoAllocation`、`internal/run/drive.TestLinearProcessorHopAllocatesZero`、`internal/run/queue.TestQueueTransferAllocatesZero`、`host.BenchmarkPreparedRunLinear` |
| 3: observation off の追加 work なし | `internal/observe.TestOffCreatesNoCounterAndNeverReadsClock` と `internal/run.TestObservationStrategiesDoNotEvaluateDetailedTraitsWhenOffOrBasic` が counter、clock、Size/Time trait 呼出しを 0 で検査する |
| 4: node ごとの goroutine/channel なし | `internal/run.TestCompileFusesMaximalLinearProcessorIsland` と `TestBuildRunsSourceAndBoundaryTasksAroundFusedProcessors`。4 node 中 2 Processor は 1 island、processor task は 0、source 1 task と I/O boundary 2 task だけを持つ |
| 5: linear ownership の refcount increment なし | `internal/run/drive.TestTypedSourceProcessorSinkComposeWithoutPerItemErasure` と `TestOneOutputFanoutIsLinearMove` が Fork 呼出し 0、複数 fan-out だけを Fork 経路にする |
| 6: stream property/metadata を item に複製しない | `media/stream.TestDescriptorKeepsStreamLocalPropertiesOutOfItems` と `TestDescriptorCarriesImmutableStaticMetadata` |
| 7: timestamp rescale に arbitrary precision なし | `media/timing` の fixed 128-bit checked integer 実装、overflow/rounding test、`TestRescaleAllocatesZero` |
| 8: resource accounting を中央 item loop に置かない | `internal/memory.TestReservationsAreCoarseAndRepaidOnce`、edge-local queue snapshot test、task-local `internal/observe.Local` test。Host manager は Open 前の coarse reservation だけを扱う |
| 9: item loop に panic-recovery `defer` なし | `internal/run.TestTaskTopPanicIdentifiesNodeAndDropsActiveItem` と Host lifecycle panic matrix。recovery は `internal/task.Group` の task top に一度だけ置く |
| 11: audio converter は region 境界だけ | `internal/solve.TestCompatibleAudioFilterRegionUsesOnlyBoundaryConverters` が filter N=1/4/16 の全てで converter 2 個を検査する |
| 12: exclusive in-place / shared branch COW | `media/audio.TestExclusiveFrameEditReusesBackingWithoutAllocation` と `internal/run/drive.TestAudioFanoutCopiesOnlyModifyingBranch`。exclusive は同一 address・0 allocation、2-way fan-out は変更 branch だけ 1 copy。immutable sample read の代表値は `plugin/pcm/linear.BenchmarkPCMReadViews` |

R-03 の immutable read view 導入時は、4096 sample の 16-bit decode/encode 相当を direct slice reference と同一 process で paired 測定した。

```bash
go test ./plugin/pcm/linear -run '^$' -bench '^BenchmarkPCMReadViews$' -benchmem -benchtime=200ms -count=5
```

2026-08-13、上記 M5 測定と同じ machine/toolchain で 5 run の median `immutable-view/direct-slice` ratio は decode `1.54`、encode `1.77`、両経路 `0 B/op` / `0 allocs/op` だった。

`direct-slice` は同じ変換を最短の slice loop で書いた理論下限であって、R-03 以前の実装ではない。旧実装は sample ごとに `binary.ByteOrder` interface を間接呼び出ししており、同一 machine の paired 測定では 4096 sample mono で decode `8.9 µs` / encode `7.1 µs`、stereo で decode `18.8 µs` / encode `16.6 µs` だった。現行は同 case で decode `5.5 µs` / encode `3.9 µs`、stereo で decode `13.6 µs` / encode `9.7 µs` である。R-03 は hot path を遅くしていない。この 2 つの ratio は下限からの距離であり、regression の判定基準ではない。

`At` を element ごとに呼ぶ初期移行は明白な回帰になったため、`buffer.Bytes.Blocks` で計上済み scratch へ `CopyTo` してから処理する実装へ置換した。`Blocks` の block は長さが呼出しごとに変わるため、hot loop 側で block を frame 境界へ trim し destination を window すると、element loop の bounds check が消えて inline 実装と同じ時間に戻る。scratch は operator の field に置く。stack local の配列を `Blocks` の closure へ渡すと escape して呼出しごとに block size の heap allocation になる（4096 B/op を実測）。

file sink も payload 全体を `AppendTo(nil)` せず、計上済み 64 KiB scratch を再利用して immutable view を drain する。copy 自体は payload size に比例する 1 pass が増えるが、これは syscall に対して無視できる。比例しないのは allocation であり、それを test で 0 に固定する。absolute timing は将来 machine との比較に使わない。

paired harness は旧 contract の最後の consumer であり、結果を確定した M5 cut 時点の一回限りの比較記録である。`_legacy/` は移植参照 algorithm だけに限定するため、旧 package の削除後に build できない harness source は current tree に残さない。継続 gate は `Execution` の test-only shortcut ではなく、resource reservation、Open、Finalize、output transaction、cleanup を含む production の `Prepared.Run` 経路を測る。

```bash
go test ./host -run '^$' -bench '^BenchmarkPreparedRunLinear$' -benchmem
```

## M7 multi-stream performance gate

M7 の fast path は `MP4 demux → per-track copy or typed PCM path → SerialFanIn → MP4 mux` とする。同一
process/fixture で Router/RoutedReader と SerialFanIn を含む unchanged remux、PCM-bound path を AB/BA で交互に測り、Open と
steady-state Run を分けて `-benchmem` を取る。routed producer/SerialFanIn の orchestration は item ごとの mandatory
allocation を 0 とする。route ordinal、callback、queue handoff に reflection、string lookup、`any` transport を
持ち込まない。

`buffer.Handle.Range` など payload slicing の control allocation はこの literal zero gate の対象外とする。user-visible
time または無視できない `B/op`・`allocs/op` が同一条件で概ね 2 倍以上悪化した場合だけ、paired AB/BA の再測定と
profile を行い、payload slicing を再設計する。小さな差や payload size に比例する copy 自体を gate failure にしない。
`flow.Direct` を宣言した port を単一 routed producer が駆動する direct island では、
その call 順を MP4 の physical order として correctness vector にする。宣言のない generic Serial 構成の cross-track
physical interleave、wall-clock order、byte reproducibility は performance gate の前提にしない。MP4 correctness/exact は
track ordinal、`Packet.Sequence`、PTS/DTS/duration、per-track sample table と、demux が sample offset の merge で作る
格納順の保持で判定する。demux/mux が track ごとに cursor を持つ分の常駐は track 数に比例してよく、sample 数には
比例しない。入力が持たない physical order を新規に組む consumer は execution signature と別 ordered policy/backpressure
を要求する。

M7 の constant-RAM/resource gate は、同じ topology、descriptor、queue、processing page、semantic metadata cap で
1,000 samples と 1,000,000 samples を比較する。Inspect、Compile、Open の peak live heap と retained object 数は
duration、sample 数、opaque raw payload 長に比例して増加させない。Inspection は shared immutable な format-owned
source range/summary だけを保持し、source Opening/I/O handle、raw payload bytes、O(samples) 配列を保持しない。
Host は Open 時に元の source Opening を inspected demux と same-format mux へ貸し出し、range は fixed-size page で読む。
WAVE unknown chunk/trailer も同じ range gate を通し、semantic metadata は inline value の明示 cap 内に制限する。

unfragmented transform mux が sample table/offset を蓄積する場合、増加分は Host-owned disk table journal の bytes として
計上し、明示した aggregate quota を越える前に deterministic に失敗させる。これは output boundary の sequential sink を
変換する spool ではない。1k/1M の処理時間が入力数に比例することは gate failure ではないが、heap の sample-count
growth、opaque bytes の全量保持、journal quota 無視、または steady-state allocation/item の同一条件で概ね
2 倍を超える回帰は failure とし、paired AB/BA と profile で確認する。boundary spool を table journal の代用にしない。

## 現行実装の監査結果

現在の最適化は性質が異なるものを同じ build/runtime dispatch で扱っている。

### bit-exact な SIMD

少なくとも次は scalar と exact equality を test している。

- float32 → S16 conversion
- PCM S32 stereo pack/unpack
- MP3 DCT
- MP3 antialias
- FLAC の整数 LPC restore/residual/rice 系
- MS ADPCM predictor の整数処理

これらは SIMD だから `Fast` 専用にする必要はない。`Stable`/`Portable` が要求する domain でも、全対象で exact だと証明できれば利用できる。

### bounded difference を持つ SIMD

FLAC encoder の autocorrelation は scalar の逐次加算に対し、AVX2/FMA で積和と reduction order が変わる。test は relative error `1e-12` を許容している。

decoded PCM は lossless でも、LPC/order/subframe の選択や最終 FLAC bitstream は変わり得る。したがって contract は「lossless semantic exact」と「encoded bytes reproducible」を分ける必要がある。

### 並列化

FLAC encoder/decoder は work completion が out-of-order でも submission queue 順に出力し、parallelism 1 と 8 の byte equality test を持つ。並列であること自体は nondeterministic output を意味しない。

一方、worker 数は `0 -> runtime.GOMAXPROCS(0)` と実行環境から暗黙解決される。現在の Plan/description は CPU feature、scalar/SIMD/FMA、worker 数、block partition を reproducibility signature として十分に固定していない。

### filter と chunking

mixer は `inputIDs` の明示順で accumulation し、normalize は逐次 peak、convolver は partition 順に accumulation する。現在の主経路は schedule completion order を直接使っていない。

ただし将来、parallel reduction、SIMD FMA、fusion、block size、FFT implementation を変更すると差が生じ得る。stateful filter は一 sample の誤差だけでなく、長時間 drift、SNR、stability、chunk-boundary invariance を検証する必要がある。

### build/runtime dispatch

SIMD file は `goexperiment.simd && amd64` build constraint と、package 初期化時の private な CPU feature snapshot で dispatch している。M5 review で exported mutable flag は read-only function に置き換えたが、維持 utility は変換呼び出しごとにその process snapshot を参照する。この方式のまま新 runtime の item loop へ接続すると次が不明確になる。

- 同じ binary 内で `Stable`/`Portable` が scalar を要求する方法
- Plan 時と Run 時の selected variant
- CPU feature が cache/fingerprint に入るか
- `GODEC_FORCE_SCALAR` の process-wide 初期化値と Job policy の関係

build constraint は「variant が binary に存在するか」だけに使い、selection は Host が immutable CPU feature snapshot と policy を使って Compile 時に行うべきである。`GODEC_FORCE_SCALAR` は scalar/SIMD differential harness の process input に限定し、利用者向け runtime policy にしない。M8 は component Compile/Open が選択済み direct function を Program に保持し、Run の item loop が `sdk/dsp` の feature functionや環境変数を読まないこと、選択 feature/variant が Plan fingerprint に入ることを検査する。

`sdk/bits` には別に、通常buildでprogrammer assertionを有効、`production` tagでno-opにする独自build modeがある。現行Docker buildやroot test commandはこのtagをrelease contractとして固定・比較していない。利用者が知らないtagでcorrectness checkとhot-path costを変えない。

- untrusted boundaryのvalidationは全buildで必須にする。
- programmer invariantはvalidated private type/APIで表現し、可能なら失敗不能にする。
- debug-only assertionが必要なら明示instrumentation buildとし、official releaseの唯一の安全網にしない。
- check除去に意味のある性能差がある場合だけpaired benchmarkで証明し、checked/referenceとのdifferential testを行う。
- build tag/variantはartifact provenanceとCI matrixへ必ず入れる。

## 常に守る correctness

performance preset に関係なく、次は非交渉条件である。

- timestamp/time-base overflow と rounding rule
- packet/frame/event の順序
- frame/sample の欠落・重複
- declared fan-in semantics（Zip の alignment を含む。SerialFanIn は timestamp alignment を持たない）
- EOF、Flush、Finalize
- stream mapping と metadata loss report
- buffer bounds、input validation、panic/error semantics
- lossless decoder の logical sample/data exactness
- CRC、checksum、integer PCM、bit parser の規格 exactness
- cancel、resource limit、transaction

`Fast` を理由に validation を省いた unchecked SIMD を untrusted boundary へ適用しない。境界で一度 validation した後の internal fast path にだけ unchecked operation を許可する。

Realtime でも drop/conceal を暗黙許可しない。deadline miss 時の `Block`、`Fail`、`DropLate`、`Conceal` は media/schema ごとの `ContinuityPolicy` で明示する。

## 三種類の同一性

「同じ結果」を一つにしない。

### Semantic exact

規格上の意味が同じである。

- FLAC bitstream が異なっても decode 後 PCM が完全一致
- container chunk ordering が異なっても stream/metadata semantics が同じ

lossless codec は最低でも semantic exact を全 preset で満たす。

### Numerical bounded

decoded/filter output が variant の宣言した tolerance 内である。

用途に応じて次を使う。

- max absolute/relative error
- max ULP
- RMSE/SNR
- peak/energy deviation
- phase/time drift
- long-stream stability
- codec conformance tolerance

generic Host が異種 component の tolerance を根拠なく加算し、偽の end-to-end bound を表示しない。Plan は node ごとの contract と「end-to-end bound 未算出」を区別する。公式 pipeline は integration fixture で全体品質を検査する。

### Byte reproducible

最終 output bytes が同じである。encoder decision、container field order、padding、random seed、creation time、metadata ordering、worker/reduction order まで固定する必要がある。

byte reproducibility を要求する policy では、現在時刻、process ID、map order、unrecorded RNG、host-dependent metadata を自動出力しない。dither/randomized algorithm は seed を Plan に固定する。

## repeatability と artifact reproducibility

accuracy、run-to-run repeatability、final artifact identityは別の要求である。

### Repeatability

- `Repeatable`: 同じProgram/inputではtask completion timingにかかわらず同じitem valueを返す。固定partition/reduction orderを使う。
- `Variable`: 同じProgram/inputでもscheduleにより、宣言したaccuracy bound内の数値差を許す。

`Variable`でもplannerの選択、packet/frame ordering、timestamp、item数はdeterministicでなければならない。許可するのは数値contract内の差だけである。

### ArtifactNone

final byte identityを要求しない。CPU feature、FMA、encoder decision、container ordering等により、semantic exactまたはbounded numerical outputから異なるartifactを生成できる。

### ArtifactStable

同じ execution signature で最終 bytes を再現する。

signature:

- component/variant marker identity と implementation version
- config と input snapshot
- Go toolchain/build provenance
- GOOS/GOARCH と relevant CPU features
- numerical mode
- worker count と deterministic partition/reduction tree
- block、batch、FFT partition、fusion layout
- explicit seed

`ArtifactStable`は`Repeatable`を含意する。variantはtask completion orderをaccumulation/orderに使わない。同じsignatureなのにoutputが異なる場合はplugin bugである。

### ArtifactPortable

variant が宣言する portability domain 内で、architecture、SIMD availability、thread countを越えた byte reproducibility を要求する。

すべての浮動小数点/transcendental algorithm が Portable を提供できるとは限らない。fixed-point/reference algorithm、固定係数、定義済み rounding/reduction を持つ variant だけが対応を宣言する。graph の一部が要求を満たせなければ、黙ってStable/ArtifactNoneへ落とさずCompile errorにする。

Portable は `unsafe`/SIMD の全面禁止ではない。全対象 architecture の differential test で exact semantics を証明できる整数 SIMD 等は使用できる。CPU feature により結果が変わる FMA path は使用できない。

portability domain には algorithm/schema version と supported toolchain/target matrix を明示する。「将来のすべての Go version/architecture で永久に同じ」という保証はしない。

## user-facing preset

### Fast

```text
Goal:            Throughput
Accuracy:        Exact where required, otherwise declared bounded
Repeatability:   Repeatable
Artifact:        None
Implementation: official pure-Go, unsafe/SIMD allowed, native only if separately allowed
Continuity:      Preserve
Resources:       auto within job/host limits
```

- fastest conforming scalar/SIMD/FMA/parallel variant を選べる
- runtime-resolved worker/CPU feature を Plan に固定する
- semantic exact と structural correctness は維持する
- `Variable` variantは明示opt-inとし、variation causeをPlanに記録する

### Stable

```text
Goal:            Balanced/Throughput
Accuracy:        Exact or deterministic bounded
Repeatability:   Repeatable
Artifact:        Stable
Implementation: stable variants only
Continuity:      Preserve
Resources:       resolved/fixed in execution signature
```

- 同じ signature で final bytes を再現する
- deterministic partition/reduction を使う
- exact SIMD は利用可能
- CPU-specific bounded variant も、同じ CPU/signature 内で bit-reproducible なら利用可能

### Portable

```text
Goal:            Reproducibility
Accuracy:        portable exact/deterministic contract
Repeatability:   Repeatable
Artifact:        Portable
Implementation: declared portable variants only
Continuity:      Preserve
Resources:       output-invariant worker policy
```

- cross-target digest test を通る reference/fixed algorithm を選ぶ
- unsupported component があれば Compile error
- scalar を優先できるが、「scalar だから Portable」とは自動判断しない

### Realtime

```text
Goal:            Latency
Accuracy:        declared bounded
Repeatability:   explicit policy
Artifact:        None by default
Implementation: deadline-capable variants
Continuity:      required explicit policy
Resources:       bounded queue/buffer and deadline
```

Realtime は Fast/Stable/Portable と完全に同じ軸ではなく、主に Goal/Continuity/Resources の preset である。必要なら `Realtime + Stable` 相当の明示 vector を作れる。

deadline miss だけで offline correctness を捨てない。drop/conceal を許可しない場合、deadline miss は diagnostic/failure にする。

## component variant contract

同じ semantic component は複数 implementation variant を持てる。

```go
type Variant[P any] struct {
    Identity        plugin.Identity
    Requirements    platform.Constraint
    Accuracy        numeric.Contract
    Reproducibility reproducibility.Contract
    Estimate        component.Estimate
    Open            func(OpenContext, P) (flow.Operator, error)
}
```

variant identity は component identity/config と分ける。`scalar`、`avx2` の手入力 alias だけを canonical identity にせず、variant 専用 marker type から作る。表示 alias は別に持てる。

variant は次を宣言する。

- exact、bounded、semantic-only の区分
- tolerance と適用 schema/property range
- Stable/Portable domain
- CPU/OS/architecture/toolchain requirement
- pure-Go、unsafe、CGO/native、device property
- schedule/chunk/worker count dependence
- memory、latency、throughput estimate
- alignment/block-size precondition

一つしか implementation がなければ variant boilerplate を要求しない。component helper が implicit default variant を構築する。

third-party plugin の宣言は利用者の trust 対象である。Host が numerical truth を runtime に証明することはできない。testkit、official review、provenance が宣言を検証する。

## selection と fallback

selection は Compile/Optimize 時に一度行い、private `Program` に direct function/operator factory として固定する。

- item/frame ごとに policy、CPU feature、variant registry を lookup しない
- variant branch を inner loop に残さない
- `Open` 後に silent scalar/SIMD fallback しない
- CPU/device/resource requirement が Plan 後に満たせなければ stale Plan/Prepare error にする
- fallback が必要なら再 Compile して新しい Plan を得る

CPU feature は Host construction/Prepare 時の immutable snapshot とする。`dsp.HasAVX2` のような exported mutable global は M5 review で削除済みであり、M8 の新経路 test は injected feature set/catalog を使う。維持 utility の process snapshot を Program selection の代用にしない。

worker 数、block/partition、fusion が output に影響し得る場合は Plan fingerprint に含める。`parallelism=auto` は実 worker grantへ解決してから Plan を確定する。

## planner

planner の hard filter:

1. schema/semantic requirement
2. implementation allow/deny policy
3. required accuracy
4. required repeatability と artifact identity
5. platform/resource availability

hard filter 後の候補だけを Goal/Estimate で順位付けする。少し速い variant が accuracy/reproducibility hard requirement を逆転させない。

`Effect` は少なくとも次を区別する。

- numerical bounded difference
- schedule-dependent numerical result
- semantic-exact but byte-different encoding
- non-portable output
- lossy encode generation
- content/timeline/stream loss

最後の三種類を一つの `QualityLoss` 整数へ潰さない。

## caching と provenance

Plan に記録する。

- requested preset と expanded policy
- component/variant canonical identity
- implementation/build/toolchain provenance
- CPU/platform feature requirement
- resolved worker/block/partition/fusion
- accuracy/reproducibility contract
- schedule dependence
- explicit seed

ArtifactStable/ArtifactPortable output は execution signature/domain が一致する場合だけ content-addressed cache key にできる。ArtifactNone/Variable output を、input/config が同じという理由だけで byte-identical artifact cache として再利用しない。

public Plan に raw pointer/function や platform-specific opaque handle を入れない。execution signature は canonical DTO/hash とする。

## test strategy

### variant differential

公式 optimized variant は reference/scalar と次を比較する。

- zero、boundary、NaN/Inf policy、denormal、clipping
- random/property/fuzz input
- misalignment、tail length、small/large block
- scalar vs 各 SIMD feature
- FMA on/off
- worker 1/2/N
- chunk/block/partition size
- forced completion-order permutation
- long stream/state drift

Exact variant は equality/digest、bounded variant は schema-specific tolerance で判定する。

### codec

- lossless decoder: logical samples exact
- lossless encoder: decode roundtrip exact
- Stable encoder: same signature の bitstream digest exact
- Portable encoder: target matrix の bitstream digest exact
- lossy codec: conformance vector + PCM tolerance/SNR
- parser/CRC/timestamp: 全 preset exact

### filters

- impulse、step、sine、noise、silence、boundary
- chunk-boundary invariance
- state reset/seek
- long-duration drift
- fan-in order
- scalar/SIMD/parallel difference
- end-to-end SNR/peak/phase

### full CI matrix

- scalar build
- SIMD build + available feature paths
- forced scalar variant inside SIMD-capable build
- parallelism 1/N
- Stable repeat run/digest
- Portable cross-target artifact/digest comparison
- race/fuzz/sanitizer-equivalent checks where available

`tools/cmd/test-runner`（`./test-runner.exe --simd` 相当）は指定した1 variant の build/test を走らせるだけで、それ単独では scalar/SIMD artifact の横断比較を保証しない。

## benchmark と採用条件

bounded-difference variant は「速そう」という理由だけで追加しない。

- scalar/reference と同じ input の代表 case で、correctness と改善の方向を確認する。
- 採用判断が小さな差に依存する場合は、同じ process で交互に走らせる paired benchmark を使う。
- bottleneck や予想外の回帰を説明する必要がある場合だけ CPU、allocation、memory bandwidth、block/mutex profile を取る。
- lifecycle cost が判断を変える経路では cold Open と steady-state Run を分ける。
- bounded difference を許す variant は output difference/tolerance を correctness test で固定し、性能値だけで採用しない。

PC の電源状態等で絶対時間が変わるため、過去 machine の raw timing と単純比較しない。日常の回帰判定は上記の 2 倍目安を使い、精密な採用判断だけ paired result と必要な profile で review する。

optimized variant の保守コストに対して有意な改善がない場合は削除する。Fast path の存在自体を目的にしない。

## architecture overhead gate

policy/variant abstraction を導入しても、data plane へ次を増やさない。

- item ごとの interface/registry lookup
- item ごとの CPU feature/policy branch
- variant ごとの frame conversion
- observation 用 mandatory atomic/clock
- stable mode のための Fast path 上の global lock

Compile/Open が direct typed function、block size、worker layout を選び、Run は specialized path を使う。Stable/Portable の追加処理はそれを選んだ Program にだけ含める。

## 文書全体の完了条件

この節は performance/reproducibility contract の最終状態を示し、M0 単独には遡及適用しない。

- Fast/Stable/Portable/Realtime が一つの曖昧な enum ではなく policy vector として説明できる。
- Fast でも media ordering、timestamp、frame count、lossless semantics、validation を緩めない。
- exact SIMD は Stable/Portable から不必要に排除されない。
- bounded SIMD/FMA と semantic-exact/byte-different encoder を Plan で区別できる。
- selected variant、CPU feature、worker、block/partition、seed が Plan fingerprint に入る。
- same Stable signature の repeat run が byte-identical になる。
- Portable 非対応 graph は silent downgrade せず Compile error になる。
- Realtime の drop/conceal は explicit ContinuityPolicy なしに有効にならない。
- official optimized variant に scalar/reference differential test と代表 benchmark があり、小さな性能差を採用根拠にする場合は paired comparison がある。
- observation off の Fast path に policy/variant lookup が現れない。
- undocumented `production` tag でvalidation/correctness semanticsを切り替えない。
