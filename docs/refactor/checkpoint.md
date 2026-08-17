# Roadmap checkpoint

> 実装進捗: **7 / 12 マイルストーン完了（M0〜M6）**。M6 の再完了と最終 verification は 2026-08-17 に記録済み。M7-0 を static multi-stream vertical slice へ是正し、次は M7-1 である。

この文書を M0〜M11 の状態、直近の成果、次の作業、blocker の正本とする。目標と完了条件は [refactor.md](../refactor.md#実装ロードマップ)、各領域の contract はリンク先の設計資料を正本とする。完了までの個別修正や監査の時系列は Git 履歴で追跡し、ここへ再掲しない。

## 更新規則

- 状態は `未着手`、`進行中`、`完了` の三つとする。M10 は他の milestone と並行できるため、複数 milestone が同時に `進行中` でもよい。
- 着手、意味のある中間成果、新しい blocker、完了判定の時に、該当行の状態と checkpoint を更新する。
- checkpoint は現在地を判断できる一文にとどめ、完了済み作業の長い列挙、benchmark の単発値、test log は残さない。再現条件や未解消事項が必要なら専用文書へリンクする。
- `完了` は [refactor.md](../refactor.md#実装ロードマップ) の成果と、詳細資料でその milestone に明示された固有条件を満たした場合だけ付ける。

## 進捗

| ID | 状態 | checkpoint |
|---|---|---|
| M0 | 完了 | correctness、failure semantics、metadata/stream、worker/variant、performance の比較条件を [baseline.md](baseline.md) に固定した。 |
| M1 | 完了 | source monorepo、最終 package path、tracked workspace、単一 release train、generator bootstrap を確定した。 |
| M2 | 完了 | diagnostic、typed config、marker identity、immutable plugin composition、catalog validation、Host construction を完成した。 |
| M3 | 完了 | metadata/side/event、Access/Endpoint foundation contract と第三者拡張点を完成した。 |
| M4 | 完了 | typed Compile/Suggest、bounded solver、public Plan/private Program、binding、実 linear PCM を完成した。 |
| M5 | 完了 | typed runtime、ownership/COW、bounded queue、cancel、Finalize、transactional lifecycle を完成し、旧 contract を切断した。 |
| M6 | 完了 | file/WAVE/PCM、probe/inspect/spool/transaction、standard/testkit/CLI の実経路と R-17 の final contract を repository-wide verification まで確認した（2026-08-17）。 |
| M7 | 進行中 | [M7-0 contract](m7-0.md) を static `Many` topology、ordered repeated descriptors、Router、`Joiner + MergeFanIn`、unfragmented RandomRead+StableSize/RandomWrite MP4 vertical slice へ是正した。次は M7-1 の static multi-stream execution。 |
| M8 | 未着手 | 公式 family 移行とともに `ilst`/generic loss/strictness、finite seek を実 consumer から確定し、`_legacy/` を削除する。 |
| M9 | 未着手 | stdin/stdout、WASM、demo、rich selector、fragmented/sequential MP4、spool、device/session Endpoint を扱う。 |
| M10 | 未着手 | milestone と並行できる品質・配布基盤を扱う。 |
| M11 | 未着手 | 移行文書を終端処理し、設計文書を恒久化する。 |

## 現在の注記

- M0〜M6 は完了。M6 の再完了判定、根拠、将来の残件は [M0〜M6 実装監査](review-m0-m6.md#m6-再完了条件) に集約し、領域ごとの現行 contract は [runtime](runtime.md)、[access](access.md)、[config](config.md)、[quality](quality.md) を正本とする。
- 現在の foundation は `flow.Item` の単一 ownership slot、Ledger/Domain/Span の failure evidence、Access snapshot/callback boundary、bounded/loss-aware result である。`Fail` が Ledger への唯一の failure ingress で、`Close` は bounded wait と cleanup の完了を担う。cancel normalization は trusted な `context.Cause` の pure single chain だけを採用し、CLI の `ExitCanceled` は caller context state のみを authority とする。旧 review の経緯は [review-m0-m6](review-m0-m6.md) の superseded 注記から辿る。
- M7-0 は dynamic per-track Shape と過剰な同時 surface 追加を撤回し、static `Spec.Ports`、logical `Many` edge、default/exact mapping、unfragmented MP4 remux を M7-1〜M7-4 へ分解した。fragmented/spool、rich selector、generic loss DTO/`ilst`、seek は実 consumer の M8/M9 へ延期した。M7 の次作業と traceability は [M7-0](m7-0.md) を正本とする。
