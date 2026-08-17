# M0〜M6 実装監査

> 監査日: 2026-08-13  
> 対象 commit: `0780b145e6f755c447d3a288dd98516da7f9df61`  
> 対象 branch: `m6-ownership-cell`  
> toolchain: `go1.26.4 windows/amd64`

> **Historical record.** 初回監査の結論、gate output、R-01〜R-16 の状態と日付は、その時点の記録として traceability のために残す。現在の normative status ではない。特に `2026-08-14` の completion claim は superseded であり、current status と final gate の記録は以下の R-17 と M6 再完了条件を正本とする。

## 結論

初回監査 (2026-08-13) と R-01〜R-16 の follow-up は、module 境界、typed media path、planner/runtime、file transaction、WAVE/PCM の実経路、第三者相当 plugin、CLI までを接続する過程と、その時点の是正を記録した historical record である。現在の tree では、R-17 が受理した ownership/panic、StableSize/snapshot、queue termination、file boundary、WAVE INFO、cancellation projection の contract を同期する。2026-08-17 の final verification と独立 Terra review の no-finding をもって M6 を再完了とする。

review set の現在の扱いは次のとおりである。

| review set | 件数 | 現在の扱い |
|---|---:|---|
| R-01〜R-12 | 12 | historical remediation; current normative status ではない |
| R-13〜R-16 | 4 | historical follow-up; R-17 によって current contract を再同期する |
| R-17 | 1 | accepted current contract; final verification complete 2026-08-17 |
| current total | 17 | M6 再完了（2026-08-17） |

2026-08-13〜2026-08-17 の各 finding status は、実装経緯と再 open の理由を追跡するために残す。`2026-08-14` の「すべて閉じた」という記述を含め、過去の完了宣言を現在の M6 gate の代用にはしない。

## 監査範囲

次を対象にした。

- [refactor.md](../refactor.md)、[checkpoint.md](checkpoint.md)、`docs/refactor/` の領域別 contract、M2〜M6 task 文書
- `_legacy/` を除く production source、公開 API、official plugin、`standard`、CLI、public testkit、integration module
- config resolution、plugin Shape/Compile/Suggest/Open、planner、runtime ownership、fan-out/COW、Access session、spool、file transaction、WAVE inspect/demux/mux/metadata、surface rendering
- unit/integration/race test、coverage の薄い箇所、完了条件と test assertion の対応

`_legacy/` は AGENTS.md の規則どおり、移植参照であって現行実装ではないため監査対象から除外した。

## 検証結果（初回監査時点の historical record）

次の gate は 2026-08-13 の初回監査時点で成功したと記録されている。これは current final gate の結果ではない。

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

