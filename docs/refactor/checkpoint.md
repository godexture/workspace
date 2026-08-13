# Roadmap checkpoint

> 実装進捗: **7 / 12 マイルストーン完了（M0〜M6）**

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
| M6 | 完了 | file/WAVE/PCM、probe/inspect/spool/transaction、standard/testkit/CLI を一つの Host 経路で完成した。 |
| M7 | 未着手 | 着手前監査と sub-unit 分割を先に行う。 |
| M8 | 未着手 | 完了時に `_legacy/` を削除する。 |
| M9 | 未着手 | stdin/stdout、WASM、demo、device/session Endpoint を扱う。 |
| M10 | 未着手 | milestone と並行できる品質・配布基盤を扱う。 |
| M11 | 未着手 | 移行文書を終端処理し、設計文書を恒久化する。 |

## 現在の注記

- [M6 review 修正](task/m6-fix.md) の 12 単位を実装・文書・回帰 test へ反映し、Access capability の宣言と実 view、file transaction、WAVE truncation、Plan provenance、standard surface を一致させた。
- M6 後の review で `flow` の所有権 contract を作り直した。所有権を pointer で渡す cell (`flow.Item`) が表し、規則は「作った item と受け取った item は `defer ... Drop()` する」の一つになった。`Input`/`Owned`/`Shared` の 3 型と Processor/Joiner で異なる 2 つの規則、runtime 側の item panic cleanup、`Scope` の cleaner が消え、`noCopy` により所有権の複製を `go vet` が検出する。詳細は [runtime](runtime.md#ownership) を正本とする。
- 次の作業は M7 着手前監査である。MP4 (ISO BMFF)、multi-stream、mapping、stream copy、loss report、seek、`QueuePolicy.Window` の責務を、各単位が端から端まで green の実 consumer を持つ sub-unit へ分割してから実装する。**loss report と metadata raw preservation は実 consumer として MP4 の metadata encoding (`ilst`/`udta`) を要するため、分割時にその単位を落とさない。** この監査と分割自体は M6 review 修正のスコープに含めない。
