# M2 修正指示書: 完了条件を満たしていない箇所の是正

M2 の実装 review で見つかった欠陥を是正する作業指示である。前提とスコープ境界は [task/m2.md](m2.md) と同じで、`core`・`sdk`・`plugin/<family>`・surface には触れない。受け入れ基準は [plugins.md](../plugins.md#m2-完了条件) と [config.md](../config.md#m2-完了条件) の完了条件とする（今回の review を受けて条件を一部追記済み）。

review 時点の状態: `go build ./...`、`go vet`、対象 package の test と `-race`、`go run ./tools/cmd/test-runner --simd` はすべて green。依存方向も正しい。以下は回帰ではなく contract の欠陥である。

1〜7 は 1 回目の review、8〜10 はその是正後の 2 回目の review で見つかった項目である。1〜10 は修正済みで、対象 package の test・race、全体 build、全体 `--simd` runner、config benchmark を完了している。

## 必読

[task/m2.md](m2.md) の必読に加えて [findings.md](../findings.md) の F26、F28 を読む。

## 1. 未 Build の zero `config.Schema[C]` を invalid にする

`config/schema.go` の `Valid()` は `problems` が空なら true を返すため、`Schema[C]{}` が valid 扱いになる。この component は `plugin.NewComponent` から `host.New` まで通過し、config field が 0 個の component として catalog に載る。失敗するのは実行時の `Resolve` になる。

- `Schema` が Build を経ていないことを検出できるようにし、`Valid()`、`Err()`、`View().Valid()` が invalid を返すようにする。
- `plugin.Component.Diagnostics()` がその diagnostic を component identity 付きで集約し、`host.New` が失敗することを確認する。
- 再現 test を `config` と `host` の両方に追加する。host 側は「zero schema を持つ component を含む Set で `host.New` が error になる」ことを検査する。

## 2. generic reflection clone を廃止し codec `Clone` に一本化する

`config/clone.go` の struct 分岐は `result.Set(value)` の後に `CanSet()` が false の unexported field を skip するため、unexported な reference field を静かに shallow copy する。default factory が共有値を返す config では `Default()` も `Resolved.Value` も snapshot にならない。`config.SecretValue[T]` 自身が unexported field を持つため標準型が影響を受ける。これは [F26](../findings.md) が挙げる失敗モードそのものであり、[inventory.md](../inventory.md) は同種の reflection clone を削除対象としている。

確定した方針は [config.md](../config.md#immutability-と-canonicalization) に追記済みである。

- `config/clone.go` を削除し、`cloneTyped` への依存を取り除く。
- snapshot は「default factory が fresh な値を返す」ことと「登録 field ごとの codec `Clone`」だけで構成する。`Default()`、`Resolve`、`ResolveValue`、`Resolved.Value` のすべてがこの経路を通る。
- reference 型を扱う codec が `Clone` を宣言していない場合は schema 登録を失敗させる。標準 codec のうち `Slice`、`Map`、`Optional`、`Auto`、`Union`、`Secret` は既に `Clone` を持つ。`Nested` は `cloneTyped` を使っているので、nested schema の field codec を経由する形へ置き換える。
- `Map` codec は value だけでなく key も clone する。あわせて `Normalize` を実装し、`Slice` との非対称をなくす。
- test: 共有値を返す factory（package 変数や closure が捕捉した値）から `Default()` を 2 回取り、片方の変更が他方と factory source に波及しないことを検査する。exported field、unexported field、`SecretValue` の 3 経路を含める。

## 3. 未登録 field を schema 登録 error にする

canonical/fingerprint は登録 field だけを走査するため、未登録 field があると `Value` が違うのに fingerprint が同じ config を作れる。M4 の planner cache key に直結する。

- schema build 時に `C` の mutable な field がすべて登録されているかを検査し、未登録があれば component identity と field path 付きで失敗させる。検査は control plane の一度きりの処理なので reflection を使ってよい（[plugins.md](../plugins.md#reflection-の使用範囲) の許可範囲内）。
- 判定基準（どの型を mutable とみなすか、unexported field をどう扱うか）は実装者が決めてよいが、godoc に一行で根拠を残す。2 の「reference 型には `Clone` が要る」と同じ判定を共有できるなら共有する。
- test: 未登録の slice field を持つ config で schema 登録が失敗すること。

## 4. nested/slice/map の surface decode で unknown field を error にする

`config/field.go` の `Nested` は `json.Unmarshal` を使うため未知キーを黙って捨てる。`Slice`、`Map` の JSON decode も同じ経路である。

- `json.Decoder` の `DisallowUnknownFields` を使うか、nested schema の field ID を使った decode へ置き換える。
- test: 未知キーを含む nested/slice/map の surface 値が error になり、diagnostic に field path が付くこと。

## 5. schema identity と version を必須にする

canonical encoding に含まれるのに空でも通るため、識別力の弱い schema を作れてしまう。

- `Build()` で identity と version が空なら schema 登録を失敗させる。
- `plugin/example_test.go` の example は両方を設定した形へ直す。example は plugin 開発者が最初に読む形なので、省略できる印象を与えない。

## 6. preset provenance の扱いを決めて実装と一致させる

現在は default と preset 適用後の canonical を field ごとに比較して `SourcePreset` を決めているため、preset が default と同じ値を代入した field は `default` と記録される。`Preset(id, apply func(*C))` では触れた field を追えないための妥協である。

- 値の差分検出を維持する場合は、`Preset` の godoc と [config.md](../config.md) の provenance の項に制約として明記する。
- 触れた field を正確に記録する形（preset が `Patch` を返す等）へ API を変える場合は、[decisions.md](../decisions.md) の更新規則に従い product 判断として確認を取ってから実装する。**独断で API を変えない。**

## 7. 小さな是正

- `config/field.go` の `parseInt`/`parseUint` が 64bit で parse した値を `T` へ無検査変換している。narrow な `T` と 32bit target で silent truncation になるため範囲検査を入れる。
- `plugin/set.go` の `Add` は error 時に空 `Set` ではなく receiver を返す。
- `internal/catalog` の `cloneComponents` は名前どおり clone するか、shallow copy であることが明確な名前にする。同 loop の `byID[identity] = len(byID)` は読まれない値を入れているので、重複検出の意図が読める形にする。
- `diagnostic` の `Info`/`Warning`/`ErrorSeverity` は命名が非対称なので揃える。`Error` 型と衝突しない一貫した名前にする。
- test の dead code を削除する（AGENTS.md「途中で必要なくなったコードは削除する」）。
  - `config/schema_test.go` の `testSchema(reverse bool)` は `reverse` を使っておらず両分岐が同一である。使うか、引数を削除する。
  - 同ファイルの `base := testSchema(false)` と続く空 loop。
  - `internal/catalog/catalog_test.go` の重複した `diagnostic.ItemsOf` アサート。
- canonical encoding の golden digest test を追加する。fingerprint が process 再起動に依存しないことと、canonical format の意図しない変更の両方を固定できる。

## 8. 構造化 codec の surface 表現を decode と対称にする

1 回目の是正後の review で見つかった欠陥である。`Schema.encodeJSON` は各 field の human-readable な `Encode` 出力を JSON document へ埋め込むが、`Slice`/`Map` の `Encode` は JSON ではない（`[a,b]`）。`json.Valid` に落ちて文字列として quote されるため、`Nested` の encode 結果を同じ codec の `decodeJSON` が復元できない。

```text
nested encode = {"level":3,"tags":"[a,b]"}
decode        = slice must be a JSON array
```

- `Nested` の encode と decode は同じ「JSON object」表現を名乗っているので、逆関数にする。
- そのために、構造化 codec（`Slice`、`Map`、`Nested`、`Union`）の surface 表現を decode が受け付ける syntax に揃える。人間向けの短い表示が別途必要なら、`Encode` とは別の表示専用 API として分ける。両者を一つの `Encode` に混ぜない。
- test: `Nested`、`Slice`、`Map`、`Union` それぞれで `Decode(Encode(v))` が元の値と一致すること。nested の中に slice と map を含む形を必ず含める。

## 9. 未登録 field の検査を全 field へ広げる

8 と同じ review で見つかった。3 の実装は mutable field だけを検査するため、未登録の scalar field が残る。`ResolveValue` に渡した値の差が `Resolved.Value` には現れるのに canonical には入らず、異なる config が同じ fingerprint を持つ。

```text
ResolveValue{Level:1, Forgotten:1} と {Level:1, Forgotten:999} が同じ fingerprint
```

これは 3 の指示（および [config.md](../config.md#immutability-と-canonicalization)）が検査対象を「mutable field」と限定していたことによる。文書は全 field を対象とする形へ訂正済みである。

- `C` の top-level field はすべて登録を必須にし、未登録があれば component identity と field path 付きで schema 登録を失敗させる。
- mutable 判定は codec の `Clone` 必須判定にだけ使い、登録必須判定とは分ける。
- test: 未登録の scalar field を持つ schema が登録に失敗すること。

## 10. 小さな是正

- `Builder.Preset` の godoc に provenance の制約を書く。6 で [config.md](../config.md) には記載したが godoc が未反映で、plugin 開発者は godoc を先に読む。
- `Set.Override`/`OverridePlugin` も error 時に空 `Set` ではなく receiver を返す。7 で `Add` だけ直したため非対称になっている。
- `Schema.decodeJSON`/`encodeJSON` を `config/field.go` から `config/schema.go` へ移す。`Schema` の責務であり、field codec 定義の file に置く理由がない。

## 検証

- 単位ごと: 対象 package の test と `-race`。
- 全体: `go build ./...` と `go run ./tools/cmd/test-runner --simd`。今回の変更は `config` の public API を変えるが、利用側は新 foundation package と自身の test だけなので旧経路への影響はないはずである。影響が出た場合は原因を報告する。
- benchmark: 2 で snapshot 経路が変わるため `config` の既存 benchmark を再取得し、[performance.md](../performance.md#開発時の性能回帰方針) の 2 倍目安で明白な回帰がないことだけ確認する。小さな差は gate にしない。

## 中断して確認する条件

[task/m2.md](m2.md#中断して確認する条件) と同じ。特に 6 は product 判断を含むため、API を変える方向に倒す場合は必ず確認を取る。

## 完了時の記録

1. [plugins.md](../plugins.md#m2-完了条件) と [config.md](../config.md#m2-完了条件) の完了条件を逐条で確認する。
2. `go run ./tools/cmd/test-runner --simd` を実行する。
3. [checkpoint.md](../checkpoint.md) の M2 行を更新する。あわせて M3 以降への申し送りを注記へ書く。現時点で分かっている申し送りは次の 2 件である。
   - `config.SchemaView` は description しか持たないため、catalog 経由で `Patch` を resolve する経路がない。CLI/WASM への投影が必要になる M3/M4 で component 契約と一緒に設計する。
   - `plugin/identity` の import path snapshot test は、公式 plugin へ marker identity を導入する M6/M8 で marker ベースの test へ置き換える。
