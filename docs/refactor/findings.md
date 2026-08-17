# 現行コードの監査結果

この文書は、リファクタリングが必要と判断した根拠の索引である。実装方針・完了条件・進捗はここで重複管理しない。

- 全体の順序と進捗: [refactor.md](../refactor.md)
- 確定した判断: [decisions.md](decisions.md)
- 現行 package ごとの処遇: [inventory.md](inventory.md)
- 各項目の目標仕様: 表の「詳細」列

ID は議論・実装・レビューで参照するため維持する。優先度は依存順に近いが、一項目ずつ独立 patch にすることを意味しない。

## P0: foundation と correctness

| ID | 監査結果 | 詳細 | 担当 |
|---|---|---|---|
| F1 | `core` が `sdk` と公式 plugin に依存し、`sdk` も `core` と公式 plugin に依存する。調査時点で11 moduleが同じ requirement graph の強連結成分に入っており、独立 versioning できない。 | [architecture](architecture.md)、[inventory](inventory.md)。**完了（M5 cut）** | M1/M5 |
| F2 | `core/registry`、`resolver`、`routing` が decoder/demuxer/filter 等を固定列挙するため、新しい component role の追加に core の型と switch の変更が必要になる。 | [plugins](plugins.md)、[media](media.md)。**完了（M5 cut）** | M2/M3 |
| F3 | conversion/routing が単一の主 audio stream と固定 Packet/Frame 経路を前提にし、複数 input/output、video、subtitle、data、attachment、program を表現できない。 | foundation/cut は **完了（M5）**。static `Many` port、ordered repeated descriptors、typed Router、`SerialFanIn` の最初の production consumer と残る完了条件は [M7-0 contract](m7-0.md) の M7-C01〜M7-C11 で追跡する | M3/M7 |
| F4 | `InputSet` と routing が `io.ReadSeeker`/`io.Writer` を live spec に保持し、Job、storage、session、device、mapping、policy の責務が混在している。 | [surfaces](surfaces.md)、[access](access.md)。**完了（M5 cut）** | M3/M4 |
| F5 | `MediaAttributes` は audio を直接持ち、video は未実装である。stream kind/property を増やすたび core model の変更が必要になる。 | [media](media.md)。**完了（M5 cut）** | M3 |
| F6 | 汎用 `Frame` は実質的に audio 型を隠し、`PacketKindStreamEnd` は data と lifecycle を混在させる。time/side data も不足し、audio filter ごとに byte↔float 変換を繰り返す。 | [media](media.md)、[audio](audio.md)、[runtime](runtime.md)。**完了（M5 cut）** | M3/M8 |
| F7 | planner が候補 Factory を生成・Close して出力を調べる。一方、plan 用 transformation と runtime path に同じ変換規則を別実装すると drift する。 | [planner](planner.md)、[plugins](plugins.md)。**完了（M4）** | M4 |
| F8 | cancel、channel close、plugin goroutine、queue drain、shutdown が分散し、double-close、send-after-close、item/resource leak、無期限 wait の余地がある。 | [runtime](runtime.md)。**完了（M5）** | M5 |
| F9 | Packet/Frame の retain/release、write error、fan-out、drop 時の owner が API 全体で統一されず、plugin author が手動 release を扱う。 | [runtime](runtime.md)。**完了（M5）** | M3/M5 |
| F10 | schema、multiplicity、duplicate connection、required port、cycle、reachability の検証が builder/runtime に分散し、不正 graph が panic または実行時 error になる。 | [runtime](runtime.md)、[planner](planner.md)。**完了（M4）** | M4 |
| F11 | package `init` と blank import が process-global registry を変更するため、複数 Host、test isolation、custom distribution、deterministic catalog を妨げる。 | [plugins](plugins.md)。**完了（M5 cut）** | M2 |
| F12 | reflection identity の衝突回避という利点はあるが、config 型や runtime 型が component identity も兼ね、設定 refactor と identity が結合している。 | [plugins](plugins.md)。**完了（M5 cut）** | M2 |
| F13 | config generator の単一 preset 分岐は `(Config, error)` を単一値として代入するコードを生成する。さらに generated functional options が config schema と別の巨大な public API を作っている。 | [config](config.md)、[quality](quality.md)。**完了（M5 cut）** | M2/M8 |
| F14 | test-only CGO/FFmpeg は production purity の問題ではない一方、foundation/module graph と通常 CI に native/公式 plugin integration が混ざっている。 | [quality](quality.md)、[supply](supply.md) | M10 |

