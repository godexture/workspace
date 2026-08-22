# Godec リファクタリング計画

> この文書を、リファクタリング全体の目標・ロードマップ定義・実装順の正本とする。M0〜M11 の状態、直近の成果、次の作業は [checkpoint.md](refactor/checkpoint.md) を進捗の正本とする。

## 目標

Godec を、Go アプリケーションへ組み込める pure-Go のトランスコーディング基盤として再設計する。第三者は core を変更せず、Go package を import して component を追加するだけで、新しい stream schema、format、codec、parser、metadata encoding、processor、Access Provider、Endpoint を利用できるものとする。

長期的には FFmpeg を代理できる拡張性を目指す。ただし、すべての規格・protocol・device を公式実装として同時に提供することや、plugin marketplace、production HTTP service、in-process plugin sandbox を core の責務にはしない。

## 目標構成

```text
application / CLI / WASM
            |
            v
         standard --------> official plugins
            |                      |
            +----------+-----------+
                       v
          foundation contract + Host
                       |
                       v
             private planner/runtime
```

設計上の要点は次のとおりである。

- 依存方向を `foundation <- plugins <- standard <- surfaces` に固定する。
- global `init` registry を廃止し、immutable な `plugin.Set` を `Host` へ明示的に渡す。
- plugin/component identity は専用 marker type から自動導出し、手書きの衝突回避 ID を要求しない。
- 万能な `Frame` と固定 role を廃止し、open schema と typed port を使う。
- Format、Codec、Parser、Carrier、Metadata Encoding、Binding を別責務にする。
- semantic transformation は純粋な `Compile` に一度だけ記述し、選択後の `Open` が実行 object を生成する。
- planner、scheduler、queue、resource、task、diagnostic は Host が所有する。
- hot path に reflection、文字列検索、必須 allocation、node ごとの必須 goroutine/channel、観測用の必須 atomic を持ち込まない。
- 通常の出力既定は入力の format、codec、stream、metadata を維持し、可能なら copy/remux を選ぶ。
- 公式 codec は CGO を必須にせず、`unsafe` と CGO 不要 SIMD を許容する。

確定判断の正確な文言と延期事項は [decision ledger](refactor/decisions.md) を参照する。

## 実装ロードマップ

各マイルストーンは、この表の成果と、詳細資料内で同じ milestone ID を明示した固有の完了条件を定義する。詳細資料末尾の「文書全体の完了条件」は最終設計の gate であり、参照しただけで先行 milestone にすべて遡及適用しない。

