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
| M3 | 未着手 | 次の主対象。固有の完了条件を [media.md](media.md#m3-完了条件)、[access.md](access.md#m3-完了条件)、[scope.md](scope.md#m3-完了条件) に定義した。着手前の [C17](decisions.md#c17-config-snapshot-は-codec-clone-だけで構成する) 実 config 検証は完了した。着手時に実装単位を記録する。 |
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
- [C17](decisions.md#c17-config-snapshot-は-codec-clone-だけで構成する) を実 config で検証し、規則を維持すると決めた。FLAC encoder config は 18 field 中 17 を登録でき、`Apodizations []func([]float64)` だけが canonical 表現を持てず登録に失敗する。C17 が過剰なのではなく、closure が parameter を捕捉して `Tukey(0.5)` と `Tukey(0.9)` を区別できなくしている実装漏れを検出したものである。data 表現へ移す作業は [capability.md](capability.md#挙動変更の記録) の B4 として M8 が担当する。prototype は捨て、一般の contract test として `config/function_test.go` に残した。
- C17 検証で分かった M3/M4 への申し送り。`plugin/audio` の `MixerConfig` は空 struct で、port 数は config になく実行時の入力数から導出している。[config.md](config.md#動的-field-と-topology) が求める「安定した config 型に count または typed port definition を持たせ、解決済み config を `Shape` が読む」は移行ではなく新規設計であり、`Shape` を設計する M3/M4 で port 数の出所を決める。equalizer の可変長 band は現在 comma 区切り string で、C17 は通るが型付き slice への移行は M8 が担当する。
- 明示 schema の登録コストは実測で 1 field あたり 3 行前後（FLAC encoder は struct 23 行に対し登録 154 行）。ただし現行は help/range/単位が `config_options.go` と struct tag に分散しており、それらを含めた総量では schema 化で減る見込みである。[config.md](config.md#public-model) が M8 へ割り当てた tag adaptor は、scalar のみの config には効くが equalizer のような conditional/可変長を含む config には明示 schema が必要、という前提で判断する。
- `bindings/wasm/bindings_gen.go` は `gowasm-bindgen` の出力で、gofmt 非準拠のまま checked in されている。format すると生成器の出力と乖離して drift 検出が誤発火するため、意図的に未 format で維持している。[supply.md](supply.md#generate) の generate phase が求める「生成物を format/typecheck/compile して checked-in state と比較する」形は、build recipe に gofmt を挟む M9/M10 で満たす。
- repository の line ending は `.gitattributes` の `* text=auto eol=lf` で固定した。これがないと Windows checkout が CRLF になり、`gofmt -l` が数百 file を誤検出して format を gate にできない。`.gitignore` と `LICENSE` は CRLF で commit されているため、次に `git add` した時点で LF へ正規化される。