## P1: extensibility、runtime、surface

| ID | 監査結果 | 詳細 | 担当 |
|---|---|---|---|
| F15 | `sdk/engine`、`conversion`、`catalog`、`config` が core contract を再抽象化し、plugin authoring、application API、utility、test helper を一層に抱えている。 | [architecture](architecture.md)、[plugins](plugins.md)、[inventory](inventory.md)。**完了（M5 cut）** | M5 |
| F16 | config の意味が struct/tag、default variable、`Validate`、dynamic resolver、CLI reflection、preset、generated option、catalog description に分散する。 | [config](config.md)。**完了（M5 cut）** | M2/M8 |
| F17 | role ごとの boolean/callback capability と plugin 自己申告の単純 cost では、open schema、I/O requirement、variant、loss/effect を構造的に比較できない。 | [planner](planner.md)、[performance](performance.md) | M4/M8 |
| F18 | Format 候補が同じ cursor を rewind して probe し、read budget、range共有、non-seekable input、capability不足が明示されない。 | [access](access.md)。**完了（M6）**: prepared session acquire、候補間の共有 probe cache、byte/round budget、sequential prefix replay、実 capability/view 再検証、必要時の spool insertion を file/WAVE 経路で検査した | M3/M4/M6 |
| F19 | metadata key が core の閉じた method set に依存し、format plugin が ID3/Vorbis Comment 実装を直接 import するため、第三者規格の追加に既存 package の変更が必要になる。 | [media](media.md)。**完了（M5 cut）** | M3 |
| F20 | format が codec tag、parameter、packetization を直接知る箇所があり、WAVE と MP3/PCM 等が直接依存する。container chunk と codec packet の境界も独立していない。 | [media](media.md)。**完了（M5 cut）** | M3/M6 |
| F21 | item ごとに中央 resource accounting を行う設計へ拡張すると atomic/lock contention が hot path のボトルネックになる。現行 pool も resource owner/上限を表さない。 | [runtime](runtime.md)、[performance](performance.md)。**完了（M5）** | M5 |
| F22 | multi-input filter が goroutine の到着順で入力を選ぶ経路を持ち、mixer、sidechain、A/V sync、EOF の意味が scheduler timing に依存し得る。 | [runtime](runtime.md)、[performance](performance.md)。M5 で generic fan-in の lifecycle/ownership を整理し、M7-1 で `SerialFanIn` の callback 同期直列化と input ordinal を確定した。Serial input buffer のない direct な single synchronous execution island で単一 Router producer が emit する場合だけ call 順を physical order として扱い、buffer/fan-out/concurrent producer の cross-track physical interleave は契約しない。MP4 correctness/exact は per-track の `Packet.Sequence`、PTS/DTS/duration、sample table を基準にし、physical interleave の変更を semantic loss としない。Stable/byte reproducibility は execution signature と別 ordered policy/backpressure を実 consumer とともに追加する。 | M5/M7 |
| F23 | 全 plugin に Start/Close、goroutine/channel、手動 ownership を要求する一方、単一 item API だけでは codec/mixer/session を表現できない。 | [plugins](plugins.md)、[runtime](runtime.md)。**完了（M5）** | M3/M5 |
| F24 | data packet、`io.EOF`、channel close、would-block、Flush、dynamic stream event、final parameters の状態が重なっている。 | [runtime](runtime.md)。**完了（M5）** | M5 |
| F25 | Registry、manifest、descriptor、Plan の mutable/shallow copy により、構築後の変更、race、selection の非決定性が起こり得る。 | [architecture](architecture.md)、[runtime](runtime.md)。**完了（M5 cut）** | M2/M4 |
| F26 | generic reflection clone が pointer、slice、map、interface、unexported field の意味を推測し、大きな buffer の共有/複製も不明確にする。 | [config](config.md)、[media](media.md)、[runtime](runtime.md)。**完了（M5 cut）** | M2/M3 |
| F27 | buffer/plane/layout/size の検証が境界に集約されず、panic/corruption と hot-loop の重複 check の両方を招き得る。 | [media](media.md)、[runtime](runtime.md)。**完了（M5 cut）** | M3 |
| F28 | invalid manifest、default、config/schema の解決失敗を catalog が黙って省略する経路があり、「未導入」と「壊れた plugin」を区別できない。 | [plugins](plugins.md)、[config](config.md)。**完了（M5 cut）** | M2 |
| F29 | CLI、WASM、HTTP/example が resolver、Job、progress、error、lifecycle を個別に持ち、同じ要求でも default と結果がずれ得る。 | [surfaces](surfaces.md)。**library/CLI の最短経路完了（M6）**。WASM/HTTP/example は M9 | M6/M9 |
| F30 | 公式 CLI が plugin を固定 import する一方、明示 composition を低水準 API だけにすると、通常利用者と custom Host 作者のどちらかへ過剰な負担が移る。 | [plugins](plugins.md)、[surfaces](surfaces.md)、[experience](experience.md)。**最短経路完了（M6）**。command だけが standard composition を持ち、CLI library は Host injection、custom composition は `Set.Add` を使う | M6/M9 |
| F31 | Go の live manifest/Plan/error を wire DTO に流用し、TypeScript 側も固定 role の手書き型と無検証 `JSON.parse` を使うため、内部変更と wire compatibility が結合している。 | [web](web.md)、[surfaces](surfaces.md) | M9 |
| F32 | WASM が `runtime.Gosched` loop、全量 `[]byte`/`bytes.Buffer` copy、global job map、暗黙 lifetime に依存し、large input、cancel、dispose、browser event loop に弱い。 | [web](web.md) | M9 |
| F33 | source code の Git submodule と16 moduleの境界が責務・release independenceに対応せず、横断変更に tag/submodule pointer の同期を要求する。 | [architecture](architecture.md)、[inventory](inventory.md)。**完了（M1）** | M1 |
| F34 | source LICENSE だけでは plugin、transitive dependency、generated data、test asset、npm/WASM artifact の license/provenance を保証できない。 | [supply](supply.md) | M10 |