| ID | マイルストーン | 完了時に得られるもの | 詳細 |
|---|---|---|---|
| M0 | 現行基準の固定 | decode/encode/roundtrip、metadata伝播、現行stream経路、failure semantics、対象別 correctness/performance の再現可能な比較基準 | [performance](refactor/performance.md)、[quality](refactor/quality.md) |
| M1 | repository と release topology の再編 | source monorepo、最終 `plugin/<family>` path、単一の設計期 release train、一方向の module DAG、clean-checkout bootstrap | [architecture](refactor/architecture.md)、[inventory](refactor/inventory.md) |
| M2 | identity・catalog・config contract | marker identity、immutable `Set`/Catalog、typed config schema、構造化 diagnostic | [plugins](refactor/plugins.md)、[config](refactor/config.md) |
| M3 | media・metadata・I/O contract と walking skeleton | open schema、typed port、stream/time/packet、Binding、Access Provider、Endpoint。test 用の trivial な schema/format/codec で bytes → packet → frame → packet → bytes が新 contract だけで通る | [media](refactor/media.md)、[access](refactor/access.md)、[scope](refactor/scope.md) |
| M4 | planner と Program | pure `Compile`、bounded solver、graph validation、説明可能な `Plan`、private `Program`。walking skeleton を planner 経由で通す。あわせて container を持たない実 PCM を最初の実 codec として通し、trivial component が自分で作った要求にしか答えない状態を抜ける | [planner](refactor/planner.md)、[runtime](refactor/runtime.md)、[media](refactor/media.md) |
| M5 | runtime と ownership、旧 contract 層の切断 | execution island、move/fan-out/COW、cancel、queue、Finalize、transactional Open/Close。walking skeleton を新 runtime で通し、旧/新 runtime を同一 harness で比較する paired benchmark を取る。その直後に旧 contract 層を一括削除し、未移植の algorithm を `_legacy/` へ隔離する | [runtime](refactor/runtime.md)、[performance](refactor/performance.md)、[inventory](refactor/inventory.md) |
| M6 | 最初の実 container 経路と最短 surface | M4 の実 PCM へ WAVE container と file Provider/session を足し、acquire → shared probe → inspect → demux → decode → encode → mux が新設計だけで動く。capability 不足時の明示 spool、temporary quota/cleanup、`standard` composition、`integration` module、public testkit の最小形、`standard.Convert` と `cmd/godec` の最短経路を含む。out-of-tree 相当 plugin による拡張性 gate を最初に通す | [media](refactor/media.md)、[access](refactor/access.md)、[plugins](refactor/plugins.md)、[quality](refactor/quality.md)、[capability](refactor/capability.md)、[experience](refactor/experience.md) |
| M7 | multi-stream・保存優先の既定動作 | unfragmented MP4 (ISO BMFF) を実 consumer として、static `Many` port と ordered repeated descriptors、typed Router、既存 `flow.Joiner` の `SerialFanIn`、default preserve-all、exact `MapStream`、raw preservation を RandomRead+StableSize/RandomWrite file-to-file 経路で通す。Inspection は shared immutable な source range/summary に限定し、duration/sample 数/opaque raw payload 長に依存しない constant-RAM、WAVE unknown chunk/trailer の range preservation、quota 付き Host-owned table journal も M7 の契約に含める。`flow.Direct` を宣言した port を単一 routed producer が駆動する direct island では、その call 順を physical mdat order として扱い、宣言のない generic Serial 構成の cross-track physical interleave は契約しない。MP4 correctness/exact は track ordinal、`Packet.Sequence`、PTS/DTS/duration、per-track sample table で判定する。音声は必要時だけ PCM を bind し、video/subtitle/data track は stream copy する | [M7-0 contract](refactor/m7-0.md)、[planner](refactor/planner.md)、[surfaces](refactor/surfaces.md)、[media](refactor/media.md)、[scope](refactor/scope.md) |
| M8 | 公式 plugin contract の移行 | M1 で固定した family path 上で、MP3、FLAC、audio processor が Parser/Binding/variant/typed audio contract を使い、公式 plugin 間の直接依存を解消する。移行した metadata encoding を consumer として `ilst` mapping・generic loss/strictness を、MP4 と MP3/FLAC decode path を consumer として finite seek を確定し、`_legacy/` を削除する | [audio](refactor/audio.md)、[performance](refactor/performance.md)、[inventory](refactor/inventory.md)、[capability](refactor/capability.md) |
| M9 | 利用 surface の完成 | library、CLI、WASM、非 production demo が同じ Host/Job/Plan/Result を使う。M6 の最短経路を全 flag、versioned DTO、catalog 駆動 editor へ広げ、rich stream selector を確定する。stdin/stdout・remote/streaming consumer とともに pure fragmented/sequential MP4 と output boundary spool alternative を追加する。playback は typed Endpoint と専用 command へ分離し、M6 の拡張性 gate を surface 越しに再実行する | [surfaces](refactor/surfaces.md)、[web](refactor/web.md)、[experience](refactor/experience.md) |
| M10 | 品質・配布基盤 | conformance testkit の完成、root CI、外部 corpus/asset の取得tier・provenance、hermetic build、SBOM/NOTICE、release plan | [quality](refactor/quality.md)、[fixtures](refactor/fixtures.md)、[supply](refactor/supply.md) |
| M11 | 拡張性の維持確認と module 境界の確定 | M6/M9 で通した拡張性 gate が維持されていること、全 finding が閉じたこと、不要 export が残っていないことを確認する。stable v1 前に必要な family module split を実施または不要と判断する | [findings](refactor/findings.md)、[architecture](refactor/architecture.md)、[capability](refactor/capability.md)、[inventory](refactor/inventory.md) |

