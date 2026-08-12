# M6 修正指示書: 実装 review 指摘の是正

M0〜M6 の通し review で見つかった欠陥の是正指示である。スコープは既存 package の contract 是正と
文書の同期に限り、M7 の成果物（MP4、multi-stream、mapping、seek、loss report）には着手しない。

review 時点の状態: `go run ./tools/cmd/test-runner --simd` exit 0、`go vet ./...` clean、`gofmt -l` 0 件、
`docs-check` exit 0、`git status` clean。実機の WAVE→WAVE / WAVE→raw→WAVE は payload byte 一致、
未知 chunk（fmt 前・中間・data 後）と重複 `IART` を含む LIST/INFO は順序・padding 込みで完全復元、
truncated 入力は output abort と temporary file 非残留を確認済み。**以下は回帰ではなく contract の欠陥である。**

未 release のため、public API の rename・削除・signature 変更を互換のために避けない。旧名の alias、
deprecated wrapper、移行 shim を残さないこと（[AGENTS.md](../../../AGENTS.md) の互換性規則、[C12](../decisions.md)）。

## 必読

[AGENTS.md](../../../AGENTS.md)、[refactor.md](../../refactor.md#実装ロードマップ) の粒度規則、
[access.md](../access.md#m6-完了条件)、[media.md](../media.md#m6-完了条件)、
[scope.md](../scope.md#m6-の-contract-分類)、[experience.md](../experience.md#m6-完了条件)、
[capability.md](../capability.md#挙動変更の記録)、[findings.md](../findings.md) の F18。

## 作業単位

1〜3 は利用者に見える不具合、4〜7 は第三者開発者向け contract の正確さ、8〜11 は planner/surface の
仕上げ、12 は文書同期である。1〜3 と 12 は独立に着手できる。4〜7 は `access` の公開 API を触るため
一つの単位として通すこと。

---

## 1. CLI が正常終了のたびに event を失い warning を出す

[cli/cli.go](../../../cli/cli.go) の `Run` は `host.Observe(ObservationDetailed, host.DeliverEvents(8, renderer))`
を渡し、`RetainEvents` を設定していない。17 KB の WAVE 変換で 16 event が発生するため、容量 8 の
non-blocking queue が dispatcher の排出より先に溢れ、[internal/observe/collector.go](../../../internal/observe/collector.go)
の `append` が `default:` 分岐で drop する。実測では 5 回すべてで `deliveryDropped=1` になり、

```
lifecycle sequence=8 node=auto-...muxerID phase=open state=start
lifecycle sequence=10 ...                          ← 9（open complete）が欠落
warning observation-loss history=0 delivery=1
```

となる。history を持たないため落ちた event は回復不能である。「溢れたら media を待たせず drop して
件数を報告する」という機構自体は [scope.md](../scope.md#m6-の-contract-分類) の決定どおり正しい。
欠陥は **CLI が選んだ上限と history 不在** であり、happy path に warning を出している点である。

- CLI は live delivery と bounded history の両方を設定する。通常規模の変換で live 表示が欠けないこと、
  かつ欠けた場合でも `Result.Events` から復元できることを両立させる。
- 実行終了時に、live 表示できなかった event を `Result.Events` の `Sequence` 欠番から補って表示する。
  表示順は sequence 昇順とし、live 表示済みの event を二重に出さない。
- `warning observation-loss` は**情報が実際に失われた時だけ**出す。history が保持していて事後表示で
  補完できた drop は warning にしない。history も溢れた場合、または history を持たない場合は従来どおり出す。
- media path へ backpressure をかける形にしない。dispatcher が stderr へ blocking write することは
  media 経路の外なので許容する。
- test: 小規模 WAVE→WAVE 変換の stderr に `observation-loss` が 0 行で、lifecycle/progress の
  `sequence` が欠番なく連続すること。history を意図的に溢れさせた場合だけ warning が出ること。

## 2. 出力 file の permission と durability

[plugin/file/sink_session.go](../../../plugin/file/sink_session.go) の `acquireSink` は `os.CreateTemp` を使う。
`os.CreateTemp` は mode 0600 固定であり、chmod せずに `Commit` の `os.Rename` で置換するため、
POSIX では変換結果が所有者しか読めない。既存の 0644 target を置換すると 0600 へ**格下げ**される。
[access.md](../access.md#sink-transaction) は permission preservation と directory sync を file Provider の責務と
明記しており、[capability.md](../capability.md#挙動変更の記録) の B5 は Windows の ACL/attribute 差分しか
記録していない。

- temporary file を、通常の file 作成と同じ permission で作る。`os.CreateTemp` の 0600 固定を使わず、
  `os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o666)` 相当で process umask が効く形にする。
  名前の一意化は `plugin/file` 内に閉じ、既存の `"." + base + ".godec-*"` の pattern と同一 directory の
  制約を維持する。
- target が既に存在する場合は、commit 前に temporary の permission を target の既存 permission bit へ合わせる。
  target が存在しない場合は umask 由来の既定に任せる。
- rename 成功後、親 directory の durability を確保する。Windows で `os.Open(dir)` が失敗する経路を
  壊さないこと。stdlib だけで実装し、`golang.org/x/sys` を root module へ持ち込まない。
- test: 既存 target（0644 相当）を置換した後の mode が 0600 にならないこと。新規作成時の mode が
  同じ directory に `os.Create` で作った file と一致すること。Windows でも既存 test が green であること。
- [capability.md](../capability.md#挙動変更の記録) の B5 を、実装した permission 規則を含む形へ更新する。
  ACL/attribute を継承しない点は据え置き。

## 3. WAVE 出力に付く 36 byte の `JUNK` が記録されていない

RF64 のための `ds64` 予約により、全 WAVE 出力の先頭に `JUNK`(8+28) が入る。実測 17684 → 17720 byte。
これは [media.md](../media.md#m6-完了条件) が要求した設計どおりの挙動であり、実装を変えない。
記録が無いことだけが欠陥である。

- [capability.md](../capability.md#挙動変更の記録) に行を追加する。「payload と全 chunk は exact に復元するが、
  file の byte 一致は保証しない」ことと理由（header 長を固定して後追い patch を成立させる）を書く。
  担当は M6、[media.md](../media.md#m6-完了条件) の該当条項を参照する。

---

## 4. `access.AnyOf` の名前が意味を反転させている

[access/capability.go](../../../access/capability.go) の `AnyOf` は 1 個の `Alternative` を構築し、`Select` は
alternative 内の**全 capability** を要求する。OR は `Requirements.Alternatives` 側にある。したがって
[plugin/pcm/linear/definition.go](../../../plugin/pcm/linear/definition.go) の
`access.AnyOf(access.RandomRead, access.StableSize)` は「random read **かつ** stable size」を意味する。
第三者 Provider/Format 作者が最初に触る API で、名前が読みと逆である。

- `AnyOf` を `AllOf` に rename する。`Alternative` は AND 集合、`Requirements.Alternatives` は OR の列である
  ことが名前から読めるようにし、両者の godoc に一行ずつ明記する。
- 呼び出し側 26 file・71 箇所をすべて更新する。alias を残さない。
- [access.md](../access.md#source-capability) の該当記述と [access/example_test.go](../../../access/example_test.go)
  の `ExampleNewRequirements` / `ExampleSelect` を更新する。Example は plugin 作者が最初に読む正本なので、
  AND と OR の対比が一目で分かる形にする。

## 5. `format.Read` と `format.Write` の非対称

[media/format/trait.go](../../../media/format/trait.go) は `Read(value, requirements access.Requirements, ...ReadOption)`
と `Write(value, alternatives ...access.Alternative)` で、同じ概念を別の shape で受ける。`Read` は
`ReadOption` の可変長を既に持つため alternatives を可変長にできない。

- `Write` を `Write(value Format, requirements access.Requirements, ...)` へ揃える。将来 `WriteOption` を
  足せる形にしておく。
- 呼び出し側（[plugin/wave/mux_component.go](../../../plugin/wave/mux_component.go)、
  [plugin/pcm/linear/definition.go](../../../plugin/pcm/linear/definition.go)、`integration/acme`、test）を更新する。

## 6. 守れない capability と view を持たない capability

[plugin/file/source_session.go](../../../plugin/file/source_session.go) は `Reopen` と `CancelableRead` を宣言するが、

- `Reopen`: `access` に reopen 操作は存在せず、この capability を読む code も存在しない。
- `CancelableRead`: `Read`/`ReadAt` は blocking syscall の前後で ctx を見るだけで、blocked read は cancel でも
  Close でも解除されない。[access.md](../access.md#cancellation-と-ownership) の
  「Close が blocked read/write を解除できる Provider はその保証を contract に含める」に反する。
- `ConcurrentRead`: 定義のみで宣言も consumer も 0 件。

[media.md](../media.md#key-機構は一つ容器は三つkey-型は二つ) が避けようとした「false capability を後から
発見する」失敗そのものである。

- `Reopen`、`ConcurrentRead`、`CancelableRead` を `access.Capability` から削除する。
  [access/capability.go](../../../access/capability.go) の `capabilitiesValidFor` の列挙、
  `plugin/file`、`integration/acme`、testkit、test の宣言をすべて更新する。
- 削除した 3 つと、それぞれを再導入する条件（`Reopen` と `ConcurrentRead` は remote Provider、
  `CancelableRead` は実際に blocked I/O を解除できる Provider）を
  [scope.md](../scope.md#m6-の-contract-分類) へ担当 milestone 付きで記録する。宣言だけを残さない。

## 7. `StableSize` に操作 view が無く、truncation が Run まで検出されない

[access/view.go](../../../access/view.go) の `viewsFor` は `StableSize` を「semantic capability だから view を
持たない」として扱う。そのため `linear` reader が `AllOf(RandomRead, StableSize)` を要求しても size を
実際に問い合わせる経路が無く、要求は inert である。[plugin/wave/component.go](../../../plugin/wave/component.go)
の read trait は `RandomRead` だけを要求し、`inspectHeaderWithMetadata` が `data` の宣言 size を実 file size と
比較できない。実測では 17 KB の WAVE を 200 byte へ切り詰めると、output transaction を開始した後の
`host.run: WAVE data chunk is truncated` になる。

[access.md](../access.md#source-capability) の設計例は「WAVE inspect: sequential OR random + stable size」であり、
size を使う前提になっている。6 と同じ理由で、**view を与えて実際に使うか、capability を消すか**の
どちらかにする。前者を採る。

- `access` に `StableSize` の narrow view（size を返す context-aware な操作）を足し、`viewsFor` と
  `Opening.Valid()` の capability→view 対応へ組み込む。`Selected()` に `StableSize` が含まれるのに view を
  取得できない session は、Open 後の type assertion ではなく Prepare の構造化 diagnostic にする
  （既存の `prepare.access-view` 経路に合わせる）。
- [plugin/file](../../../plugin/file) の source session が実装する。
- WAVE の read trait の alternative を `AllOf(RandomRead, StableSize)` にし、`inspectHeaderWithMetadata` が
  `data` chunk の終端と RIFF 宣言 size を実 size と突き合わせて、truncation を Prepare の diagnostic にする。
  RF64 の `ds64` 経路も同じ検査に載せる。
- test: truncated WAVE が `Prepare` で失敗し、output session が acquire されないこと（temporary file が
  一つも作られないこと）。正常な WAVE が従来どおり通ること。growing/live source を将来扱う余地を
  塞がないよう、`StableSize` を持たない source では検査を skip すること。

> **判断が必要になった場合は止めて確認する。** view の signature（size のみか、EOF 意味を含むか）が
> M7 の growing/live source と衝突すると判断した場合、独断で広げず [AGENTS.md](../../../AGENTS.md) の
> 規則どおり指示を仰ぐ。

## 8. `access.Factory` に consumer が無い

[access/access.go](../../../access/access.go) の `Factory` / `SessionFactory[T]` は package 外の参照が
`access/access_test.go` だけである。一方 [scope.md](../scope.md#m6-の-contract-分類) は
「`Own`/`Borrow`/`Factory` … いずれも WAVE Format または local file Provider が実 consumer になる」と
書いており、事実と食い違う。[refactor.md](../../refactor.md#実装ロードマップ) の
「consumer を持たない export を残さない」に反する。

- `Factory` と `SessionFactory[T]` を削除する。`Own`/`Borrow`/`Resource[T]`/`Direct[T]` は `job` と testkit に
  consumer があるので残す。
- [access.md](../access.md#cancellation-と-ownership) の `access.Factory(f)` の記述を削除する。
- [scope.md](../scope.md#m6-の-contract-分類) の M6 分類から `Factory` を外し、再導入するなら
  stdin/stdout を扱う M9 が担当であることを記録する。

---

## 9. planner が入れた config が Plan 上で `explicit` と表示される

[config/resolved.go](../../../config/resolved.go) の `Source` は default/preset/explicit/normalized の 4 値で、
planner を表す値が無い。[internal/solve/candidate.go](../../../internal/solve/candidate.go) が bridge 候補用に
組み立てた patch も `explicit` になるため、実測の Plan は

```
node auto-... component=...linear.parserID origin=automatic reason=graph.schema-mismatch
  config rate=44100 source=explicit        ← 利用者は何も指定していない
```

と表示する。node 行の `origin=automatic` から追えるとはいえ、field 単位の provenance が誤解を招く。
[planner.md](../planner.md) の「説明可能な Plan」に対する穴である。

- `config.Source` に planner 由来を表す値を足す。`internal/solve` が合成した patch と、Host が自動挿入
  node へ渡す patch がその値になるようにする。利用者が `job.Node` へ渡した config は `explicit` のままとする。
- [cli/render.go](../../../cli/render.go) の表示と [plan/model.go](../../../plan/model.go) の `Config` projection を
  更新する。
- **provenance は表示 metadata であり、fingerprint に影響させない。** 同じ Job から作った Plan の
  fingerprint が この変更の前後で変わらないこと、および provenance だけが異なる 2 つの解決結果が同じ
  fingerprint になることを test で固定する。

## 10. 既定の copy/remux が graph 形状の副産物で pin されていない

実測した WAVE→WAVE の plan は `source → wave demux → linear parser → wave mux → sink` で、decoder/encoder を
開かない。[C4](../decisions.md) と [capability.md](../capability.md#挙動変更の記録) の B1 が目指す挙動だが、
これは policy ではなく「muxer が `codec.Packets()` を受けるので solver が最短経路を選んだ」結果である。
`integration/`・`cli/`・`standard/`・`host/` に、この plan 形状を検査する test は無い。cost model や
component が変われば黙って decode を始め、M7 の B1 が「追加」ではなく「復元」になる。

- `integration` に、無指定の WAVE→WAVE 変換の `Plan` が decoder/encoder component を含まないことを
  検査する test を足す。node identity で検査し、node 数のような脆い条件にしない。
- B1 は M7 担当のまま変えない。この test は「M6 時点で偶然成立している性質を、M7 が policy として
  引き取るまで壊さない」ための固定である。その意図を test の doc comment に一行で残す。

## 11. `standard` から policy と budget を渡せない

[standard/job.go](../../../standard/job.go) の `FileJobOption` は Format 系だけで、`job.WithPolicy` /
`job.WithBudget` へ到達できない。`Realtime` preset や probe budget を変えたい利用者は `job.New` へ降りて
file 正規化を書き直すことになり、[experience.md](../experience.md#m6-完了条件) の
「2 段目へ連続的に移行できる（1 段目で書いた code を捨てない）」が破れる。

- `standard` に policy と budget を渡す `FileJobOption` を足し、`surface.FileJob` の signature を対応させる。
- [job/policy.go](../../../job/policy.go) の `PolicyFor` はどの preset でも `AllowSpool` を立てない。これは
  M6 の唯一の sink が random write を持つため正しい既定なので**変えない**。ただし
  「spool は `job.ResourcePolicy` を明示した場合だけ有効で、preset からは有効にならない」ことを
  [scope.md](../scope.md#m6-の-contract-分類) へ記録し、M7 の MP4 fragmented/spool が再発見しないようにする。
- test: `standard.NewFileJob` に `Realtime` policy を渡した Job が Host を通り、Plan の
  `EffectivePolicy` に反映されること。

### 軽微

- [media/buffer/buffer.go](../../../media/buffer/buffer.go) の `Allocate` は `rawSize` を課金するが
  `allocateStorage` は `rawSize+alignment-1` を確保する。grant を exact にするか、差分を godoc に明記する。
- [host/session.go](../../../host/session.go) の `acquireSessions` は output boundary でも一度
  `entry.SourceTrait().Capabilities()` を呼んでから捨てている。さらに loop 内の `direction` が引数
  `direction plan.BoundaryDirection` を shadow している。両方を解消する。

---

## 12. 計画文書の同期

- **[findings.md](../findings.md) の F18。** 現在も「**一部完了** … session acquire、共有 probe cache、
  実 capability 再検証、spool consumer は M6」だが、これらは M6 で全部入った。findings.md 自身の規則
  （担当 milestone と詳細資料の完了条件を満たしたら完了扱い）に従って更新する。
- **`docs/legacy/` の処遇。** 10 文書が [refactor.md](../../refactor.md#文書の終端) の移行文書にも
  設計文書にも列挙されておらず、担当 milestone を持たない。`plugin-system.md` と `packages.md` は
  M5 で削除した `registry.DemuxerManifest`、`node.Demuxer`、global registry を今も説明している
  （"registry" 出現 25 / 27 件）。削除するか、担当 milestone を決めて refactor.md の
  「文書の終端」へ第三の分類として追加する。**放置しない。**
- **[checkpoint.md](../checkpoint.md) の更新。** この修正単位の完了を「現在の注記」へ一項目で書く。
  完了済み作業の長い列挙は残さない（checkpoint の更新規則）。
- **M7 着手前監査を次の作業として明記する。** M7 は MP4 (ISO BMFF) + multi-stream + mapping +
  stream copy + loss report + seek + `QueuePolicy.Window` の分離を一括で抱えており、現時点で最大の
  未分解 milestone である。M6 は着手前監査で contract の断絶が 3 件見つかり 9 単位（M6-0〜M6-5b）へ
  割った経緯がある。[refactor.md](../../refactor.md#実装ロードマップ) の「完了条件は着手前に書く」
  「機構を作る milestone は実 consumer を同じ milestone 内に持つ」「各単位が端から端まで green の実行
  経路を残す」がそのまま適用される。**この監査と sub-unit 分割は本指示書のスコープ外**であり、
  checkpoint に次の作業として記録するだけでよい。

## 非スコープ

MP4、multi-stream、mapping、seek、metadata loss report、`metadata.Mapping` の適用（すべて M7）。
MP3/FLAC/audio filter と variant selection（M8）。stdin/stdout、WASM、demo web、残りの CLI flag、
device/session Endpoint（M9）。conformance testkit の完成と root CI（M10）。`_legacy/` の削除（M8）。

## 検証

```bash
go test ./access/ ./config/ ./job/ ./media/buffer/ ./media/format/ ./internal/bind/ ./internal/solve/ ./host/ ./plugin/file/ ./plugin/pcm/linear/ ./plugin/wave/ ./standard/ ./cli/ ./testkit/ -race
go vet ./...
go test ./... -race                         # integration module 内
go build ./...
go run ./tools/cmd/generate
go run ./tools/cmd/docs-check
go run ./tools/cmd/test-runner --simd
gofmt -l .
```

加えて、次を実機で確認する（review 時に手で実行した経路である。回帰していないことを確認する）。

```bash
go run ./cmd/godec in.wav out.wav                 # payload byte 一致、warning 0 行、sequence 連続
go run ./cmd/godec --plan in.wav out.raw          # 出力を作らない
go run ./cmd/godec in.wav in.wav                  # file.same-path、exit 2
go run ./cmd/godec in.wav out.raw                 # raw 出力
go run ./cmd/godec out.raw back.wav               # prepare.format-config-required、exit 3
go run ./cmd/godec --raw-rate 44100 --raw-valid-bits 16 --raw-layout stereo --raw-endian little out.raw back.wav
```

未知 chunk（fmt 前・中間・data 後）と重複 `IART` を含む LIST/INFO を持つ WAVE の roundtrip で、
chunk の順序・padding・byte 列が復元されること。truncated WAVE が **Prepare** で失敗し temporary file を
残さないこと（7 の変更後）。

**完了 gate:** 公式 CLI が正常変換で observation warning を出さず event の欠番も無い。POSIX の出力 file が
通常の permission で作られる。`access` の capability と Requirements API が、宣言した内容を実際に
提供・消費できるものだけで構成されている。consumer を持たない export が `access` に残っていない。
Plan の config provenance が planner 由来を区別する。WAVE→WAVE が decoder/encoder を開かないことが
test で固定されている。findings/capability/scope/checkpoint が実装と一致している。
M6 の全 completion condition と repository-wide gate は green のままである。
