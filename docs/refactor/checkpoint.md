# Roadmap checkpoint

> 実装進捗: **3 / 12 マイルストーン完了（M0〜M2）**

この文書を M0〜M11 の状態、直近の成果、次の作業、blocker の正本とする。目標と完了条件は [refactor.md](../refactor.md#実装ロードマップ)、各領域の contract はリンク先の設計資料を正本とする。完了までの個別修正や監査の時系列は Git 履歴で追跡し、ここへ再掲しない。

## 更新規則

- 状態は `未着手`、`進行中`、`完了` の三つとする。M10 は他の milestone と並行できるため、複数 milestone が同時に `進行中` でもよい。
- 着手、意味のある中間成果、新しい blocker、完了判定の時に、該当行の状態と checkpoint を更新する。
- checkpoint は現在地を判断できる一文にとどめ、完了済み作業の長い列挙、benchmark の単発値、test log は残さない。再現条件や未解消事項が必要なら専用文書へリンクする。
- `完了` は [refactor.md](../refactor.md#実装ロードマップ) の成果と、詳細資料でその milestone に明示された固有条件を満たした場合だけ付ける。

## 進捗

| ID | 状態 | checkpoint |
|---|---|---|
| M0 | 完了 | 現行の correctness、failure semantics、metadata/stream、worker/variant、performance の比較条件を固定した。再現条件は [baseline.md](baseline.md) と [baseline.manifest.json](baseline.manifest.json) を参照する。 |
| M1 | 完了 | source monorepo、最終 `plugin/<family>` path、tracked workspace、設計期間の単一 release train、generator bootstrap の `runtime.GOOS` 対応を完了した。 |
| M2 | 完了 | `diagnostic`、typed `config`、marker identity/immutable `plugin.Set`、検証済み `internal/catalog`、`host.New` を追加し、[task/m2-fix.md](task/m2-fix.md) の review 指摘 1〜19 を是正した。[plugins.md](plugins.md#m2-完了条件) と [config.md](config.md#m2-完了条件) の完了条件を逐条確認し、build、vet、対象 race、`--simd` gate（71/71 PASS、runner exit 0）を通過した。 |
| M3 | 未着手 | 次の主対象。固有の完了条件を [media.md](media.md#m3-完了条件)、[access.md](access.md#m3-完了条件)、[scope.md](scope.md#m3-完了条件) に定義した。着手前に [C17](decisions.md#c17-config-snapshot-は-codec-clone-だけで構成する) の実 config 検証を行う。着手時に実装単位を記録する。 |
| M4 | 未着手 | — |
| M5 | 未着手 | — |
| M6 | 未着手 | — |
| M7 | 未着手 | — |
| M8 | 未着手 | — |
| M9 | 未着手 | — |
| M10 | 未着手 | 各 milestone と並行して品質・配布基盤へ着手した時点で更新する。 |
| M11 | 未着手 | — |

## 現在の注記

- M0/M1 を再度開く blocker はない。M0 で把握した後続課題は [baseline.md](baseline.md#既知のギャップm0-完了時点で未解消後続-milestone-へ) に記録している。
- `example/assets`、`example/web/assets`、`plugin/flac/test/testdata/conformance` の3 gitlinkは、code と独立した任意取得の共有 data/asset として意図的に維持する。source code submodule 禁止には該当しない。
- repository 全体の scalar/SIMD/forced-scalar differential は必須 gate にしない。`tools/cmd/differential` は任意の診断 tool とし、semantic 差は対象 package の test で検証する。
- 日常開発と milestone/release の検証範囲は [quality.md](quality.md#開発時の検証-tier)、性能回帰の判定は [performance.md](performance.md#開発時の性能回帰方針) に従う。
- `config.SchemaView` は型消去 resolver を持ち、catalog 経由で `Patch` を resolve できる。CLI/WASM への投影と M3/M4 の component 契約拡張はこの経路へ接続する。
- `plugin/identity` の import path snapshot test は、公式 plugin へ marker identity を導入する M6/M8 で marker ベースの test へ置き換える。あわせて置き場所も見直す。M2 で `plugin/` 直下に foundation の `package plugin` が入ったため、現在は `plugin/flac` 等と並んで規格 family の一つに見える。[architecture.md](architecture.md#依存原則) が想定する最上位 `integration` module が適切な位置である。
- `config` の source は責務別 file へ分割したが、test は `config/schema_test.go` 一本のままである。M3 以降の作業と合わせて同じ境界で分ける。
- M3 着手前に [C17](decisions.md#c17-config-snapshot-は-codec-clone-だけで構成する) の「`C` の全 field を登録しないと schema 登録失敗」を実 config で検証する。FLAC encoder config か `plugin/audio` の equalizer/mixer を 1〜2 個 M2 schema へ写す prototype を test fixture として作り、移行はしない。可変長 band、port 数、variant 選択が規則に耐えるかをここで確かめる。規則が破綻するなら foundation を積み上げる前に直す。
- `bindings/wasm/bindings_gen.go` は `gowasm-bindgen` の出力で、gofmt 非準拠のまま checked in されている。format すると生成器の出力と乖離して drift 検出が誤発火するため、意図的に未 format で維持している。[supply.md](supply.md#generate) の generate phase が求める「生成物を format/typecheck/compile して checked-in state と比較する」形は、build recipe に gofmt を挟む M9/M10 で満たす。
- repository の line ending は `.gitattributes` の `* text=auto eol=lf` で固定した。これがないと Windows checkout が CRLF になり、`gofmt -l` が数百 file を誤検出して format を gate にできない。`.gitignore` と `LICENSE` は CRLF で commit されているため、次に `git add` した時点で LF へ正規化される。