M0〜M5 は foundation を固めるための先行作業である。M1 は将来の reflection/marker identity を変えないために最終 family package path までを固定するが、foundation package の責務分割と plugin 間の contract 分離までは要求しない。それらは M2/M3/M8 で行う。M5 の末尾で旧 contract 層を切断し、以後は tree に compile される実装が常に一つだけになる。M6 で最小の実用経路を完成させ、その経路を壊さず M7〜M9 へ広げる。M10 は各段階と並行して整備するが、release gate を満たすまでは完了としない。M11 は削除作業ではなく、拡張性が維持されていることの確認と module 境界の確定に充てる。

M7 は実装開始前に [M7-0 contract](refactor/m7-0.md) で公開境界、product semantics、R-17 traceability、sub-unit の依存順、固有完了条件を確定した。M7-2 は明示された 1 input/1 output の MP4 graph に限定し、standard が Many terminal と mapping を自動解決する経路は M7-3 で追加する。M7 の実装・完了判定ではこの文書の M7 行だけでなく、同 contract の M7-C01〜M7-C12 をすべて満たす。

この製品は未 release であるため、移行期間中に main が全機能を提供し続けることを要求しない。同じ完成形へ至る複数の経路があるとき、優先するのは (1) 実装時の安全性、(2) 変更差分の小ささである。

milestone ID は追加・再採番しない。粒度と担当の調整は次の規則で行う。

