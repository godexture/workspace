# M2 修正指示書: 完了条件を満たしていない箇所の是正

M2 の実装 review で見つかった欠陥を是正する作業指示である。前提とスコープ境界は [task/m2.md](m2.md) と同じで、`core`・`sdk`・`plugin/<family>`・surface には触れない。受け入れ基準は [plugins.md](../plugins.md#m2-完了条件) と [config.md](../config.md#m2-完了条件) の完了条件とする（今回の review を受けて条件を一部追記済み）。

review 時点の状態: `go build ./...`、`go vet`、対象 package の test と `-race`、`go run ./tools/cmd/test-runner --simd` はすべて green。依存方向も正しい。以下は回帰ではなく contract の欠陥である。

1〜7 は 1 回目、8〜10 は 2 回目、11〜12 は 3 回目、13〜17 は 4 回目の review で見つかった項目で、いずれも修正済みである。18 は 5 回目の review で見つかった仕上げ項目であり、今回の修正と検証を完了した。

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

## 11. secret を surface 表現から外し、redaction marker の decode を error にする

8 の是正後の review で見つかった。`Nested.Encode` が wire 表現になったことで、secret field が `<redacted>` という marker として wire に載り、同じ codec の decode がそれを**そのまま値として受け取る**。

```text
Encode(live)  -> {"endpoint":"s3://bucket","token":"<redacted>"}
Decode(above) -> err=nil, token="<redacted>"
```

nested に限らず、`Patch.SetText("token", "<redacted>")` も同じく素通りする。CLI 表示や保存 graph を読み戻して再実行すると、本物の credential が marker に置き換わったまま remote へ送られ、手元には何の diagnostic も出ない。

確定した方針は [config.md](../config.md#immutability-と-canonicalization) に追記済みで、次の二つを**両方**実装する。

- 構造化 codec の surface encode（`Schema` の wire encode と、それを使う `Nested` 等）は `Secret` な field を出力しない。decode 側は欠けた secret field を「未指定」として扱い、default を使う。error にはしない。
- `SecretCodec` の decode が redaction marker を受け取ったら error にする。code は `config.secret-redacted` 等の安定した識別子とし、message と diagnostic に marker 以外の値を含めない。
- 人間向けの `Codec.Encode` は `<redacted>` を返してよい。表示専用であり wire 表現ではないことを godoc に書く。
- test: secret を含む nested config の encode 出力に secret が現れないこと、その出力を decode すると secret 以外が復元され secret は default になること、marker を明示的に decode させると error になること、error と diagnostic に本物の secret も marker 由来の値も現れないこと。

## 12. blank/zero-size field を未登録検査から除く

9 の全 field 検査は blank field と zero-size field も対象にするため、次のような config 型は登録できる accessor が存在せず、schema を永久に構築できない。

```text
type markerConf struct {
    Level int
    _     struct{}
}
-> _: error: config.unregistered-field
```

blank field と zero-size field は config の意味を運べないため、検査から除く。test に blank field を持つ config 型を含める。

## 13. `Set` の error を保持し `host.New` で集約する

11・12 の是正後、実装全体を文書の正本と照らして見直した結果である。[plugins.md](../plugins.md) は composition を次の形で書いており、重複は **host 構築時**に error とすると明記している。

```go
set := plugin.NewSet(wave.Plugin, pcm.Plugin, audio.Plugin)     // plugins.md
set := standard.Set().Add(acme.Plugin).Override(acme.FastFLAC)  // plugins.md
set := plugin.NewSet(mp3.Plugin(), flac.Plugin())               // architecture.md
```

現在の `NewSet`/`Add`/`Override`/`OverridePlugin` は `(Set, error)` を返すため、この形で書けない。さらに error を握りつぶすと壊れた composition が痕跡なく消える。

```text
set, _ := plugin.NewSet(first)
set, _  = set.Add(duplicate)
host.New(host.Plugins(set)) -> err=nil, components=1
```

これは [F28](../findings.md) の「未導入と壊れた plugin を区別できない」と同じ失敗であり、schema builder・`Component`・`Definition` がすべて採っている「問題を保持して `host.New` が集約する」方針とも逆行している。

- `Set` が composition 時の問題（重複 identity、無効な override target、identity 不一致の置換、存在しない `Remove` 対象）を diagnostic として保持する。
- `NewSet`、`Add`、`Remove`、`Override`、`OverridePlugin` は `Set` だけを返し、chain して書ける形にする。
- `host.New` と `catalog.Build` がその問題を他の diagnostic と一緒に集約する。
- 保持した問題は失われない。`Set` を further compose しても消えないこと、`Definitions()`/`Components()` の内容と矛盾しないことを保証する。
- test: 重複を加えた `Set` で `host.New` が失敗し、diagnostic に重複した identity が現れること。chain した composition が期待どおりの catalog を作ること。

## 14. `Component` が型消去した config resolver を保持する

`NewComponent[Marker, C]` は `C` を知っているのに、保持しているのは `Schema.View()`（`Valid`/`Diagnostics`/`Description`）だけで、`Patch` を解決する経路がない。M4 の planner は component identity で選んだ component に利用者の `Patch` を渡して `Resolved[C]` を作り `Compile` へ渡す必要があるため、このままでは M3/M4 で `Component` の契約を作り直すことになる。完了条件の「型消去した config schema を保持する」も description だけでは満たしきれない。

- `NewComponent` が `C` を確定している時点で型消去した resolver を capture する。`Patch` を受け取り、解決済み config と diagnostic を返せるようにする。解決結果の具体型を公開する必要はない。
- `config.SchemaView` を interface から `config.Schema[C]` だけが作れる具体型へ変える。第三者が `Valid() == true` を騙る実装を差し込めないようにし、「invalid definition は host 構築で失敗する」不変条件を型で閉じる。
- `Compile`、port、`Open` に関わる型はこの milestone では追加しない。追加するのは config 解決の経路だけである。
- test: catalog から取り出した component に `Patch` を渡して解決でき、unknown field や範囲外の値が field path 付き diagnostic になること。

## 15. component descriptor の重複を減らす

現在は component ごとに `DisplayName` と `Version` が必須で、family 単位では同じ version を 4〜5 回書くことになる。[experience.md](../experience.md) の目標像は marker、config、Processor だけであり、complexity budget の「plugin 開発者: marker type 一つ」から離れている。

- descriptor の必須は plugin 単位だけにする。
- `Define` が component descriptor の未設定 field を親 plugin の descriptor から引き継ぐ。
- component の `DisplayName` が未設定なら marker 型名から導出してよい。導出規則を godoc に書く。
- test: component descriptor を省いた definition が host 構築を通り、catalog view に親から引き継いだ値が現れること。

## 16. `config` package の file を責務で分割する

`config/schema.go` が 1223 行、`config/field.go` が 1058 行ある。さらに `Field()` は `schema.go` にあり標準 codec は `field.go` にあるという逆転が起きている。AGENTS.md の「ファイル・ディレクトリは責務によって構造的に分割する」に反する。

責務ごとに分ける。file 名は実装者が決めてよいが、`Field` と標準 codec の逆転は必ず解消する。目安は次のとおり。

- `field.go`: `FieldSpec`、`Field`、`FieldOption`
- `codec.go`: `Codec`、`CodecSpec`、codec 共通の builder
- 標準 codec: scalar、単位付き、sum type、collection、secret を別 file に分ける
- `patch.go`: `Patch`
- `resolved.go`: `Resolved`、`Provenance`、`Fingerprint`
- `describe.go`: `Description`、`SchemaDescription`、型消去した schema view
- `wire.go`: surface JSON の encode/decode
- `validate.go`: schema 自己検証と未登録 field 検査

分割は移動だけとし、同じ commit で挙動を変えない。

## 17. 残りの小さな是正

- `plugin.Component{}.View()` が nil の schema view を参照して panic する。`Diagnostics()` は nil を守っているので同じ扱いにする。zero value の public API が panic しないことを test で固定する。
- redaction marker の diagnostic は code が `config.secret-redacted` なのに message が汎用の「field input could not be decoded」のままで理由が読めない。marker 由来であることが分かる message にする。値は含めない。
- `plugin.Descriptor` の `Repository` が `Provenance.Repository` と重複している。どちらか一方にする。
- `plugin.Descriptor` の `PureGo`/`CGO`/`Native` は 3 つの独立した bool のため矛盾した状態を表現できる。排他的な enum にする。
- `host.Catalog` に catalog fingerprint を持たせる。[surfaces.md](../surfaces.md#catalog) と [web.md](../web.md#open-catalog) が要求しており、host composition と component/schema version から作る catalog の責務である。M9 まで待つ理由がない。

## 18. 仕上げ

13〜17 の是正後の review で見つかった。どちらも小さいが、放置すると catalog surface と public API に残る。

### DisplayName を継承対象から外す

`bindComponent` は `Descriptor.inherit` で `DisplayName` も親から埋めるため、`View()` にある marker 名の fallback は bound な component では到達しない（親の `DisplayName` が空なら plugin 側の descriptor 検証が先に失敗するため）。結果として family の全 component が同じ表示名になる。FLAC family なら codec、format、parser がすべて "FLAC" と表示される。

`DisplayName` は descriptor の中で唯一 component ごとに異なるべき field である。version、license、homepage、repository、build、digest、signature、provenance の継承はそのままでよい。

- `inherit` から `DisplayName` を外す。
- 未設定の component は marker 名を表示に使う（既存の fallback をそのまま活かす）。
- `plugin/foundation_test.go` の `TestDefinitionInheritsPluginDescriptor` は `got.Descriptor() == parent` で現在の挙動を固定しているので、`DisplayName` だけ別に検査する形へ直す。
- test: 同じ plugin の 2 component が異なる表示名を持つこと。

### `Set.ValidateDuplicates` を削除する

`Diagnostics()` の内容を含んだうえで重複走査を足す形になっており、名前と実態がずれている。doc の「future adapters に有用」も投機的で、AGENTS.md の「必要なくなった export は残さない」から外れる。`catalog.Build` は既に component の重複走査を自前で持っている。

- `catalog.Build` が `set.Diagnostics()` を取り込み、自前の走査へ plugin identity の重複検査を足す。
- `Set.ValidateDuplicates` を削除する。
- test: 既存の重複検査 test が catalog 経由で同じ diagnostic を得ること。

`config/schema_test.go` が 677 行のまま source の分割に追随していない点は、M3 以降の作業と合わせて整理する。

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
   - `config.SchemaView` は型消去 resolver を持ち、catalog 経由で `Patch` を resolve できる状態にした。CLI/WASM への投影と M3/M4 の component 契約拡張はこの経路へ接続する。
   - `plugin/identity` の import path snapshot test は、公式 plugin へ marker identity を導入する M6/M8 で marker ベースの test へ置き換える。