## P2: 開発・運用・仕上げ

| ID | 監査結果 | 詳細 | 担当 |
|---|---|---|---|
| F35 | 第三者 plugin が Compile purity、lifecycle、ownership、cancel、probe budget、metadata preservation を共通に検証する public testkit がない。 | [quality](quality.md)、[plugins](plugins.md)。**M6 対象の共通 runner と typed case 完了**。残りの専門 contract は担当付き coverage registry から M10 が完成させる | M6/M10 |
| F36 | module ごとの test だけでは dependency direction、CGO-off、scalar/SIMD、generator、wire、license、artifact を repository 全体で保証できない。 | [quality](quality.md)、[supply](supply.md) | M10 |
| F37 | generator runner の対象列挙、並行数、error 集約、出力更新が deterministic/transactional でなく、部分実行を正確に報告しにくい。 | [quality](quality.md) | M10 |
| F38 | test runner が `bufio.Scanner` の既定64 KiB上限と不十分な `scanner.Err()` 処理に依存し、長い失敗出力を欠落させ得る。 | [quality](quality.md) | M10 |
| F39 | 多数の module/artifact を順次 publish する release tool は、途中失敗時の状態、dependency order、resume を十分に表さない。 | [supply](supply.md)、[quality](quality.md) | M10 |
| F40 | JS/WASM artifact と Go/TinyGo/Bun/bindgen、source commit、dependency、license、digest の対応が一つの release manifest に固定されていない。 | [supply](supply.md)、[web](web.md) | M10 |
| F41 | output transaction が CLI の temporary file/rename に閉じ、mux Finalize、Flush/Sync、Windows replace、object-store commit、multi-output partial failure と分離している。 | [access](access.md)。**contract 完了（M5）、local file transaction 完了（M6）**。object store と multi-output は実 consumer の milestone | M5/M6 |
| F42 | pipeline、CLI progress、runtime metrics、WASM/HTTP reporting が別々の状態計算を持ち、意味と観測 overhead が重複する。 | [runtime](runtime.md)、[surfaces](surfaces.md)。**runtime event model 完了（M5）、bounded CLI projection 完了（M6）**。WASM/HTTP projection は M9 | M5/M6/M9 |
| F43 | Go unit test だけでは browser event loop、AbortSignal、worker/main thread、page unload、JS exception、WASM memory を検証できない。 | [web](web.md)、[quality](quality.md) | M10 |
| F44 | example web は production product ではないが、job retention、TTL、concurrency、temporary cleanup、bounded shutdown が不足し、demo process/resource が残り得る。 | [surfaces](surfaces.md)、[web](web.md) | M9 |
| F45 | root README と既存 docs が旧 global registry、audio-only model、古い API/依存方向を示し、新しい利用者・plugin author・core developer の導線が不足する。 | [experience](experience.md)、[quality](quality.md)。**M6 の library/CLI quick start は現行化済み**。全 surface と plugin author guide は M9 | M6/M9 |
| F46 | setup、generate、test、benchmark、license、artifact build の入口が module/submodule/toolchain ごとに分散し、clean checkout からの再現が難しい。 | [quality](quality.md)、[supply](supply.md) | M10 |
| F47 | 旧 factory/resolver/routing/registry、typo、空実装、不要 export、互換 wrapper、責務の混在した巨大 file/command が残っている。 | [architecture](architecture.md)、[inventory](inventory.md)。**旧 contract 削除完了（M5）**。移植時の algorithm 整理は M8 | M5/M8 |
| F48 | web editor が `source/filter/output` と audio main/aux を固定し、catalog descriptor を保存し、Host と別の `compileGraph` を実装するため、未知 component/schema に閉じている。 | [web](web.md) | M9 |
| F49 | FLAC/PCM/MP3 testdata が600 MiBを超え、巨大 decimal snapshot と同一 demo media の複製が product module/download/CI を圧迫する。 | [fixtures](fixtures.md)。**旧 product testdata 削除完了（M5 cut）**。新 fixture policy は M10 | M10 |
| F50 | `Seek` 復元、metadata parse、DSP conversion、Close/remove、shutdown、random ID 等で error を捨てる経路があり、primary failure と cleanup failure を区別できない。 | [runtime](runtime.md)、[quality](quality.md)。**current runtime 完了（M5）** | M5 |
| F51 | floating container tag、root lockを使わないclient build、published旧moduleへfallbackし得るserver build、remote asset、未固定 toolchain が network-off rebuild を妨げる。 | [supply](supply.md) | M10 |
| F52 | `sdk/bits` の独自 `production` tag が assertion semantics を変える一方、release/CIで同値性と使用条件が固定されていない。 | [performance](performance.md)、[quality](quality.md) | M8 |
| F53 | global registry、mutable CPU feature、shallow-copy default、process-wide pool/WASM job map が Host/Job の owner、resource budget、test isolation を迂回する。 | [runtime](runtime.md)、[config](config.md)。**Host/Job owner と旧 global surface、`sdk/dsp` の exported mutable feature は完了（M5 review）**。process snapshot を新 item loop から参照せず、Plan/Program に direct variant を固定する責務は M8 | M2/M5/M8 |
| F54 | node payload grant が下流 queue slot だけを数え、各 operator が処理中に保持する item を数えないため、zero-copy 段が producer の storage を保持したまま grant を使い切り、grant を超える入力の変換が途中で停止する。 | [runtime](runtime.md)。**完了（M6 review）**: `inFlightMultiplier` が reachable node 数と queue slot 数の和を返す。回帰は `standard` の grant 超過変換 test が固定する | M6 |

