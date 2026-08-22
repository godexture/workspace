# Roadmap checkpoint

> 実装進捗: **7 / 12 マイルストーン完了（M0〜M6）**。M6 の再完了と最終 verification は 2026-08-17 に記録済み。M7 は M7-0〜M7-3 を実装・レビュー済みで、M7-4 の完了検証に入っている。

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
| M7 | 進行中 | M7-0〜M7-3 は完了。static `Spec.Ports` と typed Router/RoutedReader、input ordinal を保つ `SerialFanIn`、単一 `mdat` の unfragmented MP4 direct reader/writer、aggregate `ScratchMaxBytes` と node-local journal、preserve-all/exact `MapStream`、選択 track subset を一つの `muxLayout` で書く remux、必要時だけ選ぶ MP4 PCM binding までを実装・レビュー済み。M7-4 は conformance を実行 Plan から採取する `testkit.Coverage.Observe`、direct island の emit 順を physical order とする vector、Prepare/Run 別の benchmark、1k/1M の resource gate を追加した。残りは M7-C01〜M7-C12 の traceability 最終確認である。 |
| M8 | 未着手 | 公式 family 移行とともに `ilst`/generic loss/strictness、finite seek を実 consumer から確定し、`_legacy/` を削除する。 |
| M9 | 未着手 | stdin/stdout、WASM、demo、rich selector、pure fragmented/sequential MP4、output boundary spool、device/session Endpoint を扱う。 |
| M10 | 未着手 | milestone と並行できる品質・配布基盤を扱う。 |
| M11 | 未着手 | 移行文書を終端処理し、設計文書を恒久化する。 |

## 現在の注記

- M0〜M6 は完了。M6 の再完了判定、根拠、将来の残件は [M0〜M6 実装監査](review-m0-m6.md#m6-再完了条件) に集約し、領域ごとの現行 contract は [runtime](runtime.md)、[access](access.md)、[config](config.md)、[quality](quality.md) を正本とする。
- 現在の foundation は `flow.Item` の単一 ownership slot、Ledger/Domain/Span の failure evidence、Access snapshot/callback boundary、bounded/loss-aware result である。`Fail` が Ledger への唯一の failure ingress で、`Close` は bounded wait と cleanup の完了を担う。cancel normalization は trusted な `context.Cause` の pure single chain だけを採用し、CLI の `ExitCanceled` は caller context state のみを authority とする。旧 review の経緯は [review-m0-m6](review-m0-m6.md) の superseded 注記から辿る。
- M7-0 は dynamic per-track Shape と過剰な同時 surface 追加を撤回し、static `Spec.Ports`、logical `Many` edge、callback の同期直列化と input ordinal を持つ `SerialFanIn`、explicit MP4 graph、default/exact mapping、unfragmented MP4 remux を M7-1〜M7-4 へ分解した。M7-1〜M7-3 は実装・レビュー済みで、M7-4 は conformance 採取・physical order vector・benchmark・resource gate を追加した。MP4 correctness/exact は track ordinal、`Packet.Sequence`、PTS/DTS/duration、per-track sample table を基準とし、physical interleave の変更を semantic loss としない。複数 `mdat` は silent mishandling せず、bounded disk index/scratch consumer まで unsupported とする。pure fragmented/sequential MP4、output boundary spool、rich selector、generic loss DTO/`ilst`、public seek、別 container の time-ordered fan-in は実 consumer と backpressure 設計が揃うまで延期した。offline preset の scratch 既定値は 64 MiB、`Realtime` は disabled (0) で、実際の node claim 分だけ journal を作成する。M7 の次作業と traceability は [M7-0](m7-0.md) を正本とする。
- M7-3 の実装で確定した二つの product 判断を記録する。(1) `sidx`/`ssix`/`mfra`/`tfra`/`iloc` のように sample table の外へ byte offset を記録する box を持つ movie は、track subset だけでなく全 track の remux も拒否する。remux は常に mdat payload を track 順で書き直すため、preserve-all でもこれらの offset は stale になり得る。(2) `tref` の target track ID は inspection に保持しない。track model を slice 無しで保つ代わりに、`tref` を持つ track を含む subset は fail closed とする。全 track を保つ remux は `trak` を verbatim に複製するため影響しない。
- MP4 の PCM Binding の実 consumer は MP4 → WAVE である。MP4 の mux は preserving remuxer なので MP4 出力は常に copy になり、binding が観測できるのは出力 format が別の wire 表現を要求する場合だけである。little-endian `sowt` は copy、big-endian `twos` は decode/encode を経由する。
- M7-4 で conformance の採取経路を変えた。typed runner は一 stream を一 input port で駆動するため、carrier を持たない MP4 reader と repeated descriptors を受ける mux を `Subject` として表せない。両者は Set を狭めて gate から外れていたので、実行した `plan.Plan` から component を採取する `testkit.Coverage.Observe` を追加し、公式 composition 全体を verify するようにした。MP4 専用の private path は作っていない。