- **contract より先に動く経路を作る。** [architecture](refactor/architecture.md#移行規則) の「最小の新縦断経路を先に作る」に従い、M3 は test 用の trivial component で端から端まで通す walking skeleton を成果に含める。M4 と M5 はその skeleton を壊さずに planner と runtime を差し込む。consumer のいない contract を積み上げない。
- **risk の高い仮定を早く現実に当てる。** trivial component は自分で作った要求にしか答えないため、skeleton は「部品が噛み合うか」を示しても「現実の規格に耐えるか」は示さない。したがって container を持たない実 PCM を M4 の成果に含め、実 codec が planner と contract の最初の consumer になるようにする。M6 はその PCM へ WAVE container を足す形になり、書き直しではなく拡張になる。設計の妥当性を議論だけで決める区間を短くする。
- **機構を作る milestone は、その機構の実 consumer を同じ milestone 内に持つ。** 上の規則を全 milestone へ一般化したものである。M7 の multi-stream、mapping、stream copy は、WAVE/MP3/FLAC がいずれも単一 audio stream であるため公式 family に consumer を持たない。したがって MP4 (ISO BMFF) を M7 の成果に含める。音声は PCM を bind し、video/subtitle/data は stream copy のみ扱うため `media/video`/`media/subtitle` の frame 型は不要である。MP4 は per-track timescale が `timing` の rescale を、sample entry が codec Binding を、未知 box が narrow raw preservation を、それぞれ実規格で検証する。M7 は Inspect/remux の bounded random read/re-scan と、固定 page・明示 quota の Host-owned table journal を許すが、output boundary spool とは区別する。M7-2 の mux は `flow.Direct` を宣言した port を単一 routed producer が駆動する direct island の emit call 順を mdat の physical order として扱う。宣言のない generic Serial 構成の cross-track physical interleave は契約しない。MP4 demux は per-track cursor を sample offset で merge して source の格納順に emit するため、remux は入力の interleave をそのまま保ち、全 track を保つ remux は入力と byte 一致する。correctness/exact の基準は track ordinal、`Packet.Sequence`、PTS/DTS/duration、per-track sample table、格納順の保持である。入力が持たない physical order（時刻順 interleave の新規構築）を必要とする consumer は、execution signature と別 ordered policy/backpressure を別途要求する。`SerialFanIn` は ordering algorithm ではなく callback の同期直列化であり、ISO BMFF のための global DTS interleave は要求しない。pure fragmented/sequential I/O、output boundary spool、rich selector、generic loss DTO、`ilst` mapping、public seek、別 container の time-ordered fan-in は、それぞれ実 consumer と backpressure policy が揃うまで追加しない。
- **旧 contract 層は M5 の末尾で一括削除する。** [C12](refactor/decisions.md) の「後方互換層を残さない」を、milestone ごとの部分削除ではなく一度の切断で満たす。実装事故の主因は同じ概念の実装が 2 つ compile されていることであり、`core/node` と `flow`、`core/registry` と `plugin.Set`、`core/domain/media` と `media/*`、`sdk/engine` と `flow.Processor`、`sdk/config` と `config` がそれに当たる。旧 stack の最後の用途は M5 完了条件の paired benchmark なので、それを取り終えた時点が切断点になる。削除対象と `_legacy/` 隔離の範囲は [inventory](refactor/inventory.md#m5-の切断) を正本とする。
- **移植参照は `_legacy/` に置き、compile 対象から外す。** 旧実装には規格上の細部（RIFF chunk の境界処理、MP3 header の quirk、FLAC の LPC/rice 実装）が残っており、移植時に読む価値がある。一方その価値は algorithm 層にあり、contract 層にはない。未移植の algorithm は `_legacy/` へ移す。`_` 始まりの directory は go tool が完全に無視するため、build されず、import できず、`go list ./...` にも現れない。`go build ./...` と `go test ./...` の green は常に新 stack だけを意味する。`_legacy/` は M8 の family 移行完了とともに削除する。
- **拡張性は M11 を待たずに実証する。** 「第三者が core を変更せず追加できる」はこの計画の中心目標であり、最後の milestone で初めて確かめる対象にしない。foundation test 内の第三者相当 fixture は planner、runtime、surface を通っていない。実 container 経路が動く M6 で out-of-tree 相当 plugin による拡張性 gate を最初に通し、M9 で surface 越しに再実行し、M11 はそれが維持されていることの確認に充てる。投資の対価が可視化される時期も早まる。
- **正しさは旧実装ではなく仕様で確認する。** 正式 release 前のため旧実装との出力一致は要求しない。代わりに M6、M7、M8 の完了確認で対象 family の conformance corpus と lossless roundtrip exact を実行する。M0 baseline は commit `4429711a` に固定されており、`_legacy/` と併せて差異が意図的か bug かを判断する参照として使える。意図的な差異は [capability](refactor/capability.md#挙動変更の記録) へ記録する。
- **消える機能を決めてから消す。** 現行機能の維持/変更/廃止は [capability](refactor/capability.md) で管理する。`未定` の行が残っている機能の旧経路を削除しない。
- **M10 の一部は M6 より前に必要になる。** 公式 plugin も第三者と同じ public testkit を使う以上、testkit と `integration` module の最小形は M6 の成果に含める。M10 は CI matrix、corpus tier、hermetic build、release plan の完成を担当する。
- **finding には担当 milestone を書く。** [findings](refactor/findings.md) の規則は「finding を完了扱いにするのは、対応するロードマップのマイルストーンと詳細資料の完了条件を満たした時」と定めており、担当が分かっている前提に立つ。担当列のない finding は完了判定ができないため、全 finding に担当 milestone を持たせる。
- **旧/新 runtime の比較は M5 で harness に接続する。** M0 baseline は旧 pipeline を測っているため、旧 contract 層を切断した後では比較対象が失われる。この paired benchmark を取り終えることが切断の前提条件であり、M5 完了条件の一部である。
- **public seek は実 consumer が揃う M8 で機構と format 実装を同時に確定する。** M7 は MP4 sample index を remux の random read/re-scan に使うが、decoder preroll、processor reset、Result projection のない段階で graph operation を先行追加しない。MP4 と移行後の MP3/FLAC path を consumer として、M8 完了時点で旧経路と同等以上の seek 能力を確認する。
- **完了条件は着手前に書く。** milestone の固有条件が詳細資料に存在しない状態で実装へ入らない。条件は「何を作るか」だけでなく「何を作らないか」と「未完了として残すもの」を明示する。M3 の 3 単位で最も効いたのは [media](refactor/media.md#m3-完了条件) と [access](refactor/access.md#m3-完了条件) の「M3 では最小の型だけを置き、詳細は実際の consumer を作る milestone で決める」という明文だった。実装がまだ見えていない先の milestone まで条件を先取りすると、書き直しが増えて正本の信頼が落ちるため、着手直前の milestone を対象にする。
- **引き継いだ責務を完了条件へ再掲する。** milestone の着手前監査では、先行 milestone が「宣言のみ」「未完了」「後続で実装」とした全項目を検索し、実 consumer、完了条件、またはさらに先へ送る明示記録のいずれかへ一件ずつ対応付ける。新規 export だけを監査対象にせず、既存宣言の担当漏れを残さない。
- **consumer を持たない export を残さない。** 各 milestone の完了条件に「新規 export ごとに、呼び出し元を示すか、宣言のみとして [scope](refactor/scope.md) の分類節へ consumer を作る milestone とともに記載する」を含める。M3 の 3 単位すべてで、呼び出し元も実装者もない public API が繰り返し発生した。宣言だけの contract 自体は milestone の性質上必要だが、担当を書けないものは今置くべきでない印である。

## 読み方

知りたい内容から、次の資料を参照する。

| 知りたいこと | 読む資料 |
|---|---|
| 確定した方針、延期した判断 | [decisions.md](refactor/decisions.md) |
| 全マイルストーンの状態、直近の成果、次の作業 | [checkpoint.md](refactor/checkpoint.md) |
| M0〜M6 の実装監査、未解消 finding、再完了条件 | [review-m0-m6.md](refactor/review-m0-m6.md) |
| 実装エージェントへ渡す milestone ごとの作業手順 | [task/](refactor/task/) |
| M0 baseline の再現手順・toolchain・所見の要約 | [baseline.md](refactor/baseline.md) |
| package/module/repository の境界、依存方向 | [architecture.md](refactor/architecture.md) |
| plugin identity、composition、component lifecycle、custom Host | [plugins.md](refactor/plugins.md) |
| config、default、preset、validation、surface projection | [config.md](refactor/config.md) |
| typed data plane、stream/packet/time、Format/Codec/Metadata の関係 | [media.md](refactor/media.md) |
| audio sample 表現、filter chain、in-place/COW | [audio.md](refactor/audio.md) |
| object I/O、non-seekable input、transaction、live/device endpoint | [access.md](refactor/access.md) |
| FFmpeg 代替に必要な拡張点の全体像 | [scope.md](refactor/scope.md) |
| bridge 探索、自動挿入、cost/effect、default copy | [planner.md](refactor/planner.md) |
| graph 実行、ownership、queue、cancel、Finalize、observability | [runtime.md](refactor/runtime.md) |
| Fast/Stable/Portable、variant、再現性、性能回帰防止 | [performance.md](refactor/performance.md) |
| Job、mapping、catalog、CLI、WASM、demo HTTP | [surfaces.md](refactor/surfaces.md) |
| JavaScript wire、WASM lifecycle、catalog-driven editor | [web.md](refactor/web.md) |
| 利用者・plugin 開発者・core 開発者の体験 | [experience.md](refactor/experience.md) |
| test 構成、testkit、CI、generator、runner | [quality.md](refactor/quality.md) |
| fixture、巨大 corpus、example asset | [fixtures.md](refactor/fixtures.md) |
| dependency、toolchain、license、SBOM、release | [supply.md](refactor/supply.md) |
| 現行コードで確認した問題の索引 | [findings.md](refactor/findings.md) |
| 現行 package ごとの移動・統合・削除先 | [inventory.md](refactor/inventory.md) |
| 現行機能の維持・変更・廃止と挙動変更の記録 | [capability.md](refactor/capability.md) |

設計の背景を追う場合は、`decisions → architecture → 対象領域の資料` の順で読む。実装対象を探す場合は、上のロードマップから対象マイルストーンを選び、`findings` と `inventory` で現行コードへの対応を確認する。

## 文書の終端

これらの資料は二種類に分かれ、M11 で処遇が変わる。

**移行文書**（現行コードまたは移行過程を記述する。M11 で削除する）: この `refactor.md`、[checkpoint](refactor/checkpoint.md)、[findings](refactor/findings.md)、[inventory](refactor/inventory.md)、[capability](refactor/capability.md)、[baseline](refactor/baseline.md)、`baseline.manifest.json`、[task/](refactor/task/)。

**設計文書**（目標設計を記述する。M11 で `docs/design/` へ移し恒久化する）: [decisions](refactor/decisions.md)、[architecture](refactor/architecture.md)、[media](refactor/media.md)、[audio](refactor/audio.md)、[access](refactor/access.md)、[config](refactor/config.md)、[plugins](refactor/plugins.md)、[planner](refactor/planner.md)、[runtime](refactor/runtime.md)、[performance](refactor/performance.md)、[quality](refactor/quality.md)、[fixtures](refactor/fixtures.md)、[supply](refactor/supply.md)、[surfaces](refactor/surfaces.md)、[web](refactor/web.md)、[experience](refactor/experience.md)、[scope](refactor/scope.md)。移す際に「現行実装の監査結果」節と milestone 完了条件節を落とし、目標設計だけを残す。

この二分類に属さず削除済み contract を説明していた `docs/legacy/` は M6 review で削除した。規格 algorithm の移植参照である repository root の `_legacy/` とは別物であり、後者は M8 まで維持する。

設計文書に残すのは、godoc では表現できない横断的な判断（policy vector、planner の探索規則、reproducibility contract、依存方向の原則）に限る。個々の API の形は [experience](refactor/experience.md) の規則どおり各 package の `Example` 関数を正本とし、文書からは参照する。実装済み package を説明する Go code block を文書に残さない。

## 確定済みの境界

詳細は [decision ledger](refactor/decisions.md) の Confirmed 判断を正本とする。特に実装中に崩してはならない境界は次である。

- plugin の基本導入は static import と明示 composition である。
- in-process plugin の trust は組み込む利用者が決める。Host の panic recovery は sandbox ではない。
- metadata の表現不能項目は既定で warning/loss report とし、strict failure は opt-in である。
- offline の既定は `Fast + Repeatable` であり、schedule 由来の `Variable` は opt-in である。
- Access contract は I/O capability を表すが、Godec 固有の権限管理 system は提供しない。
- HTTP server は固定公式 plugin を使う小さな demo/reference であり、production service ではない。
- 設計期間は monorepo・単一 product release trainとし、contract 安定後に規格 family 単位の独立 release を可能にする。
- 後方互換 shim を残さない。旧 contract 層は M5 の末尾で一括削除し、未移植の algorithm だけを compile 対象外の `_legacy/` に移植参照として残す。
- 公式 family は WAVE、PCM、MP4、MP3、FLAC とし、MP4 の video/subtitle は stream copy のみ扱う。video/audio codec の網羅は目標にせず、第三者が同じ contract で追加できることを目標にする。

初期実装を妨げず延期した事項は dynamic install、remote plugin protocol、live dynamic topology の既定 policy、公式 hardware accelerator 範囲である。これらは [Deferred decisions](refactor/decisions.md#deferred-without-blocking-the-first-implementation) に記録する。

## 全体の完了条件

M0〜M11 に加え、少なくとも次をすべて満たした時にリファクタリング完了とする。

- 第三者 package が core/surface を編集せず、新しい schema、component、format、codec、metadata、Provider、Endpoint を追加できる。
- 通常利用者は `standard.NewHost()` 相当の短い経路で利用でき、custom composition も通常の Go コードだけで完結する。
- planner は候補 runtime を生成せず、同じ入力 snapshot から決定的で説明可能な Plan を作る。
- linear data path は reflection、hop ごとの allocation/refcount、node ごとの goroutine/channel、観測用 atomic を必須にしない。
- cancel、panic、partial Open、fan-out、Finalize/Commit failure で item、goroutine、resource、temporary output を leak しない。
- 無指定出力は copy/remux と情報保持を優先し、metadata/stream loss を黙って発生させない。
- 複数 stream を持つ実 container で default/exact mapping と stream copy が動作し、decode 実装を持たない video/subtitle/data stream も情報を失わずに通過できる。
- 公式 standard distribution は CGO なしで build でき、性能・再現性 contract を対象別 differential test と代表 benchmark で検証できる。
- full corpus を通常 module download へ含めず、toolchain、dependency、asset、license、SBOM、artifact provenance を release ごとに追跡できる。
- CLI、WASM、demo web は Host の planner/runtime を再実装せず、未知の第三者 component/schema を固定 role の変更なしに扱える。
- 旧 factory/resolver/routing/registry/SDK 経路、互換 wrapper、`_legacy/`、不要 export、source code の Git submodule が残っておらず、data/asset submodule は任意取得の test/demo dependency に限定されている。
