# M0〜M6 実装監査

> 監査日: 2026-08-13  
> 対象 commit: `0780b145e6f755c447d3a288dd98516da7f9df61`  
> 対象 branch: `m6-ownership-cell`  
> toolchain: `go1.26.4 windows/amd64`

## 結論

M0〜M6 の実装は、module 境界、typed media path、planner/runtime、file transaction、WAVE/PCM の実経路、第三者相当 plugin、CLI までを一つの Host 経路へ接続しており、基礎設計の方向はよい。既存の full test、race test、vet、generator、文書検査もすべて成功した。

一方、公開 API だけで再現できる contract 違反が残っている。特に secret の表示、ownership、borrow/COW、WAVE raw preservation は、通常 test が green でも利用者データまたは process safety を損なう。現在の [checkpoint](checkpoint.md#進捗) にある「M0〜M6 完了」は、そのままでは支持できない。

監査結果は次のとおりである。

| 優先度 | 件数 | 判定 |
|---|---:|---|
| P0 | 0 | 即時の repository-wide stop に相当する問題は確認しなかった |
| P1 | 7 | M7 着手前に contract と回帰 test を直す |
| P2 | 4 | M6 再完了または直後の hardening 単位で直す |
| P3 | 1 | M7 で責務が増える前に分割する |

推奨する進捗上の扱いは、M0/M1 の完了を維持し、M2〜M6 の過去成果を消すのではなく、M6 を `進行中（M0〜M6 review remediation）` へ戻して本書の P1 を閉じることである。P2 も、明示した担当 milestone と実 consumer が無いものは M6 再完了までに閉じる。

## 監査範囲

次を対象にした。

- [refactor.md](../refactor.md)、[checkpoint.md](checkpoint.md)、`docs/refactor/` の領域別 contract、M2〜M6 task 文書
- `_legacy/` を除く production source、公開 API、official plugin、`standard`、CLI、public testkit、integration module
- config resolution、plugin Shape/Compile/Suggest/Open、planner、runtime ownership、fan-out/COW、Access session、spool、file transaction、WAVE inspect/demux/mux/metadata、surface rendering
- unit/integration/race test、coverage の薄い箇所、完了条件と test assertion の対応

`_legacy/` は AGENTS.md の規則どおり、移植参照であって現行実装ではないため監査対象から除外した。

## 検証結果

次の gate は成功した。

```text
go run ./tools/cmd/test-runner --simd
go run ./tools/cmd/generate
go run ./tools/cmd/docs-check
go vet ./...
go test -race ./...

cd integration
go vet ./...
go test -race ./...

cd tools
GOWORK=off go build ./...
```

追加で Windows file transaction の既存 target 置換と permission 保持 test、CLI help、root/integration の coverage を確認した。Go 1.26.4 の Windows `os.Rename` は `MOVEFILE_REPLACE_EXISTING` を使うため、[access.md](access.md#m6-完了条件) の既存 target 置換方針に問題は確認しなかった。

coverage は合否条件にはしていないが、未検査 contract の探索に使った。代表値は root の `config` 70.0%、`flow` 61.7%、`testkit` 46.9%、`internal/bind` 2.1%、integration から root 全体を含めた値が 63.1% だった。後述する問題の多くは、低い行 coverage そのものではなく、assertion が contract を検査していないことに起因する。

## milestone 判定

| Milestone | 判定 | 根拠 |
|---|---|---|
| M0 | 完了維持 | baseline commit、manifest、再現条件、意味上の比較軸が残っている。今回の現行実装の欠陥は baseline artifact を無効にしない |
| M1 | 完了維持 | root/integration/tools の module DAG、tracked workspace、generator bootstrap、`GOWORK=off` の tools build が成立している |
| M2 | 要是正 | R-01、R-05、R-06、R-07 により secret、normalization、immutable config、panic-free construction の contract が未成立 |
| M3 | 要是正 | R-03、R-08、R-10 により borrowed media、allocator bounds、Access snapshot/StableSize の contract が未成立 |
| M4 | 要是正 | R-06 と R-09 により pure Shape/Compile と bounded Suggest の検証が不十分 |
| M5 | 要是正 | R-02、R-03 により exact-once ownership、panic cleanup、COW 強制が未成立 |
| M6 | 要是正 | R-04、R-09、R-10、R-11 により WAVE preservation、public testkit、file snapshot、CLI failure UX の完了条件が未成立 |

## Findings

### R-01 [P1] secret が標準 formatting から漏れる

**状態: 解決済み（2026-08-13）**

`SecretValue`、`Patch`、`Resolved`、`ResolvedView` に全 verb を安全に処理する formatter を追加した。formatter method が呼ばれない named type や unexported outer field でも raw value を反射表示できない opaque storage とし、struct/pointer/slice/map/interface、typed/type-erased resolved value、主要な verb/flag/width/precision の回帰 test を追加した。`Patch` の表示は schema 解決前であるため preset、field ID、source のみを残し、typed/text value は一律非表示にした。secret contract を検査する test failure 自身も raw value を出力しない。

**根拠**

- [`SecretValue.String`](../../config/secret.go)（24 行目）は通常の `%v`/`%s` だけを redaction する。`fmt.Formatter` または `%#v` 向けの保護が無い。
- [`Patch.SetText`](../../config/patch.go)（39〜42 行目）は raw text を保持するが、`Patch` 自身に安全な表示 contract が無い。
- [`secret_test.go`](../../config/secret_test.go) は `fmt.Sprint` と `Resolved.String` を検査するだけで、`%+v`、`%#v`、container 内の表示、`Patch` の表示を検査していない。失敗 message 自身にも `%#v` がある。
- 実測では `fmt.Printf("%#v", config.NewSecret("review-secret"))` が `config.SecretValue[string]{value:"review-secret"}` を、`fmt.Sprint(config.NewPatch().SetText("token", "patch-secret"))` が `patch-secret` を表示した。

**影響**

debug log、test failure、panic report、observability adapter が一般的な `%#v` を使うだけで credential が漏れる。`Resolved.String` が安全でも、`Resolved`、`ResolvedView`、config struct、`Patch` を再帰表示すれば回避できない。これは [config.md](config.md) の「raw secret を public representation、diagnostic、error、log に出さない」という contract に反する。

**必要な是正**

- `SecretValue` の全 formatting verb を redaction する。outer struct、pointer、slice、map、interface に入れた場合も test する。
- schema 未解決の `Patch` はどの field が secret か判断できないため、表示時は値を一律に隠し、preset、field ID、source だけを示す。
- secret を含み得る diagnostic detail、panic recovery、test failure で raw `%v`/`%#v` を使わない。

### R-02 [P1] ownership token の複製と panic が exact-once release を破る

R-02 には独立した二つの failure mode がある。

#### R-02a `Owned[T]` を複製して double drop できる

[`Item.Consume`](../../flow/item.go)（133〜141 行目）と `Item.Adopt`（144〜155 行目）は、公開された copyable な `Owned[T]`（188〜205 行目）を受け渡す。`Release` は value receiver であり token を無効化しないため、コピーまたは adopt 後の元 token から何度でも release できる。

次の公開 API 経路は drop を 2 回呼ぶ。

```go
item := flow.NewItem(value, typ)
owned := item.Consume()
var adopted flow.Item[T]
adopted.Adopt(owned)
owned.Release()
adopted.Drop()
```

実測でも drop count は `2` になった。公式 `buffer.Handle` の release が lease 単位で idempotent でも、第三者 schema の `Drop` にその要件は無い。

これは [checkpoint.md](checkpoint.md#現在の注記) の「`Input`/`Owned`/`Shared` が消え、`noCopy` で所有権複製を検出する」という記録とも一致しない。`Item` の `noCopy` を、public `Owned` で迂回できる。

#### R-02b `Transfer` の build panic で payload が leak する

[`flow.Transfer`](../../flow/item.go)（171〜185 行目）は source を `clear` してから任意の `build` callback を呼ぶ。error return では元 payload を drop するが、panic では drop しない。caller の deferred `source.Drop()` は空 cell に対して何もしない。

これは [runtime.md](runtime.md#ownership) の「panic 巻き戻し中も deferred Drop が走り、runtime cleanup は不要」という根拠を直接破る。公式 WAVE/linear component も `Transfer` を使うが、既存 test は成功/error/panic の release count を網羅していない。

**必要な是正**

- transport 用 token を component-facing `flow` surface から隠し、private runtime/queue 側へ置くことを第一候補にする。
- public storage primitive が必要なら、copy しても一つの atomic state を共有する single-consume token、または pointer-only/noCopy API とし、adopt が token を確実に空にする。
- `Transfer` は build 完了前の unwind を `defer` で検出し、error と panic の両方で元 payload を一度だけ drop する。panic は同じ値で再送出する。
- success、returned error、panic、target overwrite、copied token、repeated release を exact count で test する。

### R-03 [P1] borrowed read-only view が mutable slice を返し COW を迂回できる

**状態: 解決済み（2026-08-13）**

byte payload は private backing の immutable `buffer.Bytes`、typed plane は immutable `audio.Samples[S]` を返す API へ一括移行した。両 view は length/index、copy/append と必要な read/compare 操作だけを提供し、borrowed/read-only/shared backing の `[]byte` / `[]T` を返さない。view/reader は raw slice header を保持せず originating lease と範囲を操作時に検証するため、owner 解放後に backing を読み続けたり allocator grant を暗黙に延長したりできない。mutable slice は `buffer.Edit`、`audio.Editor`、`buffer.Mutable` / `WriteLease` の明示 writer path に限定し、`Allocator.FromBytes` も `WriteLease` で初期化する。fan-out sibling isolation、owner 解放後の view expiration と別 `Share` owner の独立 lifetime、immutable copy の独立性、exclusive editor の backing reuse と zero allocation を回帰 test で固定した。payload size に比例する読み取りは `buffer.Bytes.Blocks` が唯一の実装であり、official PCM hot loop と file sink はそれを計上済み scratch で呼ぶだけで allocation を持たない。理論下限の direct slice reference との同一 process paired median は decode `1.54`、encode `1.77` で 2 倍 trigger 未満、R-03 以前の `binary.ByteOrder` loop 比では decode/encode とも速い。file sink の scratch drain は payload-size 比例の copy を 1 pass 増やすが、比例する allocation は持たない。

**根拠**

- [`buffer.View`](../../media/buffer/buffer.go)（74〜77 行目）は read-only borrow と説明されるが、`View.Bytes`（422〜426 行目）と `View.Plane`（429〜439 行目）は backing storage そのものの `[]byte` を返す。
- owned `Handle.Bytes`/`Plane` も同じ slice を返し、`ReadOnly`/`Shared` を検査するのは `MutableBytes` だけである。
- [`audio.Frame.Plane`](../../media/audio/audio.go) と `PlaneSamples`（75〜90 行目）、[`packet.Chunk.Bytes`/`Packet.Bytes`](../../media/packet/packet.go)（35〜40、76〜84 行目）も mutable slice をそのまま公開する。

**影響**

fan-out 後の第三者 component は `Edit`/`MutableBytes` を呼ばず、borrowed slice へ代入するだけで他 branch の payload を変更できる。`ReadOnly`、`Shared`、copy-on-write、branch isolation は contract ではなく慣習になる。誤った plugin 一つが、別 plugin の入力、fingerprint 前提、出力を黙って壊す。

**必要な是正**

Go の `[]byte`/`[]T` に read-only 型は無いため、これは method 名の変更だけでは直らない。M7 前に次の設計判断が必要である。

- public borrow を `Len`/`At`/`CopyTo`、read-only iterator、byte なら immutable string view 等へ変更し、mutable slice は `Edit`/`Editor` だけから返す。
- hot path に必要な zero-copy bulk read は、private runtime/official implementation 専用の trusted path と public safe path を分けるか、read-only view abstraction の benchmark を取って決める。
- fan-out branch から通常の `Bytes`/`PlaneSamples` を通じて書換えを試みても sibling が変化しない test を public testkit に置く。

### R-04 [P1] 合法な WAVE `JUNK` payload を無条件に捨てる

**根拠**

- [`inspect.go`](../../plugin/wave/inspect.go)（116〜121 行目）は RIFF 先頭の reservation offset にあり size が 28 byte の `JUNK` を、payload を見ずに structural と判定して preservation 対象から外す。
- [`mux_header.go`](../../plugin/wave/mux_header.go)（45〜75 行目）は同じ位置へ zero-filled `JUNK` を生成する。
- RIFF/WAVE 上、同じ位置と size の non-zero `JUNK` も合法であり、入力由来の byte である。現在の roundtrip はそれを zero に変える。
- [`metadata_test.go`](../../plugin/wave/metadata_test.go)（296〜335 行目）は zero payload のみで誤った heuristic を完了条件として固定している。
- [`integration/media_e2e_test.go`](../../integration/media_e2e_test.go)（543〜553 行目）は全 `JUNK` を preserved chunk の比較対象から除外するため、この破壊を検出しない。

**影響**

[capability.md](capability.md) と [media.md](media.md) が要求する input-derived unknown chunk/padding の byte-exact preservation に反し、正常終了した変換が利用者データを黙って変更する。

**必要な是正**

- 入力の reservation-slot `JUNK` も raw bytes として保持する。
- RIFF のまま出力する場合は、その raw chunk を reservation として再利用する。RF64 化で `ds64` に置換せざるを得ない場合は、loss を明示するか preserved chunk を別位置へ維持する方針を contract に固定する。
- non-zero payload、odd padding、先頭/途中/末尾、再 roundtrip、RIFF→RF64 の vector を unit と integration の両方へ追加する。
- `preservedChunks` は `JUNK` 全体を除外せず、writer 自身が追加した structural reservation と入力由来 chunk を区別する。

### R-05 [P1] config codec の合成で normalization が消える

**根拠**

- [`Nested`](../../config/collection.go)（208〜230 行目）は nested schema の Decode/Encode/Canonical/Clone/Validate を委譲するが Normalize を持たない。
- [`UnionCodec`](../../config/sum.go)（307〜397 行目）は選択 variant の Clone/Validate を委譲するが Normalize を持たない。
- [`SecretCodec.Normalize`](../../config/secret.go)（57〜60 行目）は inner normalization を実行するが、返された diagnostic をすべて捨てる。
- [`Schema.finish`](../../config/schema.go)（266〜314 行目）は top-level field codec の normalization だけを呼ぶため、上記 combinator の内側へ処理は届かない。

**影響**

単独 codec では正しい値が、nested/union/secret に合成した時だけ未正規化になる。error severity の normalization diagnostic が消える場合は、無効な値が有効な fingerprint を得る。値、provenance、diagnostic、canonical identity が codec の組み方で変わり、第三者 codec の予測可能性を損なう。

**必要な是正**

- Nested は nested schema の field-order normalization を再利用する。
- Union は選択 variant の Normalize を委譲し、diagnostic path を `value` 以下へ付ける。
- Secret は raw value/message/detail を出さず、severity と安全な path/code を保った redacted diagnostic へ写す。
- List/Map/Optional/Auto/Nested/Union/Secret を多段合成した table test で、value、provenance、diagnostic、fingerprint を検査する。

### R-06 [P1] immutable request/config が shallow copy で、fingerprint 後に意味が変わる

**根拠**

- [`Patch.Set`](../../config/patch.go)（29〜34 行目）は `any` の slice、map、pointer をそのまま保持し、`Patch.Clone`（68〜76 行目）は map entry だけを copy する。
- [`job.NewNode`](../../job/graph.go)（24〜31 行目）、[`NewAdaptor`/`NewEndpoint`](../../job/choice.go)（25〜34、68〜77 行目）は Patch をそのまま保持・返す。`FormatSelector.WithConfig` が `Patch.Clone` を使っても、clone 自体が shallow なので解決しない。
- [`Enum`](../../config/sum.go)（19〜68 行目）は caller の variadic backing slice を closure に保持し、constructor 後の caller mutation で Decode/Encode/Canonical/Validate が変わる。
- [`Resolved`](../../config/resolved.go)（94〜102 行目）は immutable と説明される一方、exported `Value C` を持つ。[`ResolvedView`](../../config/describe.go)（84〜93 行目）も exported `Value any` を持つ。
- [`Schema.View`](../../config/describe.go)（170〜210 行目）は snapshot 済み value を Shape/Compile 境界ごとに clone せず渡す。Shape callback が nested slice/map を変更すると、同じ fingerprint のまま後続 Compile が別の値を見る。

**影響**

constructor 後の caller mutation で Job の意味が変わり、[M6-5](task/m6-5.md#format-selector-は-job-boundary-の-immutable-request-である) の明示要件を満たさない。plugin callback の偶発的 mutation は pure Compile、plan cache、execution identity を壊す。fingerprint が値の identity を表さなくなるため、単なる API ergonomics ではない。

**必要な是正**

- schema を知らない `Patch.Set(string, any)` では正しい deep clone ができない。pre-v1 の今、schema-bound typed field key/builder、codec が clone する setter、または immutable text patch へ API を組み直す。
- Job constructor と getter の両方で同じ snapshot rule を適用する。
- `ResolvedView.Value` を private にし、phase ごとに schema codec で fresh snapshot を渡す。Shape の後に Compile が同じ canonical value/fingerprint を受けたことを検査する。
- constructor 入力、getter 戻り値、Shape callback 内の slice/map を変更する adversarial test を追加する。

### R-07 [P1] config の第三者 callback panic が Host/process 境界を越える

**根拠**

- [`Field`](../../config/field.go)（58〜60 行目）は invalid codec/accessor でも panic しないと説明され、[`Builder.Build`](../../config/schema.go)（110〜112 行目）も validation/canonicalization を panic 無しで行うと説明される。
- しかし accessor と `Codec.Clone` は `field.go`（117〜140 行目）、Normalize/Validate/Canonical/Decode/Encode は [`codec.go`](../../config/codec.go)（85〜130 行目）、schema validator は `schema.go`（335〜345 行目）から recovery 無しで呼ばれる。
- default factory と preset apply には局所的な recovery があるため、callback 境界の方針も一貫していない。

**影響**

第三者 plugin の config accessor/codec/validator の bug が、composition diagnostic または Resolve error にならず、package initialization、Host construction、planning process 全体を panic させる。component の Shape/Compile/Suggest/Open では panic boundary を用意しているのに、その前段の config だけが無防備である。

**必要な是正**

- config 内の全 user callback を共通 helper から呼び、construction/resolve phase、schema/field path、安定した code を持つ diagnostic に変換する。
- recovered value は secret を含み得るため、その文字列表現を diagnostic detail に含めない。
- default、preset、accessor、Clone、Normalize、Validate、Canonical、Decode、Encode、schema validator の panic matrix を test する。

### R-08 [P2] buffer layout の整数 overflow が小さな allocation として通る

**根拠**

[`layoutOf`](../../media/buffer/buffer.go)（172〜200 行目）は `plane.Size + plane.Padding` と alignment 加算を checked arithmetic より先に `int` で行い、結果が負かだけを確認する。overflow が二度起きて正値へ戻る場合を検出できない。

64-bit 環境で次は error 無しで `Layout.Size == 8` になった。

```go
buffer.Spec{Planes: []buffer.PlaneSpec{
    {Size: 10},
    {Size: math.MaxInt, Padding: math.MaxInt},
}}
```

二つ目の plane は offset 10、size `MaxInt` と記録される一方 backing allocation は 8 byteであり、`Handle.Plane(1)` は slice-bounds panic になった。

**影響**

誤った第三者 plugin の layout が、allocator quota を過少計上した破損 Handle を生成し、後段で Host を panic させる。

**必要な是正**

- align、size、padding、累積 position の全加算を事前 checked arithmetic にする。
- plane range が最終 layout size 内にある invariant を allocation 前と view 作成前に検査する。
- `MaxInt`、最大 alignment、複数 plane、padding、zero-size の property/fuzz test を追加し、「error または valid layout、panic は不可」を固定する。

### R-09 [P2] public testkit が M6 必須の bounded Suggest を検査しない

**根拠**

- [quality.md の M6 完了条件](quality.md#m6-完了条件) は common contract に `Compile` purity/repeatability と bounded `Suggest` の両方を含める。
- [`testkit.executeCase`](../../testkit/runner.go)（196〜213 行目）は deadline を変えた plan fingerprint、cancel、success path を検査するが `Suggest` を呼ばない。
- `testkit/` 内に `Suggest` の呼出しは無い。`plugin/spec_test.go` は framework 自身の limit/duplicate 検査であり、公式または第三者 component ごとの typed conformance ではない。
- 公式 linear PCM component は実際に `Suggest` と `SuggestionLimit: 1` を宣言するため、consumer が無い contract ではない。

**影響**

公式 component が limit、repeatability、canonical uniqueness、invalid candidate を破っても、M6 の「第三者と同じ入口」の typed conformance を通過する。現在の M6 complete claim はこの項目について形式だけになっている。

**必要な是正**

- generic typed case に input/Need と expected candidate を渡せる Suggest scenario を追加する。
- limit、repeatability、duplicate、invalid config、panic、deadline/cancel 非依存を共通 runner で検査する。
- Suggest を宣言した executable component に Suggest case があることを coverage registry で機械的に検査する。

### R-10 [P2] Snapshot に roadmap owner がなく、local file の StableSize も成立しない

R-10 は export 管理と実 file semantics が結び付いた計画上の欠陥である。

**根拠**

- [`access.Snapshot`](../../access/snapshot.go) は production consumer を持たず、test/example 以外から使われない。
- [scope.md](scope.md#m6-の-contract-分類) は担当を「remote Provider を作る milestone」とだけ記すが、正本 roadmap は M0〜M11 固定であり、その milestone は存在しない。
- [`official_conformance_test.go`](../../integration/official_conformance_test.go)（28〜48 行目）も `remote-provider` という roadmap 外の文字列を coverage owner にする。
- [`Coverage.AssignUncovered`](../../testkit/registry.go)（36〜60 行目）は milestone が非空かしか確認しないため、実在しない owner でも gate を通る。
- [`access.Sizer`](../../access/access.go)（21〜24 行目）は StableSize が immutable byte length を約束すると説明する。
- local file Provider は [`source_session.go`](../../plugin/file/source_session.go)（21〜52 行目）で `StableSize` を広告し acquire 時の size を cacheし、`Size`（89〜99 行目）で固定値を返す。しかし別 handle から同じ file を truncate/grow/overwriteでき、同じ session の Read/ReadAt は変更後の content を読む。
- [access.md](access.md) は file identity に size/mtime/digest を挙げ、probe/inspect/run が同じ session と snapshot を使うとしているが、実装は session だけで snapshot を持たない。

**影響**

M10 の final coverage と M11 の no-unused-export を閉じられない。さらに remote source より先に、現在の唯一の official Provider で probe/inspect 後の同一 file mutationを検出できず、planning facts と run bytes が一致しない。cached size と実 bytes が矛盾する場合もある。

**必要な是正**

- `Snapshot` を今削除するか、M7/M9/M10 のいずれか実在する milestone と実 Provider consumer へ割り当てる。`remote-provider` という擬似 milestone は使わない。
- coverage registry は `M0`〜`M11` の許可集合、または正本 manifest の milestone ID だけを受理する。
- local file について strong/weak/no snapshot の意味を決める。少なくとも StableSize を広告するなら、in-place mutationを含む phase 間検査、snapshot copy、または明示的な weak semantics が必要である。
- truncate、grow、same-size overwrite、path replacementを probe/inspect と run の間に行う integration test を追加する。

### R-11 [P2] CLI が rendering/cleanup failure の原因を捨てる

**根拠**

- [`cli.Run`](../../cli/cli.go)（69〜97 行目）は usage/request/plan/prepare error の `render*` error を無視する。
- plan rendering failure 後の `prepared.Close` も捨てる。
- run 後に既存 `runErr` がある場合、`renderResult`（117〜119 行目）の failure は捨てられる。
- embedded API は `ExitCode` しか返さないため、caller は失われた rendering/cleanup error を取得できない。

**影響**

CLI process は非 zero になっても、stderr/stdout failure、cleanup failure、元の runtime failureの組合せを利用者または embedding application が診断できない。broken pipe や埋込み writer failure で特に再現しやすい。

**必要な是正**

- parse/plan/prepare/run/render/close を一つの finalize path へ集約し、独立した failure を `errors.Join` する。
- library-facing entry point は structured result/error を返し、`cmd/godec` の薄い process wrapper だけが ExitCode へ写す。
- failing stdout、failing stderr、plan render + close、run error + result render、cancel の matrix test を追加する。

### R-12 [P3] runtime と testkit の責務が単一 file に集まり過ぎている

[`internal/run/drive/drive.go`](../../internal/run/drive/drive.go) は 866 行で、binding/scope/link/task、source/writer/processor、fan-out、buffer、observe、zip を同居させる。[`testkit/runner.go`](../../testkit/runner.go) は 804 行で、case orchestration、scenario construction、fixture provider/session、operator、lifecycle assertion を同居させる。

現時点で correctness failure の直接原因ではないが、M7 の multi-stream/mapping/seek と M10 の conformance 拡張を同じ file へ追加すると、owner と invariant の境界がさらに読みにくくなる。AGENTS.md の「責務によって構造的に分割する」にも合わない。

次の責務単位へ分ける。

- drive: graph binding、edge/queue、linear processor、fan-out/fan-in、boundary source/sink、observation
- testkit: public runner、plan assertions、runtime lifecycle assertions、fixture access、scenario factory、failure expectation

package を増やすこと自体を目的にせず、private type と test fixture の owner が一意になる位置で file/package を選ぶ。

## 計画そのものへの指摘

### 1. 「test green」と「contract成立」を別の gate にする

現在の full suite は green だが、R-01、R-02、R-04、R-08 は数行の公開 API probe で再現できた。完了条件の文章が存在するだけでは不十分で、各 normative statement を assertion または明示的な未 cover assignment へ対応付ける必要がある。

M7 以降は milestone ごとに次の traceability table を持つ。

| Contract ID | normative statement | production consumer | test/helper | negative/adversarial case | owner milestone |
|---|---|---|---|---|---|

coverage percent ではなく、この表の未対応行を gate にする。

### 2. public API と private runtime transport を分ける

`flow.Owned` は private queue の都合を public component surface へ漏らした結果、`Item` の単一 ownership rule を迂回している。`ResolvedView.Value` も type erasure の都合を公開 mutable stateとして露出する。内部都合の token/closure/erased value を export する前に、第三者が安全に使う必要がある操作かを再評価する。

### 3. 「consumer が現れるまで export しない」を機械化する

`access.Snapshot` の owner は roadmap 外文字列のまま通った。次を docs-check または専用 manifest で検査する。

- owner milestone は正本の M0〜M11 に存在する。
- `完了` milestone に属する export は production consumer または実行済み conformance caseを持つ。
- 将来 owner の export は、その milestone が着手されるまで原則 source に置かない。

### 4. hostile ではなく「誤る第三者 plugin」を設計対象にする

in-process sandbox は目標外でも、plugin の accidental panic、mutable borrow、overflow、double release を Host 全体の corruption にしないことは別問題である。第三者を信頼することと、誤りを局所化しないことを混同しない。public testkit は happy path だけでなく、panic、alias mutation、boundary integer、repeat call を標準 scenario に含める。

## 推奨修正順

1. **safety boundary**: R-01、R-02、R-03、R-07、R-08
2. **semantic identity/data preservation**: R-05、R-06、R-04
3. **完了 gate と計画整合**: R-09、R-10
4. **surface/maintainability**: R-11、R-12
5. 全修正後に full runner、race、docs-check、generator、official/out-of-tree conformance、WAVE exact vectors を再実行する

R-03 と R-06 は公開 API の形を変える。後方互換性が不要な今、既存 method を残す compatibility layer ではなく、安全な contract へ利用側を一括移行する。

## M6 再完了条件

本監査に対する M6 再完了は、少なくとも次を満たした時とする。

- R-01〜R-07 の P1 が実装と negative regression test の両方で閉じている。
- R-08〜R-11 が閉じているか、実在する roadmap milestone、実 consumer、機械検査可能な完了条件へ割り当てられている。
- `checkpoint.md` の ownership 注記が実際の public export と一致する。
- public testkit が official linear Suggest、ownership exact count、borrow isolation、panic boundary を第三者と同じ入口で検査する。
- WAVE の input-derived `JUNK` を含む unknown chunk/padding exact vector が integration で通る。
- local file の mutation/snapshot semantics が文書と capability 宣言で一致する。
- full verification が green で、review 用の temporary probe/source が tree に残っていない。
