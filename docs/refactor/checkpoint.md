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
- M6 後の review で `flow` の所有権 contract を作り直した。所有権を pointer で渡す cell (`flow.Item`) が表し、規則は「作った item と受け取った item は `defer ... Drop()` する」の一つになった。`Input`/`Owned`/`Shared` の 3 型と Processor/Joiner で異なる 2 つの規則、runtime 側の item panic cleanup、`Scope` の cleaner が消えた。所有権を表す cell は `Item` だけで、`noCopy` により複製を `go vet` が検出する。bounded queue は cell を保持するため、生の値と drop trait を持ち回す経路が無い。call stack の外へ payload を置く側は cell を heap に置いて pointer で保持するため、生の値と drop trait を持つ複製可能な token は public にも internal にも存在しない。第三者 `Drop` は runtime mutex の外で呼び、複数 owner の後片付けは一件が panic しても全件を解放してから報告する。その報告は捨てない。fan-in は batch ごとの解放失敗をその場で返し、`Execution.Discard` は全 task の failure を結合し、Host は通常終了でも cleanup 経路でも Result へ載せる。panic は task の戻り値を捨てるため、戻り道の cleanup failure は task の `Scope` が node identity と並べて運び、`internal/task` の boundary が recover 時に読む。edge の drain task は取り出し中の item を task 単位の flag で持ち、consumer の error・panic・declined payload の解放 panic のいずれでも戻り道で `Abandon` する。count は正しくなるが edge は quiescent にならないため、barrier は idle を騙らず、失敗した task の cancel で終わる。fan-in も同じで、input を EOF まで drain し戻り道の cleanup も成功した join だけが barrier を成立させる。複数 cell をまとめて解放する helper は runtime だけが使うため `internal/run/release` にある。詳細は [runtime](runtime.md#ownership) を正本とする。
- [M0〜M6 実装監査](review-m0-m6.md) の R-01〜R-12 を実装・文書・回帰 test へ反映し、再監査で再 open した R-01、R-02、R-06、R-07、R-10、R-11 も閉じた。config は secret formatting、codec 合成の normalization、第三者 callback の panic boundary を閉じ、typed patch entry を schema key に束ねて `Resolved`/`ResolvedView` を phase ごとの snapshot にした。media は immutable read view、buffer layout の checked arithmetic、WAVE 予約 slot `JUNK` の byte 保持を得た。local file は content identity を報告し、Host が phase 間で照合する。public testkit は bounded Suggest を第三者と同じ入口で検査し、coverage owner は実在 milestone に限る。`cli.Run` は独立な failure をすべて返す。
- 次の作業は M7 着手前監査である。MP4 (ISO BMFF)、multi-stream、mapping、stream copy、loss report、seek、`QueuePolicy.Window` の責務を、各単位が端から端まで green の実 consumer を持つ sub-unit へ分割してから実装する。**loss report と metadata raw preservation は実 consumer として MP4 の metadata encoding (`ilst`/`udta`) を要するため、分割時にその単位を落とさない。** この監査と分割自体は M6 review 修正のスコープに含めない。
