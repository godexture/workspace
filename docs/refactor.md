# Godec リファクタリング計画

> 実装進捗: **1 / 12 マイルストーン完了（M1 完了、M0 進行中）**
>
> この文書を、リファクタリング全体の概要・実装順・進捗の正本とする。詳細資料は設計仕様を説明するものであり、個別に進捗を管理しない。
>
> ### M0/M1 進行状況メモ（直近更新分）
>
> M1 は完了。M0 は、repository 全体を対象にした `tools/cmd/differential` の横断実行を baseline/CI の必須証跡から外した後、その撤回を M0 の文書・manifest・完了宣言へ正しく反映しきれていない箇所と、filter chain benchmark の計測範囲、baseline manifest の再現性、convolver end-to-end test の resource/observation 不備が [checkpoint.md](refactor/checkpoint.md) の再監査で見つかり、進行中に差し戻した。詳細と再判定の順序は checkpoint.md を正本とする。

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

進捗状態は `未着手`、`進行中`、`完了` の三つを使う。マイルストーンは、その行の完了条件と、詳細資料内で同じ milestone ID を明示した固有の完了条件を満たした時だけ `完了` にする。詳細資料末尾の「文書全体の完了条件」は最終設計の gate であり、参照しただけで先行 milestone にすべて遡及適用しない。

| ID | 状態 | マイルストーン | 完了時に得られるもの | 詳細 |
|---|---|---|---|---|
| M0 | 進行中 | 現行基準の固定 | decode/encode/roundtrip、metadata伝播、現行stream経路、failure semantics、allocation/profile、対象別 differential/paired benchmark の再現可能な比較基準 | [performance](refactor/performance.md)、[quality](refactor/quality.md) |
| M1 | 完了 | repository と release topology の再編 | source monorepo、最終 `plugin/<family>` path、単一の設計期 release train、一方向の module DAG、clean-checkout bootstrap | [architecture](refactor/architecture.md)、[inventory](refactor/inventory.md) |
| M2 | 未着手 | identity・catalog・config contract | marker identity、immutable `Set`/Catalog、typed config schema、構造化 diagnostic | [plugins](refactor/plugins.md)、[config](refactor/config.md) |
| M3 | 未着手 | media・metadata・I/O contract | open schema、typed port、stream/time/packet、Binding、Access Provider、Endpoint | [media](refactor/media.md)、[access](refactor/access.md)、[scope](refactor/scope.md) |
| M4 | 未着手 | planner と Program | pure `Compile`、bounded solver、graph validation、説明可能な `Plan`、private `Program` | [planner](refactor/planner.md)、[runtime](refactor/runtime.md) |
| M5 | 未着手 | runtime と ownership | execution island、move/fan-out/COW、cancel、queue、Finalize、transactional Open/Close | [runtime](refactor/runtime.md)、[performance](refactor/performance.md) |
| M6 | 未着手 | 最初の縦断経路 | WAVE + PCM の demux → decode → encode → mux が新設計だけで動く | [media](refactor/media.md)、[plugins](refactor/plugins.md)、[quality](refactor/quality.md) |
| M7 | 未着手 | multi-stream と保存優先の既定動作 | 複数入出力、mapping、stream copy、metadata raw preservation、loss report | [planner](refactor/planner.md)、[surfaces](refactor/surfaces.md)、[media](refactor/media.md) |
| M8 | 未着手 | 公式 plugin contract の移行 | M1で固定したfamily path上で、MP3、FLAC、audio processor が Parser/Binding/variant/typed audio contract を使い、公式plugin間の直接依存を解消する | [audio](refactor/audio.md)、[performance](refactor/performance.md)、[inventory](refactor/inventory.md) |
| M9 | 未着手 | 利用 surface の移行 | library、CLI、WASM、非 production demo が同じ Host/Job/Plan/Result を使う | [surfaces](refactor/surfaces.md)、[web](refactor/web.md)、[experience](refactor/experience.md) |
| M10 | 未着手 | 品質・配布基盤 | conformance testkit、root CI、外部 corpus/asset の取得tier・provenance、hermetic build、SBOM/NOTICE、release plan | [quality](refactor/quality.md)、[fixtures](refactor/fixtures.md)、[supply](refactor/supply.md) |
| M11 | 未着手 | 旧経路の削除と拡張性の実証 | 旧 core/SDK/routing/registry と互換層を削除し、未知 video/subtitle plugin を core 無変更で追加 | [inventory](refactor/inventory.md)、[findings](refactor/findings.md)、[architecture](refactor/architecture.md) |