追加で Windows file transaction の既存 target 置換と permission 保持 test、CLI help、root/integration の coverage を確認した、という初回監査記録がある。Go 1.26.4 の Windows `os.Rename` は `MOVEFILE_REPLACE_EXISTING` を使うため、[access.md](access.md#m6-完了条件) の既存 target 置換方針に問題は確認しなかった、という記載も historical である。

coverage は合否条件にはしていないが、未検査 contract の探索に使った。代表値は root の `config` 70.0%、`flow` 61.7%、`testkit` 46.9%、`internal/bind` 2.1%、integration から root 全体を含めた値が 63.1% だった。後述する問題の多くは、低い行 coverage そのものではなく、assertion が contract を検査していないことに起因する。

## milestone 判定（初回監査時点の historical record）

| Milestone | 監査時の判定 | 根拠 | 是正後 |
|---|---|---|---|
| M0 | 完了維持 | baseline commit、manifest、再現条件、意味上の比較軸が残っている。今回の現行実装の欠陥は baseline artifact を無効にしない | 完了維持 |
| M1 | 完了維持 | root/integration/tools の module DAG、tracked workspace、generator bootstrap、`GOWORK=off` の tools build が成立している | 完了維持 |
| M2 | 要是正 | R-01、R-05、R-06、R-07 により secret、normalization、immutable config、panic-free construction の contract が未成立 | 是正済み |
| M3 | 要是正 | R-03、R-08、R-10 により borrowed media、allocator bounds、Access snapshot/StableSize の contract が未成立 | 是正済み |
| M4 | 要是正 | R-06 と R-09 により pure Shape/Compile と bounded Suggest の検証が不十分 | 是正済み |
| M5 | 要是正 | R-02、R-03 により exact-once ownership、panic cleanup、COW 強制が未成立 | 是正済み |
| M6 | 要是正 | R-04、R-09、R-10、R-11 により WAVE preservation、public testkit、file snapshot、CLI failure UX の完了条件が未成立 | 是正済み |

## Findings

> R-01〜R-16 は historical/superseded findings である。各節の実装 narrative、negative test、日付、`状態: 解決済み` は traceability のために保持し、current M6 completion evidence としては使わない。R-17 が現在の normative contract と final-review checklist である。

### R-01 [P1] secret が標準 formatting から漏れる

**Historical status (superseded): resolved in 2026-08-14 review**

2026-08-13 の修正は config 型の formatter だけを直しており、panic recovery が recovered value を文字列化する経路が残っていた。plugin が `panic(errors.New(secret.Reveal()))` すれば credential が diagnostic、Result、CLI 出力へ出る。2026-08-14 に `diagnostic.Recovered` を追加し、plugin の phase panic、Host の invoke、runtime task、observation sink、testkit の Parse/Marshal と output snapshot をすべてそこへ通した。panic 値は panic した側が選ぶ data なので型だけを報告し、message を残すのは `runtime` package が宣言した型だけとする。`runtime.Error` は公開 interface であり第三者も実装できるため、interface の充足ではなく宣言 package で判定する。panic 値を保持していた `task.PanicError` と `observe.SinkPanicError` も、`%#v` で raw value が出るため保持をやめ、安全な要約と stack だけを持つ。panic の位置は元から別に保持している stack trace が示す。`diagnostic` と `plugin` の回帰 test が、secret を載せた panic 値を string、error、named type、slice、map、struct、pointer で投げても error と diagnostic detail に出ないことを固定する。再発条件は個別に固定する。第三者が宣言した `runtime.Error` 実装は `Recovered` が message を残さないこと、`task.PanicError` と `observe.SinkPanicError` は `Error`/`%v`/`%#v` のいずれでも recovered value を出さないこと、testkit の Metadata Parse/Marshal boundary が報告する text にも出ないことを、それぞれの package の test が検査する。検査に失敗した test 自身も raw value を出力せず、漏れた rendering の名前だけを報告する。

以下は 2026-08-13 の記録である。`SecretValue`、`Patch`、`Resolved`、`ResolvedView` に全 verb を安全に処理する formatter を追加した。formatter method が呼ばれない named type や unexported outer field でも raw value を反射表示できない opaque storage とし、struct/pointer/slice/map/interface、typed/type-erased resolved value、主要な verb/flag/width/precision の回帰 test を追加した。`Patch` の表示は schema 解決前であるため preset、field ID、source のみを残し、typed/text value は一律非表示にした。secret contract を検査する test failure 自身も raw value を出力しない。

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

**Historical status (superseded): resolved in 2026-08-14 review**

> **この節の第 6〜第 8 review の記述は現在の正本ではない。** `journal.Scope`、`journal.Outcome`、`journal.Capture`、`Scope.EnterOperation`、`BindScope`、`EventID{Task, Attempt, Seq}`、および Host の echo 判定は [R-13](#r-13-p1-failure-の保管場所がそれを報告する-slot-より短命だった) で Ledger／Domain／Span の三層へ置き換えた。ownership contract のうち現在も有効なのは「cell が唯一の所有権表現であること」「`Drop` が panic も error も返さないこと」「第三者 `Drop` を runtime mutex の内側で呼ばないこと」であり、報告先と結末の保管に関する記述は R-13 を読むこと。

2026-08-13 に `Owned[T]` を削除したが、`Item.Detach` が copyable な `T` を返し `SetWithTraits` が同じ値から複数の cell を作れたため、同じ経路が名前を変えて残っていた。`Transfer` も `handed` を `target.Set` より先に立てており、target の `Drop` が panic すると新 payload が誰にも保持されないまま元 payload の解放も無効になっていた。`Set`/`SetWithTraits`/`Fork` にも、保持物の `Drop` が panic した時に受け取り中の payload を失う同型の欠陥があった。

2026-08-14 に次を直した。bounded queue の ring が `flow.Item` を保持するようになり、runtime の hot path から生の値と drop trait を持ち回す経路が消えた（`Move` だけで受け渡すので allocation も増えない）。call stack の外へ payload を置く必要がある collector と transport は cell を heap に置いて `[]*flow.Item[T]` のように保持する。最初は single-consume token (`Parcel`) を用意したが、`Value()` から生の値を取り出せば `NewItemWithTraits` で二人目を作れてしまい escape hatch が残るうえ、queue が `Move` へ移った後は非 test consumer も無かったため削除した。`Set` は「渡された payload を保持するか解放するかのどちらかを必ず行う」ようになり、保持物の解放が panic した場合も受け取り中の payload を解放してから panic を通す。`Fork` も同じ経路を使う。`Transfer` は build が成功した時点で解放義務を結果へ移し、その後は `Set` の不変条件に委ねる。さらに、第三者 `Drop` を runtime mutex の内側で呼ばないようにした。`Pop` は受け取り先 cell の解放を lock の外で先に行い、`Drain` は ring 全体を lock 下で取り出してから解放する。lock 中の panic は mutex を握ったままにし、後片付けをするはずの `Drain` が同じ mutex で止まるため、panic が recovery boundary へ到達しなかった。複数 owner の後片付け（queue drain、fan-out branch、fan-in batch）は一つの helper に集約し、一件が panic しても残りを解放してから failure をまとめて返す。cleanup は recovery boundary を失った経路で走るため、panic ではなく error で伝える。この helper は `flow.DropAll` として公開していたが、cell を同時に複数持つのは runtime だけであり、`[]Item[T]` を受ける signature が「cell は常に pointer で渡す」という public contract と衝突するため、`internal/run/release` の `release.All` へ移した。

その failure を捨てないところまでが contract である。fan-in は batch ごとの解放結果をその場で返し、`Execution.Discard` は全 task の failure を結合し、Host は通常終了でも cleanup 経路でも Result へ載せる。戻り道の defer で走った cleanup も同じで、ここは panic が task の戻り値を捨てるため join だけでは届かない。第 4 review までは task の `Scope` に cleanup failure を持たせ、boundary が panic 時だけそれを読む形にしていたが、この「戻り値と journal の二重経路」自体が穴を残していた。処理中の item の `Drop` が panic すると、それが primary panic を置き換えてしまう。**`Item` を「ある failure domain に属する ownership slot」とし、`Drop` を panic も error も返さないものにして、cleanup failure の出口を domain 一つに絞った。** task の結末は `journal.Outcome{Primary, Cleanup}` として構造で保持し、boundary は normal/error/panic を問わず常に journal を seal する。読み出し規則の分岐は無くなり、`Set`/`Fork` の「保持物の解放が panic したら受け取り中の payload を解放する」分岐と `release.All` の error 集約も消えた。あわせて、edge の drain task が queue から取り出し中の item を task 単位の flag で持ち、戻り道で `Abandon` するようにした。精算しないと `active` が処理していない item を数え続ける。ただし `Complete` にはしない。abandon された item は下流で完了していないため edge は quiescent にならず、barrier は失敗した task の cancel で終わる。ここで idle を返すと、Host が死んだ data path の上で Finalize と Flush を実行し、failure の phase も run から finalize へずれる。item を終えたと言えるのは consumer が受け取る機会を得て、受け取らなかった payload の解放まで成功した時であり、consumer の error、panic、宣言された `Drop` の panic を区別しない。fan-in にも同型の欠陥があった。`barrier` は task の終了を待つが、deferred な `close(done)` は panic 中も走るため、panic した join が barrier を成立させていた。input を EOF まで drain し、かつ戻り道の cleanup がすべて成功した時だけ quiesce したものとして扱う。EOF は quiesce しうる終わり方を示すだけで、保持していた未 join の batch を解放できなければ task は失敗している。`flow`、`release`、`queue`、`drive`、`run` の回帰 test が、panic 後に mutex が解放されていること、全 owner が解放されること、`Set`/`Fork`/`Transfer` の panic 時に payload が失われないこと、解放失敗が fan-in・`Execution.Discard` から返ること、そして値を終えられなかった後の edge が `active` を精算しつつ barrier では idle を報告しないことを固定する。edge は consumer の error・panic・declined payload の解放 panic を、join は EOF 後の cleanup 失敗を、それぞれ「終えられなかった」側として扱う。詳細は [runtime](runtime.md#ownership) を正本とする。

第 6 review で二つの独立した P1 が見つかった。一つは identity で、`journal.Scope` を「Run の後 Seal し、次の attempt を開いて Flush を同じ journal に書く」形にしていたため、同じ task の Run と Flush の failure が両方 `Seq` を 1 から数え、衝突していた。`EventID` に `Operation`（Run/Flush/Discard）を加え、`Scope` は一つの lifecycle operation だけを覆う contract にして、`Seal()` を terminal にした（sealed 後の書き込みは証拠を失わず、次の `Seal()` が返す）。もう一つは、存在しない slot への `Set`（`var slot *Item[T]; slot.Set(...)`）が nil receiver のガードで黙って return していたこと。未宣言の slot と同じ扱いにして panic するようにした。

この二つを直す過程で、buffered edge の下流 close を Host が別 journal で回収しようとする実装が、`-race` で実際のデータ競合として現れた。原因は barrier の意味を取り違えたことにある。`Queue.WaitIdle` が保証するのは ring が空であることだけで、drain task の goroutine が戻ったことではない。むしろ drain task は意図的に生き続ける — Quiesce の後、Finalize が生成する遅延 Flush 出力を同じ queue で受け取るためである。したがって bounded edge の下流 close は、Host が `Execution.Finish` から新しい journal を開いて行ってはならず、drain task 自身が EOF を見た時に**自分の Run journal の続きとして**行う。source と join は違う: source は `WaitSources` が、join は barrier 自身が `<-done` を待つことで、Host が新しい journal を開く前に goroutine の終了を確認している。`Execution.Finish` は `namedTask.chain` を持つ entry（source と join）にだけ Host 主導の Flush journal を開き、bounded edge の namedTask（`chain` を持たない）はこの経路から外した。

この時点では、buffered edge を経由した plugin failure の phase が `RunPhase` のままで、direct chain の Flush failure が `FlushPhase` になるのとは topology 依存で異なっていた。「Flush failure が消える」「journal を別 goroutine から読んで競合する」の二つを優先して閉じ、この不一致は残課題としていた。

続けて、これも直した。`journal.Operation` を `Scope` 単位の固定値ではなく **failure を記録した時点の label** にし、`Scope.EnterOperation(operation) Operation` で goroutine が自分自身のラベルを途中で書き換えられるようにした。bounded edge の drain task は、EOF を見て `target.close(ctx)` を呼ぶ直前に `EnterOperation(journal.Flush)` する。別 goroutine を起こさず、別 journal も開かない — 同じ journal・同じ writer のまま、これから記録する failure の分類だけを変える。cross-goroutine の同期問題を再導入しない。ring は Pop が EOF を返す条件（`count == 0 && closed`）から、この時点で証拠上空であり、Close は不可逆なので、ラベルを戻し忘れても以後この goroutine が Run 種別の failure を記録することはない。`Outcome` 自身が持っていた `Operation` field は削除した — 一つの Outcome の中で Primary と Cleanup が異なる operation を持ちうる以上、Outcome 単位の label は意味を失っており、Host は各 `Failure.Operation` を見て phase を決める。`internal/run` の回帰 test が、buffered edge の Flush failure が `journal.Flush` を持つこと、direct chain の Flush journal は release failure だけを持ち Finish の error 自体は別経路（`Execution.finishErr`）で返ることを固定する。`host` の回帰 test は、source → processor(Flush が失敗) → sink という review 記載どおりの構成で `Result.Primary.Phase == FlushPhase` になることを、buffer の有無に関わらず固定する。

Host 側では、`addCleanup` の内容ベース `errors.Is` 抑止（第 5 review で指摘）を、`Result.Primary` が確定した後にだけ、かつ primary 種別の failure（release failure ではなく、task を止めた failure）にだけ効く形へ絞った。`r.ctx` の cancellation cause と一致する primary は、data task 自身の journal と、Host が別経路（Quiesce の失敗など）で先に検出した primary の、同じ一つの出来事の二度目の観測なので、二重報告しない。release failure（cleanup 種別）はこの判定に一切かからないため、無関係な cleanup event が primary の文面と偶然似ているという理由で消えることはない。

**Historical status (superseded): resolved in 2026-08-16 review 7**

第 7 review で三点が見つかった。

一つ目は P1。`namedTask.flush` が開く fresh Flush Scope に、`n.task.Finish(ctx)` が panic すると `Seal()` が一度も呼ばれない経路が残っていた。panic は `Execution.Finish`・Host の invoke 経路をそのまま素通りし、fresh Scope が記録していた cleanup（unwind 中の `Drop` 失敗など）は誰にも回収されない。buffered 経路は `task.Group` の defer が panic 後も `Seal()` するため保存されるのに対し、direct 経路（Host が `Finish` を直接叩く）だけ保存性が欠けていた。`journal.Capture(scope, work) Outcome` を追加し、`task.Group.run` と `namedTask.flush` の両方がこの一つの helper を通るようにした。`work` の panic を recover して `scope.Panicked` に記録し、defer の中で named return へ `scope.Seal()` を代入するため、panic 経路でも必ず seal に到達する。あわせて指摘どおり、`Execution.Finish` の戻り値を `([]journal.Outcome, error)` の二経路から `[]journal.Outcome` 一本化した。Finish の error・panic はどちらも `namedTask.flush` が返す Outcome の `Primary` に入り、Host の `acceptOutcomes` は `acceptTaskReport` と同じ「Primary は run の primary を競う、Cleanup は必ず cleanup」の規則で読む。

二つ目は P2。`acceptTaskFailure` の echo 判定が `errors.Is(value.Err, r.result.Primary.Err)` という内容比較のままで、別 task の独立した failure が同じ sentinel error を返した場合に誤って握り潰されていた。`Outcome.Cause()` が返す cancellation cause に `journal.Cause{Event EventID, Err error}` として event の出自を持たせ、Host の echo 判定は `errors.As` で `context.Cause(r.ctx)` から `*journal.Cause` を取り出し `cause.Event == value.ID` で比較するように直した。`task.Group` 内のピア間 cancellation echo 判定（`cancellationEcho`）は、ピアが `context.Cause(ctx)` をそのまま自分の error として返す既存パターンにより、同じ `*Cause` 値を比較することになるため変更不要だった。`r.result.Primary != nil` のガードは残す。確定前の最初の観測（cancellation の原因そのものであっても）が `Result.Primary` になる資格を失わないようにするためである。

三つ目は P2。`journal.EventID` が `Operation` を identity の一部として使っており、「Scope に固定された値でなく記録時点のラベル」という `EnterOperation` の設計と概念上ねじれていた。`EventID` から `Operation` を外して `Attempt`（`journal.New` のたびに process 全体で採番される、Scope オブジェクト一つに対して一つの値）に置き換え、`Operation` は `Failure` 自身が持つ metadata にした。identity は将来 retry や複数 flush が増えても不変のまま保たれる。

sealed 後に production が二度目の `Seal()` を呼ばない限り証拠がメモリに残るだけ、という指摘（durable な親 ledger が要るという意見）は今回は見送った。`Capture` の導入で、この codebase 内の boundary はすべて必ず `Seal()` に到達するようになっており、「二度目の `Seal()` が呼ばれない」状況は現状のコードには存在しない。将来 production 外の consumer がこの package を直接使う場合の備えとして記録だけ残す。

Go では「自分が所有していない値の所有権を宣言する」ことを型で防げないため、`NewItem`/`NewItemWithTraits` に借用値を渡す誤用は残る。cell が保証するのは、一度 cell に入った payload の所有権を `flow` の API で複製できないことである。

**Historical status (superseded): resolved in 2026-08-16 review 8**

第 8 review で四点が見つかった。うち二点は P1 で、いずれも「見送った」という前回の判断そのものを覆す再現を伴っていた。

一つ目は P1。`internal/run/execution.go` の `BuildObserved` は、`drive.Source` を組み立てる時に `output.BindScope(scope)` は呼ぶが、`sourceTask.BindScope(scope)` を呼んでいなかった。`Joiner` の分岐は `joinTask.BindScope(scope)` を呼んでおり、対称性が壊れていた。`sourceTask` が内部で保持する item（Reader が `Read` で埋め、`Emit` へ渡す前に持つ slot）は `OpenSource` の中で作られる匿名の使い捨て Scope に bind されたままになり、誰にも Seal・収集されない。downstream が値を受け取らずに declined した時、source 自身の deferred `Drop` が失敗しても、その failure は Outcome に一切現れない。回帰 test は byte 制限で `Push` を deterministic に reject させ、宣言された `Drop` が panic する schema を使い、`sourceTask.BindScope(scope)` を欠いた状態で `Cleanup` が空になることを固定してから、`execution.go` に一行足して閉じた。

二つ目は P1。`namedTask.flush` は Flush 用に新しい `journal.Scope` を毎回開き、`n.chain.BindScope(flush)` で chain を付け替えていた。しかし [runtime.md](runtime.md#ownership) 自身が許可している「call stack の外へ payload を置く必要がある collector/transport は cell を heap に置いて保持する」pattern に従うと、Run の間に `Emitter.Own` で埋められた retained cell は、最初に bind された時点の Scope（= Run の Scope）を reporter として憶えたまま、`Item.Bind` の「既に payload を持つ slot には触れない」規則により、Flush 用の新しい Scope へは rebind されない。Run が Seal された後、Flush でその retained cell を解放しようとして失敗すると、failure は誰も二度目の `Seal()` を呼ばない古い Scope へ記録され、回収されない。第 7 review 時点で「`Capture` の導入によりこの codebase 内の boundary はすべて必ず `Seal()` に到達するので、二度目の `Seal()` が呼ばれない状況は存在しない」と判断したのは誤りだった。その判断は「goroutine が漏れて bound な slot を持ち出す」経路だけを調べており、同一 goroutine 内で単に Run と Flush の間で Scope オブジェクトが差し替わるだけでも retained cell が古い Scope に取り残される、という経路を見落としていた。是正は `namedTask.flush` が新しい Scope を開くのをやめ、bounded edge の drain task が既に行っている `Scope.EnterOperation(journal.Flush)` による relabel を、goroutine を跨いだ hand-off に対しても行うようにし、同じ Scope オブジェクトへ二度目の `Capture`/`Seal()` を呼ぶことである。これにより retained cell の reporter は Run と Flush を通じて同じ Scope を指し続け、対称性の欠如そのものが消える。回帰 test は `Process` で cell を retain し `Flush` でその解放が panic する processor を使い、Scope を明示的に一度 Seal してから `named.flush` を呼んで `Cleanup` が空になることを固定してから閉じた。あわせて `Execution.Finish`/`namedTask.flush` の doc comment と [runtime.md](runtime.md#ownership) を、Scope が「ちょうど一つの lifecycle operation を覆う」という記述から「同じ Scope オブジェクトが複数の operation にまたがってよい」という記述へ書き直した。

この是正の最初の実装は `-race ×3` の検証 gate で実際のデータ競合を出した。source の `current.done <- err` は `current.task.Run(ctx)` の直後、`work` の中から送っており、`Capture(scope, work)` は `work` が返った**後**に `scope.Fail`・`scope.Seal` を呼ぶ。`WaitSources` がこの送信を受けて Host が `Execution.Finish` へ進んだ時点では、送信元の goroutine はまだ `Capture` の中で同じ Scope に書き込んでいる最中でありうる。join の `barrier` が待つ `s.done` も同型で、`run` 自身の defer が `Capture` の `Seal()` より前に閉じていた。「`WaitSources`/barrier が goroutine の終了を確認している」という前提そのものが誤りで、確認できていたのは「`work` が返った」ことだけであり、「`Capture` がその Scope への書き込みを終えた」ことではなかった。是正は `task.Group.StartScopedNotified` に `Capture` の**後**にだけ呼ばれる `notify func(journal.Outcome)` を追加し、source の完了信号は `work` の中ではなく `notify` の中でだけ送り、join の `s.done` も `run` の defer ではなく `notify`（`zipState.sealed`）から閉じるようにした。回帰は `-race` 自体が検出したため、専用の負の再現 test は追加せず、修正後に `-race ×3` を再実行して再現しないことを確認した。

三つ目は P2。第 7 review で直した Host 側 echo 判定 `errors.As(context.Cause(r.ctx), &cause) && cause.Event == value.ID` は、`r.result.Primary` が実際にその cancellation cause 由来かどうかを確認していなかった。direct chain 自身の Flush failure が `Execution.Finish` を通じて先に `Result.Primary` になり、その後 buffered task 自身の独立した failure が run 全体を cancel していた場合、後者の EventID が `context.Cause(r.ctx)` と一致するというだけで、無関係な Primary の陰に隠れて握り潰されていた。是正は `context.Cause(r.ctx)` を経由するのをやめ、`errors.As` で `r.result.Primary` 自身の error chain から `*journal.Cause` を取り出して比較するようにした。`host.Failure` は既に `Unwrap() error` を持つため、production の `failureOf` が `err` を `Failure.Err` にそのまま格納する経路と一致する。回帰 test は独立した failure を `Outcome.Cause()` から作り、無関係な direct Flush failure を `Result.Primary` に据えた状態で `acceptTaskReport` を呼び、その独立した failure が `Result.Cleanup` に現れることを固定した。既存の echo 回帰 test も、`generic.Err` を `cause.Err`（unwrap 済み）ではなく `cause` そのものに直し、production の組み立て方と一致させた。

四つ目は P3。[runtime.md](runtime.md#ownership) の `EventID` の説明が `{Task, Operation, Seq}` のままで、同じ文書内の `{Task, Attempt, Seq}` という記述と矛盾していた。また「Scope はちょうど一つの lifecycle operation を覆う」という記述も、直後の `EnterOperation` による relabel の説明と矛盾していた。二つ目の是正と合わせて該当段落を書き直し、内部矛盾を解消した。

前回（第 7 review）の「sealed 後の証拠を durable な親 ledger へ送る件は見送った」という判断は、以上の理由で撤回する。二つ目の P1 が示すとおり、この codebase の公開 ownership contract が許す使い方の範囲内で、証拠が回収されない状況は現に存在した。ただし今回の是正は、その具体的な経路（Run と Flush が別の Scope オブジェクトを使うこと）を塞ぐことで閉じており、reviewer が提案した Run Ledger / Task Domain / Run・Flush・Close Span の三層再設計はまだ採用していない。現状のコードには、それでもなお Scope の Seal が一度きりで証拠が失われる経路は確認できていない -- ただし今回もその確認は「見落としがないと確信できるまで調べた」以上のものではなく、三層再設計は将来 production 外の consumer が `journal` package を直接使う場合や、retry/複数 flush が増える場合の備えとして記録に残す。

**Historical gate record:** `internal/run`・`host` の回帰 test は、いずれも fix を外すと該当 test が失敗することを個別に確認してから fix を適用し、再度緑になることを確認した。`go build ./...`、`go vet ./...`、`gofmt`、全 package の `go test ./...`、`go test -race ./...`（3 回）、`go run ./tools/cmd/test-runner --simd`、`go run ./tools/cmd/docs-check`、`go run ./tools/cmd/generate`（再実行後 diff なし）、`tools` module の `GOWORK=off go build ./...` はいずれも green と初回監査に記録されている。これは current final gate の結果ではない。

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

**Historical status (superseded): resolved in 2026-08-13 review**

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

**Historical status (superseded): resolved in 2026-08-13 review**

reservation slot の `JUNK` を専用 anchor (`chunkReservation`) の raw chunk として保持し、mux は自前の空 slot を作る代わりにその byte 列を同じ slot へ書き戻す。RIFF → RIFF の roundtrip は byte 一致し、繰り返しても chunk が増殖しない。RF64 昇格時は `ds64` がその slot を占めるため保持 byte が失われるが、header 長は data size 確定前に固定されるため他所へ移せない。この loss を [capability](capability.md) の B8 と [media](media.md#m6-完了条件) へ契約として明記し、M7 の loss report が実 consumer になった時点で report 対象にする。unit test は anchor 判定（reservation slot／同 size 別位置／別 size／odd padding）、non-zero payload の 2 回 roundtrip、RF64 昇格時の置換を固定した。integration の `preservedChunks` は `JUNK` 全体を除外せず、writer 自身が生成する空 reservation だけを除外し、non-zero reservation を持つ入力の end-to-end 変換で slot の byte 一致を検査する。

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

**Historical status (superseded): resolved in 2026-08-13 review**

`Schema.finish` から field-order normalization を `Schema.normalizeFields` へ切り出し、`Nested` がそれを再利用する。`UnionCodec` は選択 variant の Normalize を委譲し、diagnostic path を `value` 以下へ付ける。Union の Validate も同じ path 規則へ揃え、未登録 variant は `variant` を指す。`SecretCodec` は Normalize と Validate の inner diagnostic を捨てず、severity と code を保った redacted diagnostic へ写す。message、detail、値由来の path は落とす。Validate が inner の全 diagnostic を単一 error へ潰していた挙動もこれで直り、warning が error に格上げされなくなった。回帰 test は Slice/Optional、Map/Auto、Nested、Union、Secret を同時に含む schema で value、provenance、diagnostic path、fingerprint を検査し、secret 経路では raw 値の非漏洩も検査する。

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

**Historical status (superseded): resolved in 2026-08-14 review**

2026-08-13 の `Value()` accessor は snapshot の失敗を捨てていた。snapshot は struct を copy してから field ごとに clone で置き換えるため、clone が panic した field は保持値を指したままになる。それを返すと、fingerprint も diagnostic も変わらないまま Shape が Compile 用の値を書き換えられ、R-06 の中心がそのまま再発していた。2026-08-14 に `Resolved.Value` と `ResolvedView.Value` を `(値, error)` に変え、clone が失敗した snapshot は返さないようにした。panic を閉じ込めるだけでは足りず、失敗そのものを caller へ渡す必要がある。回帰 test は「resolve 時は clone 成功、その後 clone が panic」という順序で、alias が返らないことと、clone が回復した後は元の snapshot が保たれていることを固定する。

以下は 2026-08-13 の記録である。`Patch` を schema-bound にした。typed entry は `Schema.Key`／`SchemaView.Key` が返す `config.Key` 経由でのみ入り、key が field の宣言 clone を運ぶので `Patch` は set 時と read 時の両方で snapshot を作る。schema を知らない `Set(string, any)` では [C17](decisions.md#c17-config-snapshot-は-codec-clone-だけで構成する) に反せず deep clone できないため、この形にした。text entry は immutable なので schema を要さない。他 schema の key、型不一致、無効 key は `Patch` が diagnostic として保持し、解決時に集約する。`Resolved` と `ResolvedView` は exported field をやめ、`Value()` が呼び出しごとに fresh snapshot を返す accessor になった。これにより Shape が受け取った slice/map を書き換えても、同じ fingerprint の後続 Compile は元の値を見る。`Enum` は variadic の backing slice を複製し、`job.NewNode`／`NewAdaptor`／`NewEndpoint` は constructor と getter の両方で patch を clone する。回帰 test は constructor 入力の後書き換え、getter 戻り値の書き換え、Shape callback 内の書き換え、他 schema key、`Enum` の caller mutation を検査する。

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

**Historical status (superseded): resolved in 2026-08-14 review**

panic を diagnostic に変換するだけでは、失敗した snapshot が alias を返す経路が残っていた。その部分は R-06 と同じ修正で閉じている。以下は 2026-08-13 の記録である。

`config/callback.go` に共通 helper を置き、宣言された callback へ入る経路をすべてそこから呼ぶ。accessor と `Clone` は field boundary（read/write/decode/normalize）で、`Decode`/`Encode`/`Canonical`/`Normalize`/`Validate` は codec の入口で、schema validator・default factory・preset は schema の入口で捕捉する。失敗は phase と field/schema path を持つ `config.callback-panic` diagnostic か、その操作自身の error になる。recovered 値は secret を含み得るため、diagnostic にも error にも operation 名しか出さない。表示専用の `Encode` は失敗 channel を持たないので `<invalid>` へ縮退する。panic matrix test は accessor、clone、decode、encode、canonical、normalize、validate、schema validator、default factory、preset を網羅し、いずれの経路でも panic 値が漏れないことを検査する。

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

**Historical status (superseded): resolved in 2026-08-13 review**

`layoutOf` の alignment 切り上げ、`Size + Padding`、累積 position をすべて事前 checked arithmetic にし、`ErrLayoutOverflow` で拒否する。allocation 前に `validateLayout` が「各 plane が padding 込みで layout size 内に収まる」invariant を検査し、backing を直接 slice する `Mutable.Plane`、`Edit.MutablePlane`、`View.PlaneAligned` は範囲外 plane を `ErrRange` にして slice-bounds panic を起こさない。table test は MaxInt 単独、size+padding overflow、二度の overflow で小さな正値へ戻る場合、累積 overflow、最大 alignment、negative、zero-size を固定し、property test は 500 通りの spec で「error または、allocate と全 plane 読み書きが通る valid layout」を検査する。

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

**Historical status (superseded): resolved in 2026-08-14 review**

2026-08-13 の coverage は `executed` の未知 identity だけを検査し、`suggested` を検査していなかった。「recorded identity はすべて対象 Set に属する」という公開 contract と一致しないため、2026-08-14 に両方を同じ helper で検査するようにし、対象 Set の外にある component の Suggest scenario を記録した場合に失敗する test を足した。

以下は 2026-08-13 の記録である。`testkit.Suggests` を追加した。`Suggestion` は input descriptor、`plugin.Need`、期待 candidate を取り、candidate は redacted な config summary の field 値で表すので secret を test source へ書かずに済む。runner は declared limit、canonical 一意性、component 自身の schema への所属、error diagnostic の不在、繰り返しの一致を検査する。`plugin.Suggest` が panic と invalid config を error にするため、error なしの要求がその両方を覆う。`SuggestContext` は context を持たないので deadline/cancel に依存する余地が構造的に無く、その事実を comment に残した。規則は `verifySuggestions` に切り出し、testkit 自身の test が limit 超過、非再現、重複、未解決 candidate、件数不一致、値不一致、未知 field で実際に落ちることを固定する。coverage registry は `HasSuggest` を宣言した component に Suggest scenario が無ければ失敗する。公式 linear の 5 component すべてに、入力追従・要求 endian 採用・提示なしの 3 scenario を通した。

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

**Historical status (superseded): resolved in 2026-08-14 review**

2026-08-13 の実装には二つの穴があった。sequential-only な session は Format 選択時に prefix replay wrapper へ差し替わるが、wrapper が underlying の `Snapshotter` を委譲していなかった。acquire 時には identity を記録済みなので、run 前の照合で現在 identity が空になり、変更されていない source を変更済みと誤判定する。また `NewSnapshot` が空 identity の `WeakSnapshot` を valid とし、Host は nature を比べずに identity だけを比べていたため、弱い identity すら提供しない Provider が検査を通過できた。

2026-08-14 に、wrapper が underlying へ委譲し、underlying が identity を持たない場合だけ `access.ErrNoSnapshot` を返すようにした。Host はそれを「比較対象が無い」として扱い、「比較して一致した」とは区別する。`NewSnapshot` は `NoSnapshot` 以外に空 identity を許さない。照合は nature と identity の両方を見るので、identity を失うことも弱めることも変化として扱われる。回帰 test は wrapper 越しの identity 保持、identity を持たない source での非誤検出、`NewSnapshot` の valid/invalid 組合せを固定する。

以下は 2026-08-13 の記録である。
`access.Snapshot` を削除せず、local file Provider を実 consumer にした。`access.Snapshotter` を実装する session が現在の content identity を報告し、判断は Host が持つ。Host は acquire 時の identity を記録し、run 開始前と output commit 前に照合して、変化していれば `access/snapshot` failure にする。local file の identity は size と mtime で `WeakSnapshot` として報告する。truncate、grow、mtime が動く overwrite は検出でき、同一 timestamp tick 内の同 size 上書きは検出できない。強い identity は content の読み直しでしか作れないため、nature を偽らず weak と宣言し [capability](capability.md) の B9 に記録した。`StableSize` は渡す byte 列への約束でもあるので、read は acquire 時 size で clamp する。session は開いた path ではなく開いた file を提供するため、path 差し替えは content 変化ではない。coverage registry は `M0`〜`M11` の許可集合だけを受理し、`remote-provider` という擬似 milestone は使えなくなった。同 assignment は削除し、[scope](scope.md#m6-の-contract-分類) の記述も実在 milestone だけを指すよう直した。integration test は truncate、grow、同 size 上書き、path 差し替えを Prepare と Run の間に行う。

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

**Historical status (superseded): resolved in 2026-08-14 review**

2026-08-13 の集約は、run が失敗した時に `Prepared.Close` が返す同じ failure をもう一度 join しており、独立していない一つの failure を二つとして報告していた。`cmd/godec` も、`cli.Run` が既に描画した diagnostic をもう一度 stderr へ出していた。2026-08-14 に、cli は Close の結果が run failure と同一なら追加せず、`cmd/godec` は分類を `os.Exit` へ写すだけの wrapper に戻した。回帰 test は、出力先が既存 directory で commit に失敗する変換で、join された failure に重複が無いことを固定する。

以下は 2026-08-13 の記録である。`cli.Run` は分類済み `ExitCode` と、その裏で起きた独立な failure をすべて `errors.Join` した error を持つ `cli.Result` を返す。parse、request、plan、prepare、run、render、close は一つの outcome へ結果を足すだけで、後段の failure が前段の failure を上書きしない。`cmd/godec` だけが code を `os.Exit` へ写す薄い wrapper である。failure matrix test は plan 描画中の stdout 失敗、result 描画時点の stdout 失敗、planning failure 報告中の stderr 失敗、usage error 報告中の stderr 失敗、cancel、plan 描画失敗と close の同時発生を検査し、成功時に error が nil であることも固定する。

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

**Historical status (superseded): resolved in 2026-08-13 review**

`internal/run/drive` を graph binding (`drive.go`)、link/scope (`link.go`)、task (`task.go`)、boundary sink と linear processor (`linear.go`)、fan-out (`fanout.go`)、bounded edge (`buffer.go`)、observation (`observe.go`)、fan-in (`zip.go`) へ分けた。`testkit` は public runner (`runner.go`)、Format probe (`probe.go`)、plan/lifecycle assertion (`assert.go`)、scenario factory (`scenario.go`)、fixture component と session (`fixture.go`) へ分け、`componentOf` は Subject と同じ file へ移した。package は増やさず、private type とその invariant の owner が一つの file に収まる位置で切った。

[`internal/run/drive/drive.go`](../../internal/run/drive/drive.go) は 866 行で、binding/scope/link/task、source/writer/processor、fan-out、buffer、observe、zip を同居させる。[`testkit/runner.go`](../../testkit/runner.go) は 804 行で、case orchestration、scenario construction、fixture provider/session、operator、lifecycle assertion を同居させる。

現時点で correctness failure の直接原因ではないが、M7 の multi-stream/mapping/seek と M10 の conformance 拡張を同じ file へ追加すると、owner と invariant の境界がさらに読みにくくなる。AGENTS.md の「責務によって構造的に分割する」にも合わない。

次の責務単位へ分ける。

- drive: graph binding、edge/queue、linear processor、fan-out/fan-in、boundary source/sink、observation
- testkit: public runner、plan assertions、runtime lifecycle assertions、fixture access、scenario factory、failure expectation

package を増やすこと自体を目的にせず、private type と test fixture の owner が一意になる位置で file/package を選ぶ。

### R-13 [P1] failure の保管場所が、それを報告する slot より短命だった

**Historical status (superseded): resolved in 2026-08-17 review 9**

> **Superseded history:** R-02 の第 6〜第 8 review に残る `journal.Scope`／`Outcome`／`Capture`／`EnterOperation`／`BindScope`／`EventID{Task, Attempt, Seq}` と Host の旧 echo 判定は、R-13 の三層設計で置き換えられた。これらは経緯であり、現行 contract として参照しない。現在の正本は [runtime](runtime.md#failure-の証拠は-ledgerdomainspan-の三層で持つ) である。

旧設計では failure domain と lifecycle 結末の保管場所を同じ短命 object に置いていた。slot は operation を越えて生きられるため、個別の `Scope`/`Outcome` を延命する対症療法では retained 報告、境界後の cleanup、独立 failure の echo 判定を構造的に保証できなかった。この段落以前の `Scope`/`Outcome` の説明は superseded とする。

確認された failure mode は三つある。

**(1) 匿名で未回収の domain。** task constructor が内部で `journal.New` を呼び、`BindScope` を後から呼ばなければ、その task の slot は誰も Seal しない Scope へ報告する。第 8 review は `sourceTask.BindScope(scope)` の一行で塞いだが、「呼び忘れられる」構造そのものは残っていた。

**(2) sealed Scope への retained 報告。** Run の間に `Emitter.Own` で埋められ、`Process` を跨いで保持される cell は、埋められた時点の reporter を憶える。Flush が別の Scope を開けば、その cell の解放失敗は誰も二度目に読まない Scope へ記録される。第 8 review は「Run と Flush で同じ Scope オブジェクトを使う」ことで塞いだが、Close や Discard、あるいは登録済み background task からの遅延解放には同じ問題が残り、後者は data race でもあった。

**(3) echo 判定が独立した failure を消す。** `Result.Primary` が既にあり、かつ `Result.Primary` 自身の chain の `*Cause` が現在の EventID と一致する、という条件で握り潰していた。direct chain 自身の Flush failure が Primary になり、同時に独立した buffered task failure が run を cancel した場合、後者が消える。第 8 review は比較対象を `context.Cause` から Primary の chain へ移して一例を塞いだが、「Primary の由来」という状態に依存する推論自体が残っていた。

**是正**

責務を三層へ分けた。

- **`journal.Ledger`** は一回の Prepared Run 全体を生きる append-only な failure 保管場所である。Host lifecycle failure も task failure もここに入り、EventID (`{Run, Seq}`) を一元採番する。代表 event の sample が budget で省略されても occurrence は数え続ける。
- **`journal.Domain`** は ownership slot の報告先で、Run 全体を生きる。`Domain.At(node)` が返す `Site` に slot が bind する。Site は immutable で全 Span より長生きするので、Run 中に bind された slot は Flush/Close/join 後のいずれで解放されても回収される。解放の node 属性は「宣言した stage」で固定し、goroutine の現在位置ではない。
- **`journal.Span`** は一つの lifecycle operation の範囲・attribution・recovery boundary である。failure を保持せず、発生時点で Ledger へ書き、停止理由の event 参照だけを持つ。閉じても証拠は到達不能にならない。Span は入れ替わらず入れ子になるので、bounded edge の drain task は自分の Run の内側に Flush Span を開ける — relabel も別 goroutine も要らない。
- **`journal.Cause`** は event identity、元の error、kind を一つの自己完結した値で運ぶ。sample lookup や Cause pin table は不要であり、sample cap 後も cancellation echo を identity で解決できる。
- **stopping snapshot** は Ledger が sample budget と独立して保持する一件の provenance (`Stopping`) である。停止理由を説明する代表が `Events` cap の外へ押し出されても、Primary/Cleanup の投影に必要な node/phase/kind/identity/error を失わない。

これで (1) は task constructor が owner domain を必須引数に取り、chain 全体を自分で bind する形になって構造的に消えた。 (2) は報告先が operation ではなく Run 全体を生きる domain になったので、解放の時点は「どの operation の下に記録されるか」だけを決める。`flow.Item` の未 bind slot への `Move`/`Fork` は拒否し、呼び出しを跨いで保持する component は `plugin.OpenContext.Owner()` から得た `flow.Owner` へ明示的に bind する。(3) は `Ledger.Record` が error graph の各 occurrence を identity 単位で扱う形になり、Primary の有無や context の状態を echo 判定に使わない。同じ sentinel を返した二つの独立した failure は二つの event のまま残る。

`host.Result` は `{Primary, Secondary, Cleanup}` になった。同時に起きた独立した work failure を cleanup に偽装せず、diagnostic にも隠さない。分類は「それが何であるか」で決まり、どの boundary が気付いたかでは決まらない。`host.Failure` は由来の `EventID` を持つ。

**回帰**

三つの failure mode それぞれに直接再現 test を置き、加えて 45 case の lifecycle/ownership matrix（topology × 消費側の終わり方 × 解放の終わり方。slot 種別と operation は topology から決まるため直積にせず、除外理由を [matrix_test.go](../../internal/run/matrix_test.go) 冒頭に明記）、identity 衝突、cancellation echo の分離（同一 sentinel の独立 failure／`context.Cause` の素通し／同一 EventID の再観測／同文面別 EventID／buffer Abandon と cancel cause の分離）、buffered Flush の `-race` を固定した。各修正の要となる guard を一時的に外すと対応する test が確実に落ちることを負のコントロールで確認し、復元した。

**性能**

failure の lock、event 採番、stack depot は failure path に限り、successful `Drop` と fused hop に Ledger allocation は無い。代表 benchmark と allocation gate は [runtime](runtime.md#failure-機構が-hot-path-に持ち込んではならないもの) を正本とする。

**bounded aggregation**

failure storm 時の Ledger 肥大は、最終 `journal.Budget` で hard bound する。`Events` は保持する代表 occurrence の全体上限、`GroupSamples` は一 class の sample 上限、`Groups` は distinct class table と bounded overflow の上限、`Stacks`/`StackBytes` は stack depot の件数/byte 上限である。どの上限も work/cleanup を例外扱いしない。budget 後も occurrence 数、最初/最後の EventID、class の件数、omitted/truncated の事実は saturating counter として残す。stopping snapshot は sample event の代替ではなく、Primary/Cleanup の provenance を一件だけ保持する固定領域である。Cause は EventID と元 error を自己完結して運び、別の pin table を持たない。Host は保持 sample と省略事実を `Result.Suppressed` へ投影する。

### R-14 [P1] 三層設計の第 9 review 後に残っていた境界の欠陥

**Historical status (superseded): resolved in 2026-08-17 review 10**

R-13 の三層化そのものは維持したまま、独立再監査で見つかった 6 件を閉じた。いずれも「証拠を失う」または「独立した failure を一つに潰す」経路である。

1. **[P1] report と Span 境界が原子的でなかった。** `Domain.report` は Span/operation を読んで lock を外し、Ledger 記録後に「現在の Span」を読み直して `observe` していた。その間に `Open`/`End` が挟まると、Span 開始前に始まった report が新しい Span の cause になり、Span 内で始まった report が `End` に間に合わず cause から消えた。許可されている background Drop で到達可能である。是正は **span ticket**: report 開始時に対象 Span を固定して in-flight として登録し、第三者 error の解析は lock 外で行い、commit は取得済み ticket の Span へ行う。`Span.End` は自分に属する in-flight commit を待つ。
2. **[P1] error graph 内に `Cause` が一つあるだけで独立 failure まで echo 扱いになった。** `errors.As` は graph のどこかに `*Cause` があれば既存 event を返し、error 全体を捨てていた。`errors.Join(cause, independent)` で `independent` が消える。是正は **occurrence 単位の分解**: `errors.Join` の枝ごとに分け、Cause の枝だけを解決し、それ以外は独立 event として記録する。単一 wrapper の連鎖は一つの occurrence として保つ（`fmt.Errorf("%w")` は文脈であって二つ目の failure ではない）。あわせて source の `errors.Join(ErrReadWithItem, err)` を単一 unwrap の型に変えた — あれは一つの occurrence だからである。
3. **[P1] budget 後の Cause と停止 provenance が self-contained でなかった。** 代表 event が `Events`/`GroupSamples` cap で省略されると、sample lookup に依存する Cause は解決できず、同じ failure を新しい work failure として再記録しうる。是正は `journal.Cause{Event, Err, kind}` を自己完結した値にし、sample budget と無関係に echo identity と元 error を運ぶこと。さらに Ledger は `Stopping()` として停止理由の node/phase/kind/identity/error snapshot を一件保持し、最終 `Budget` の sample cap 後も Host が Primary/Cleanup を正しく投影できるようにした。別の Cause pin table や `Unresolvable` 概念は採用していない。
4. **[P1] 複数 component の Flush failure が Ledger 前で一つに潰れていた。** `processorDelivery.close`、`fanoutDelivery.close`、`zipState.finish` が別 component/branch の failure を `errors.Join` してから一度だけ Span に返しており、`Result.Secondary` の「独立 failure ごと」という公開 contract を満たせなかった。是正は **`Site.Fail`**: 各 Flush/branch close が自分の failure をその場で個別 event として記録し、制御用には最初の参照だけを返す。Ledger より前で独立 failure を join しない。
5. **[P2] representative event の global cap を超えられた。** `GroupSamples == 0` の class 初回経路でも代表 event が `Events` cap を越えて append されうる抜けがあった。是正は work/cleanup を区別せず hard cap を最初に評価し、sample を持たない class も count・identity・truncation を保持すること。
6. **[P2] overflow の `Classes` が distinct class 数ではなく occurrence 数だった。** 畳み込んだ class を記録していなかったため、同じ class の再出現も毎回 unseen と判定していた。是正は bounded set で正確に数え、それを超えたら `ClassesTruncated` で lower bound であることを明示すること。固定 memory で無制限の distinct 数を正確に数えることはできないため、正確なふりをしない。

回帰は 6 件それぞれに直接再現 test を置き（`internal/journal/boundary_test.go`、`internal/run/independent_test.go`）、各是正の要を外すと対応 test が確実に落ちることを負のコントロールで確認して復元した。matrix には report × Span Open/End の交差、pure Cause × Cause を含む joined error、budget の 0/1/global cap 到達後、同一 chain・fan-out 内の複数独立 Flush failure を追加した。

### R-15 [P1/P2] foundation の境界 hardening

**Historical status (superseded): resolved in 2026-08-17 review 11**

R-13/R-14 の failure evidence を土台に、M0〜M6 の foundation を現実的な第三者 plugin の誤りで再監査した。対象は callback が一度 panic する、同じ sentinel を独立に返す、capability を宣言どおり実装しない、child cleanup の一つが失敗する、payload を二重に release する、といった accidental bug である。意図的な無限 `Unwrap`/`Error`、永久 block、Host が登録していない goroutine、raw schema callback の直接呼出しは in-process plugin contract の外に残す。

1. **[P1] public Reference が canonical locator を表示していた。** opaque target、authority、path、userinfo、query、fragment のいずれも `Display`/`String`/formatting から出さず、scheme と固定の `<redacted>` marker だけを公開する。resolver 用 `Canonical` と public fingerprint を分け、redacted display を identity に使わない。`access/reference_test.go` の全 target 形状と `access/access_test.go` の canonical/display 分離が根拠である。
2. **[P2] rational rescale の nearest が奇数 denominator の半分を誤判定しうる。** 128-bit の比較で `2*remainder` と denominator を厳密に比べ、奇数 denominator では exact tie が存在しないことを明示した。`media/timing/time_test.go:TestRescaleNearestEvenUsesExactHalfForOddDenominators` が 1/3、2/3、2/5、3/5、44100→1000 と負値を固定する。
3. **[P1] map key canonical の衝突を順序で解決していた。** 異なる key が同じ canonical bytes を返した場合は `errMapCanonicalCollision` とし、key callback は一 key 一回、iteration order に依存しない error を返す。`config/collection_test.go:TestMapCanonicalEvaluatesEachKeyOnceAndRejectsCollisions` と panic/error propagation test が根拠である。これは誤る custom codec を検出する境界であり、malicious nondeterministic callback を正規化しない。
4. **[P1] Access の Snapshot/Read/Equivalent callback panic が lifecycle を壊しうる。** Prepare の acquire/capability/snapshot、run 前後の Snapshot、probe Read/ReadAt、sink Equivalent は Host の callback boundary で stable failure へ投影し、raw panic value を表示せず、取得済み session/lease/output を閉じて後続 cleanup を続ける。`host/access_panic_test.go`（Snapshot 各境界、initial close、probe lease）と `host/conflict_test.go:TestBoundaryEquivalencePanicAndErrorStaySafe` が根拠である。
5. **[P1] composite cleanup が最初の panic で sibling を飛ばしていた。** direct input/output、spool transaction/storage、replay prefix/underlying の各 child を一度ずつ試行し、panic/error を stable aggregate にし、二回目の Close は同じ結果を返す。`host/direct_cleanup_test.go`、`host/spool_test.go:TestSpoolCleanupAttemptsEveryChildAfterPanic`、`host/replay_cleanup_test.go` が全 child 試行と exact-once を固定する。in-process callback が永久 block する場合だけ cleanup bound が終了を保証できない。
6. **[P1] third-party error graph の unsafe inspection と Ledger の mutable provenance escape。** Host の `errors.As`/`diagnostic.ItemsOf` 再解釈をやめ、bounded `internal/errorx` の panic-safe traversal と journal の metadata を唯一の source にした。panic/cyclic unwrap は opaque occurrence として保持し、`Cause` を含む joined branch の独立 failure を消さない。`host/failure_safe_test.go`、`internal/errorx/errorx_test.go`、`internal/journal/boundary_test.go` と `ledger_race_test.go`、および journal/host/run の `-race` gate が根拠である。
7. **[P1] active cancellation を idle/未実行の shortcut で検査していた。** public testkit は callback が実際に Run へ入り context cancellation を観測するまで待ち、active Run を cancel して全経路の join/cleanup を通す。成功・期待 failure・rejected emit の実行も `host.VerifyOwnership()` を opt-in で有効にする。`testkit/runner.go` の active gate と `internal/run/cancel_test.go:TestAnExternalCancellationIsReportedOnce` が根拠である。
8. **[P2] ownership audit が常時 hot path に載るか、未検査の leak を成功と見なしていた。** `VerifyOwnership()` が要求された Run だけ Ledger の run-local tracker を有効にし、persistent owner、queue/fan-out move、overrelease、Close 後の live slot を Resource/Cleanup failure として投影する。`host/ownership_audit_test.go` の四ケースが根拠である。`flow.Item` は `audited bool` を一つ cache し、無効時は tracker/atomic/allocation を持たない。plugin が raw payload に schema callback を直接呼ぶ、`unsafe` で Item を複製する、Host に登録しない goroutine を残す場合は audit の観測対象ではなく plugin bug とする。

この review の共通境界は「誤る callback は局所化して続行し、Host が報告できる failure として元の node/phase/kind/identity を保つ」ことである。第三者 code の永久計算・永久待機・契約外の所有権操作を process 内で強制停止または推測して補償する設計にはしない。

### R-16 [P1/P2] cross-boundary cleanup と provenance の再確認

**Historical status (superseded): resolved in 2026-08-17 review 12**

R-15 の修正後、準備済み job、composite session、direct Resource、spool storage、boundary equivalence の間を再確認した。対象は一度 panic/error を返す現実的な provider bug であり、永久 block や悪意ある無限 error graph は従来どおり contract 外である。

1. **[P1] Prepared.Close の並行呼出しが cleanup を二重実行し、結果を失う余地があった。** `preparedClosing` の barrier と `releaseResources` の memoized `sync.Once` を組み合わせ、後続の Close は先行 cleanup の完了を待って同じ error snapshot を返す。`host/direct_cleanup_test.go:TestConcurrentPreparedCloseWaitsForOneMemoizedCleanup` は direct child の callback が一度だけ呼ばれ、二つの caller が同じ failure を受け取ることを固定する。
2. **[P1] composite child task の未 join 状態を親の停止理由と混同していた。** `task.Report.Running` の child name と `Join` phase を Host がそのまま記録し、期限内に止まらなかった task を Primary や「停止済み」として偽らない。`host/run_test.go:TestPreparedRunReportsUnjoinedPluginTaskWithoutClaimingItStopped` と `host/failure_test.go:TestJoinReportsUnstoppedTasksAsCleanupDuringCleanup` が child provenance、Cleanup 分類、Wait error の分離を検査する。
3. **[P1] access.Resource の recovered Close panic が通常の cleanup error へ降格し得た。** `Resource` は stable `ErrResourceClosePanic` と panic stack を private marker として一度だけ保持し、Ledger がその marker のみを `CleanupPanic` へ投影する。`host/direct_cleanup_test.go:TestDirectResourcePanicProjectsAsCleanupPanicUnderZeroSampleBudget` は `Events=0` でも stack、error identity、CleanupPanic kind が残ることを固定し、raw panic value は表示しない。
4. **[P1] spool の storage panic/error で sibling transaction の cleanup が抜け、storage Close が retry され得た。** storage を callback 前に detach して exact-once を決め、Abort/Close は underlying transaction/session と storage の全 child を独立に試行する。`host/spool_test.go:TestSpoolCleanupAttemptsEveryChildAfterPanic`、`TestSpoolStorageClosesOnceAfterSuccessfulFlush`、`TestSpoolCloseMixedFailuresKeepOccurrenceProvenance` は panic 後の sibling 継続、Flush 後の二重 Close 防止、`spool/storage-close` と `access/session` の node/phase/task/kind/stack を検査する。
5. **[P1] Equivalent callback panic の provenance が ordinary error と混ざり得た。** callback boundary は panic を structured `access/equivalence` failure として output node に結び、stack を保持する一方、通常の callback error に panic provenance を付けない。`host/conflict_test.go:TestBoundaryEquivalencePanicAndErrorStaySafe` は raw secret/canonical locator の漏えいがなく、panic の node/task/stack と ordinary error の無 stack、未 acquire・未 commit の境界を固定する。

五件に共通するのは、child の error/panic を一つの wrapper へ畳まず、Host が返す `Result` の `Primary`/`Secondary`/`Cleanup` と ledger EventID に元の node/phase/kind/identity を保つことである。callback が永久に戻らない、Host に登録しない goroutine を残す、raw schema callback を直接呼ぶ場合は testkit の補償範囲ではなく plugin bug とする。

### R-17 [P1/P2] current contract synchronization

**状態: accepted current contract; final verification complete 2026-08-17**

R-17 が現在の normative review であり、R-01〜R-16 の historical completion claim を置き換える。production code と regression tests が現在受理する境界は次のとおりである。

1. **private ownership と panic provenance。** `flow.Item` は一つの private ownership slot と `noCopy` marker を持つ。audit の opt-in は `internal/ownership.Wrap` が付ける unexported `track` face だけで、第三者の公開 `flow.Reporter` 実装が tracker へ紛れ込まない。panic の証拠も任意の `StackTrace` ではなく `internal/errorx.MarkPanic` の private marker を `errorx.RecoveredPanic` が認識する。`flow.ReleaseError`、`journal.PanicError`、observe の panic error は recovered value を保持せず、安全な要約と stack だけを持つ。`testkit.Coverage.VerifyExecutable` は executable component の typed Run、`HasSuggest` の Suggest scenario、Set 外 identity を検査し、planning-only failure を実行済みとは数えない。実行可能 scenario は `host.VerifyOwnership()` を付けた `Prepared.Run` を通る。
2. **実能力としての StableSize。** `StableSize` の宣言だけでは足りず、acquire した session が `access.Sizer` と `access.Snapshotter` を実装し、valid な `WeakSnapshot`/`StrongSnapshot` identity を返すことを Host が確認する。automatic Probe にも同じ検査を適用し、`NoSnapshot`、空 identity、nature/identity の弱化・変化を拒否する。local file の size/mtime は `WeakSnapshot` であり、読み出しは acquired size で clamp する。範囲外 `(0, io.EOF)` は StableSize evidence がある時だけ exact end となる。`host.replaySession` は source の capabilities、size、snapshot fields を同期的に委譲し、prefix と underlying を各一度だけ close する。
3. **Queue termination と phase context。** `queue.Seal` は queued item を drain して EOF/下流 Flush を許す成功終端、`queue.Abort` は停止終端であり、未終端 queue の `Pop` は EOF ではなく context cause（cause 無しなら `ErrAbandoned`）を返す。正常 close は phase context を記録してから Seal する。caller の external cancel、Run/Finalize failure、準備前の停止は phase context も止めて Flush を抑止する一方、peer component の Flush failure は job context を止めても phase context を止めず、準備済み sibling の Flush failures を独立に collect する。buffer の `Abandon` は non-quiescent barrier を failure とする既存 contract を保つ。
4. **file boundary。** `plugin/file` の `ioBoundaryError`/`redactIO` は `os.PathError`/`os.LinkError` の path を Error/Format/As から隠し、`errors.Is` の OS identity を保つ。file `equivalent` は filesystem access より先に `context.Cause` を確認し、Equivalent panic は `access/equivalence` の structured failure として stack/provenance を残すが、通常 error に panic provenance を混ぜず、canonical locator や path を表示しない。
5. **WAVE INFO edit-aware preservation。** `plugin/wave` の `parseInfoCarrier`/`reencodeInfoCarrier` は未変更 document の carrier bytes をそのまま返し、編集時も unchanged known child raw、padding、order、unknown child `metadata.RawBlock` を保持し、changed/removed semantic child だけを再 encode/remove する。`matchInfoEntries`/`matchInfoSubsequence` は duplicate native entries を origin/value と document order で deterministic に対応付ける。完全に同一の native・value・origin duplicate は per-occurrence identity を持たないため byte-level 個体識別をしない、という deterministic limit を明示する。`TestRIFFInfoEncodingMatchesDuplicateNativeEntriesByOriginAndValue`、`TestRIFFInfoEncodingRespectsDuplicateDocumentOrder`、`TestRIFFInfoEncodingTracksUnknownChildBlockEdits`、foreign/malformed child rejection tests がこの境界を固定する。
6. **failure evidence と cancellation projection。** observer の `Fail` が Ledger への唯一の failure ingress であり、`Close` は bounded wait と cleanup 完了を担うだけで証拠を別経路へ送らない。Span は単一の `stopping` provenance を持ち、Cleanup を先に見た場合だけ Work が置き換え、Domain/Site の node attribution は bind/open 時に固定した Site/Home へ帰属する。mutable node、`Enter`/`Leave`、`Span.node` は持たない。Omitted Cause は Kind/Operation/Task/Node と immutable shared depot Stack snapshot を含めて echo を再構成し、occurrence や budget を増やさない。`internal/cancel.Normalize` は停止済み context と pure single unwrap chain の trusted Cause だけを返し、joined/独立 provider error は保持する。CLI `ExitCanceled` の authority は caller context state のみで、live plugin sentinel は `ExitRuntime` とする。
7. **phase と operation の対応。** caller に Link された phase child と、phase child から作る job child を分ける。Flush failure は job child だけを止めて peer Flush を継続し、non-Flush failure は phase child を先に止めて delayed Flush を抑止する。`Operation` と public `Phase` は一つの対応表で射影し、coarse overflow は `UnknownPhase` として Run に偽装しない。

R-17 の contract と current repository-wide gate は 2026-08-17 に完了した。独立 Terra review でも新たな現実的 P1/P2 は確認されず、三層 Ledger/Domain/Span、caller/job の 2 context、`Seal`/`Abort` の queue state は異なる責務として維持する。

最終 verification は次のとおりである。

```text
go build ./...
go test ./... -count=1
integration: go test ./... -count=2
go test -race ./... -count=1
integration: go test -race ./... -count=2
go vet ./...
go run ./tools/cmd/test-runner --simd
go run ./tools/cmd/generate  (status unchanged)
go run ./tools/cmd/docs-check
tools: GOWORK=off go build ./...
non-_legacy gofmt -l: clean
git diff --check
```

代表 benchmark は Item 0 alloc、fused 43 alloc、Host 約 65,722 alloc で従来同等であり、明白な 2 倍回帰は確認されなかった。

## 計画そのものへの指摘（historical review guidance）

### 1. 「test green」と「contract成立」を別の gate にする

初回監査時点の full suite は green と記録されていたが、R-01、R-02、R-04、R-08 は数行の公開 API probe で再現できた。完了条件の文章が存在するだけでは不十分で、各 normative statement を assertion または明示的な未 cover assignment へ対応付ける必要がある。この原則と 2026-08-17 の final gate は R-17 の verification 記録へ反映した。

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

本監査に対する M6 は、R-01〜R-17 の全 final review findings と final gates を満たしたため、2026-08-17 に再完了した。R-17 の current contract、実装、negative regression test、repository-wide verification、独立 Terra no-finding が同日の記録で確定している。

- R-01〜R-17 の各 finding が、実装・production consumer・該当する negative regression test（または accepted contract test）へ対応付けられている。R-01〜R-16 の status/date は historical traceability であり、R-17 の current contract と矛盾しない。
- private ownership/panic marker、`testkit.Coverage.VerifyExecutable` の executable typed Run/Suggest/foreign identity 検査、planning-only shortcut の除外が current source/test と一致する。
- `StableSize` が実能力として `access.Sizer` + valid `access.Snapshotter` identity を要求し、automatic Probe、WeakSnapshot、probe EOF、replay field synchronization を含む phase 間 snapshot semantics が文書と capability 宣言で一致する。
- `queue.Seal` と `queue.Abort` の終端差、external cancel による Flush 抑止、peer Flush failure の独立収集、`Abandon` 後の non-quiescent barrier failure が runtime の phase context と回帰 test で一致する。
- file の `ioBoundaryError`/`redactIO` path redaction と `errors.Is` identity、file `equivalent` の `context.Cause`、Equivalent panic/ordinary error provenance 分離が current tests で固定されている。
- WAVE INFO の未変更 raw exact、edit-aware known/unknown child raw、padding/order、deterministic duplicate matching/limit、malformed/foreign child failure が unit と integration の vector で通る。完全同一 duplicate の per-occurrence byte identity 不在は明記済みの限界として扱う。
- `checkpoint.md` の進捗・ownership 注記と production public export が一致し、review 用の temporary probe/source が tree に残っていない。
- final gates（root/integration の build、test、race、vet、`go run ./tools/cmd/test-runner --simd`、generator、tools build、`go run ./tools/cmd/docs-check`、non-`_legacy` gofmt、`git diff --check`、integration conformance）が 2026-08-17 に実行され、R-17 の verification block に結果付きで記録されている。

是正で確定した product 判断は、それぞれの正本へ移した。schema-bound な typed patch entry と phase ごとの config snapshot は [config](config.md#完全値と疎な-patch)、reservation slot `JUNK` の再利用と RF64 昇格時の loss は [media](media.md#m6-完了条件) と [capability](capability.md) の B8、local file の weak snapshot と phase 間検証は [access](access.md#snapshotretry再現性) と B9、coverage owner を実在 milestone に限る規則は [scope](scope.md#m6-の-contract-分類)、`cli.Run` の structured result は [experience](experience.md) を正本とする。