## 監査結果の利用規則

### M7 ordering proposal history (superseded)

以前の #13 `Order` trait / `timing.Compare`、#14 全 input head の DTS ordered Merge、および旧称 `MergeFanIn` は、bounded per-route queue で合法 MP4 に現実的な deadlock を起こし得るため close/延期した。これらは superseded な履歴であり、現行の finding や完了条件ではない。現行 contract は [decisions](decisions.md) の C22/C24/C26 と [M7-0](m7-0.md) の M7-C03/M7-C09 に従う。

- finding を完了扱いにするのは、対応するロードマップのマイルストーンと詳細資料の完了条件を満たした時である。
- 新経路で解消したが旧経路に同じ問題が残る状態を「完了」と書かない。M5 cut で旧経路が消えた項目は `完了（M5 cut）` へ更新済みである。複数の原因を含む F42、F47、F49、F53 は、完了した責務と後続 milestone の残件を同じ行で明示する。
- 現行 path が移動・削除された後も ID は変更しない。必要なら `inventory.md` の移行先を更新する。
- 新しい問題が既存 finding の原因と同じなら、その行を更新する。独立した受け入れ条件が必要な場合だけ新しい ID を追加する。
- 設計判断と監査結果が衝突した場合は、[decision ledger](decisions.md) を優先し、監査文言または詳細仕様を修正する。