M0〜M5 は foundation を固めるための先行作業である。M1 は将来の reflection/marker identity を変えないために最終 family package path までを固定するが、foundation package の責務分割と plugin 間の contract 分離までは要求しない。それらは M2/M3/M8 で行う。M6 で最小の実用経路を完成させ、その経路を壊さず M7〜M9 へ広げる。M10 は各段階と並行して整備するが、release gate を満たすまでは完了としない。M11 では旧新二経路を残さず、stable v1 前に必要な module split を確定する。

## 読み方

知りたい内容から、次の資料を参照する。

| 知りたいこと | 読む資料 |
|---|---|
| 確定した方針、延期した判断 | [decisions.md](refactor/decisions.md) |
| M0/M1 完了までの backlog と再監査の解消記録 | [checkpoint.md](refactor/checkpoint.md) |
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

設計の背景を追う場合は、`decisions → architecture → 対象領域の資料` の順で読む。実装対象を探す場合は、上のロードマップから対象マイルストーンを選び、`findings` と `inventory` で現行コードへの対応を確認する。

## 確定済みの境界

詳細は [decision ledger](refactor/decisions.md) の C1〜C16 を正本とする。特に実装中に崩してはならない境界は次である。

- plugin の基本導入は static import と明示 composition である。
- in-process plugin の trust は組み込む利用者が決める。Host の panic recovery は sandbox ではない。
- metadata の表現不能項目は既定で warning/loss report とし、strict failure は opt-in である。
- offline の既定は `Fast + Repeatable` であり、schedule 由来の `Variable` は opt-in である。
- Access contract は I/O capability を表すが、Godec 固有の権限管理 system は提供しない。
- HTTP server は固定公式 plugin を使う小さな demo/reference であり、production service ではない。
- 設計期間は monorepo・単一 product release trainとし、contract 安定後に規格 family 単位の独立 release を可能にする。
- 後方互換 shim を残さず、公式利用側を同時に移して旧経路を削除する。

初期実装を妨げず延期した事項は dynamic install、remote plugin protocol、live dynamic topology の既定 policy、公式 hardware accelerator 範囲である。これらは [Deferred decisions](refactor/decisions.md#deferred-without-blocking-the-first-implementation) に記録する。

## 全体の完了条件

M0〜M11 に加え、少なくとも次をすべて満たした時にリファクタリング完了とする。

- 第三者 package が core/surface を編集せず、新しい schema、component、format、codec、metadata、Provider、Endpoint を追加できる。
- 通常利用者は `standard.NewHost()` 相当の短い経路で利用でき、custom composition も通常の Go コードだけで完結する。
- planner は候補 runtime を生成せず、同じ入力 snapshot から決定的で説明可能な Plan を作る。
- linear data path は reflection、hop ごとの allocation/refcount、node ごとの goroutine/channel、観測用 atomic を必須にしない。
- cancel、panic、partial Open、fan-out、Finalize/Commit failure で item、goroutine、resource、temporary output を leak しない。
- 無指定出力は copy/remux と情報保持を優先し、metadata/stream loss を黙って発生させない。
- 公式 standard distribution は CGO なしで build でき、性能・再現性 contract を differential test と paired benchmark で検証できる。
- full corpus を通常 module download へ含めず、toolchain、dependency、asset、license、SBOM、artifact provenance を release ごとに追跡できる。
- CLI、WASM、demo web は Host の planner/runtime を再実装せず、未知の第三者 component/schema を固定 role の変更なしに扱える。
- 旧 factory/resolver/routing/registry/SDK 経路、互換 wrapper、不要 export、source code の Git submodule が残っておらず、data/asset submodule は任意取得の test/demo dependency に限定されている。
