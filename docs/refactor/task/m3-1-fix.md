# M3-1 修正指示書: 受け入れ条件未達と設計上の欠陥の是正

M3-1 の review で、[m3-1.md](m3-1.md) の受け入れ条件を満たしていない箇所と、M3-2 が同じ形を踏襲すると傷が広がる箇所が見つかった。この文書はその是正だけを扱う。

前提、必読、スコープ境界、中断条件は [m3-1.md](m3-1.md) と同じである。`core`、`sdk`、`plugin/<family>`、`cli`、`bindings`、`example` の production code には触れない。

項目 1〜5 は受け入れ条件そのものが未達である。6〜10 は M3-2 の metadata model が直接乗る土台なので、M3-2 より先に直す。11〜14 は整理である。

## 作業順序

項目番号は深刻度順であり、依存順ではない。次の順で進める。単位ごとに独立して commit できる粒度を保つ。

```text
2  -> 3  -> 9  -> 10 -> 6  -> 4  -> 5  -> 7  -> 8  -> 1  -> 11 -> 12 -> 13 -> 14
```

項目 15 と 16 は 1〜14 の review 後に追加した。16 は `flow.Port` の形を変えるため 15 より先に行い、15 の skeleton 拡張をその上に乗せる。順序は `... -> 14 -> 16 -> 15` とする。

- 2、3、9 は他と独立している。2 を最初に置くのは `go vet` の gate を早く戻すためである。
- 10 は `access` と `media/format` の両方を触るので、format/codec を作り直す 4 より前に済ませる。
- 6 は 4 と 5 の前提である。依存の cycle を先に解かないと codec identity を `plugin.Identity` にできない。
- 7 は 1 と 8 の前提である。ownership contract が決まらないと skeleton も accessor も書けない。
- 1 は 4、5、6、7、8 がすべて揃ってから行う。skeleton は他の項目の結果を通す場所である。
- 11 は 1 の後に行う。どの `flow` interface に consumer が付いたかが 1 で確定する。
- 14 は最後に行う。1〜13 の結果を記録するためである。

## 1. walking skeleton を新 contract の上で通す

現状、4 つの operator は `Convert(flow.Input[X]) (flow.Input[Y], error)` という skeleton 専用に発明した method で繋がっている。`flow.Processor`、`flow.Emitter`、`flow.Reader`、`flow.Writer` は repository 全体で参照が 0 件であり、test は `host.Open` が返した `flow.Operator` を fixture の**具象型**へ downcast している。

これでは [media.md](../media.md#m3-完了条件) の「新 contract だけで端から端まで流れる」を満たさず、skeleton を要求した目的そのものが達成されていない。

- skeleton の operator は `flow.Processor[I, O]` を実装する。`Convert` のような独自 method を contract の外に作らない。
- test は operator を `flow.Processor[I, O]` へ **Open 時に一度だけ** assert する。fixture の具象型へ downcast しない。第三者の component でも同じ経路で駆動できることが要件である。
- 駆動 loop は test が持つ小さな `flow.Emitter[O]` 実装へ出力を集める形にする。
- `Flush` も経路に含める。encoder に相当する trivial component が遅延 item を 1 つ持つようにして、`Flush` が呼ばれないと出力が欠けることを test で固定する。
- `flow.Reader` と `flow.Writer` にも consumer を与える。skeleton の入口を `flow.Reader[[]byte]`、出口を `flow.Writer[[]byte]` にすれば 4 つの interface すべてが使われる。使い道がないと判断した interface は残さず削る。
- 出力 bytes、item 数、順序、timestamp の検査は現状のものを維持する。

## 2. `access.Resource` の値コピーをやめ、`go vet` を通す

`go vet ./...` が 3 件失敗する。M2 の完了確認で通っていた gate の回帰である。

```text
access/access.go:94:9:  Value passes lock by value: access.Resource[T] contains sync.Once
access/access.go:95:9:  Ownership passes lock by value
access/access.go:124:9: return copies lock value
```

`Own()` が `Resource[T]` を値で返すため、copy ごとに独立した `sync.Once` を持つ。同じ resource の `Close` が複数回実行され得る。

- `Resource[T]` を copy しても close が一度しか起きない形にする。`sync.Once` を値型へ埋め込まない。
- `Value()`/`Ownership()` の value receiver が lock を copy しない形にする。
- `go vet ./...` の exit status が 0 になることを確認する。

## 3. `timing.multiplyFactors` の 128bit 乗算を直す

`multiplyFactors` は 128×64 の乗算で `bits.Mul64(result.hi, factor)` の**低位語を捨てて高位語を加算**している。左右が逆であり、`result.hi != 0` になった後の乗算結果が壊れる。overflow も検出されない。

```text
rescale(2^40, base 2^30/1 -> base 1/2^30) = 0, err = nil   実測。正しくは ErrOverflow
multiplyFactors(2^40, 2^30, 2^30) = hi=0, lo=0             正しくは hi=2^36, lo=0
```

[media.md](../media.md#m3-完了条件) の「rescale が checked arithmetic で overflow と rounding policy を明示する」が未達である。

- 128bit 累積値と 64bit factor の積を正しく求め、128bit に収まらない場合を overflow として返す。
- 中間値が 2^64 を越える経路を property test で覆う。値 0、符号、rounding tie だけでなく、**overflow を返すべき入力**を明示的に検査する。現在の test はこの経路を通っていない。

## 4. codec identity を marker 由来にする

`codec.ID` は手書き文字列であり、`codec.New(ID("fixture:codec"))` のように第三者が衝突しない文字列を考えることを要求している。[C8](../decisions.md) が明示的に禁じた形である。[media.md](../media.md#codec-binding) の Binding 例も文字列ではなく component（`mp3.Codec`）を指している。

- Binding の target を component の `plugin.Identity` にする。`codec.ID` の文字列 identity は削除する。
- `codec.Parser` が `ParserFunc` を直接抱える形をやめる。Parser は第一級 component であり、Binding はその identity を指す。実装は component の `Open` が返す。
- `format.Tag` は container が持つ**データ**（WAVE の `0x0055` 等）なので、文字列や数値のままでよい。ただし `format.Format` 自身の identity に流用しない。Format も component として marker identity を持つ。
- 項目 6 の依存方向の是正と同じ単位で行う。

## 5. Binding target を catalog と照合する

`internal/catalog.Build` は binding key の重複だけを検査し、target が実在する component かを見ていない。存在しない codec を指す binding を `host.New` が受理する。[F28](../findings.md) の「未導入と壊れた plugin を区別できない」と同型である。

- host 構築時に、binding の target identity が catalog に実在することを検証する。実在しなければ component identity と探した identity を含む構造化 diagnostic で失敗させる。
- 既存の key 重複・明示 override の検査は維持する。

## 6. `plugin.Set` が宣言を抽象で保持する

現在 `plugin/set.go` が `media/codec` と `media/format` を import しており、identity/config 層が media を知る形になっている。M3-2 の metadata Binding、M3-3 の `access.Provider` も同じ経路を必要とするため、ここで方向を正す。

- `plugin.Set` は media の具体型ではなく、composition 宣言の抽象を保持する。宣言は canonical な key と、catalog に実在すべき target identity を提供できればよい。
- 衝突の namespace に手書き文字列 kind を使わない。宣言の Go 型そのものが namespace になる（marker identity と同じ理由で衝突しない）。同じ型・同じ key で target が異なる場合を conflict とする。
- 依存方向を `media/codec -> plugin` の一方向にする。`plugin` は `media/*` を import しない。
- `AddBinding`/`OverrideBinding` は一般化した宣言 API に置き換える。旧 API を残さない。
- `internal/catalog` は宣言を一律に「key 重複」と「target の実在」で検証する。binding の種類ごとに分岐を増やさない。

`media/codec.Binding` と、M3-2 の metadata Binding、M3-3 の Provider がこの口に乗ることを設計時に確認する。

## 7. `flow` の ownership contract を公開 API から作り直す

現在は `NewInput` が `ownership[T]` を heap alloc し、`Take()` が `handle[T]` を alloc し、CAS と refcount を通る。fan-out のない linear path でも 1 hop あたり最低 2 allocation と 3 atomic が発生する。[runtime.md](../runtime.md#hot-path-性能契約) の #2「hop ごとの必須 heap allocation をしない」と #5「linear ownership transfer で refcount increment をしない」に反する。

公開 API から作り直す。方向は次とする。

- **`flow` は refcount を所有しない。** 共有所有は実資源を持つ層、すなわち `media/buffer.Handle` が持つ。item wrapper が資源とは別の refcount を二重に持たない。
- `Input[T]` を値型にする。保持するのは payload と、schema trait 由来の drop/fork だけとする。linear path で allocation と atomic を発生させない。
- 意味は [plugins.md](../plugins.md#plugin-authoring-api) の 3 つを保つ。`Value()` は呼び出し中だけの借用、`Take()` は ownership の move、`Share()` は fan-out や非同期保持のための retained handle。`Share()` だけが schema の `Fork` を通り、そこで初めて資源側の retain が起きる。
- `Owned[T]`/`Shared[T]` の handle 経由 wrapper は、値型で表せるなら廃止する。
- `Emitter`/`Writer` の move-on-success 規則（成功で writer へ move、失敗で呼び出し元が保持）は維持する。
- **失う検出を明示する。** double `Take` や use-after-`Take` は runtime で検出しなくなる。これは conformance testkit（M6/M10）の担当とし、既定 build の hot path に検出用 state を持たせない。[performance.md](../performance.md#buildruntime-dispatch) の「debug-only assertion が必要なら明示 instrumentation build とし、official release の唯一の安全網にしない」に従う。この判断を godoc に一行残す。
- 受け入れは test で固定する。skeleton の linear 1 hop について `testing.AllocsPerRun` が 0 であることを検査する。数値目標ではなく契約なので、benchmark ではなく test にする。

あわせて、port の `required` と multiplicity の結合を解く。現在 `Many` と `Optional` は required にできないが、[runtime.md](../runtime.md#graph-model) は両者を別の軸として列挙している。入力 1 本以上を必須とする mixer が M4 で表現できなくなる。`flow` を作り直す単位に含める。

## 8. payload accessor が暗黙に共有しないようにする

`packet.Chunk.Payload()`、`packet.Packet.Payload()`、`audio.Frame[S].Planes()` はいずれも `buffer.Handle.Share()` を返す。payload を読むだけの呼び出しが毎回 lease を alloc し CAS を通る。項目 7 と同じ契約違反が data 型側にもある。

- accessor は借用を返す。共有が必要な呼び出し側が明示的に `Share()` を呼ぶ。
- 借用の有効範囲を godoc に書く。owner が `Release` した後の借用が無効であることを型か契約で明示する。
- skeleton を項目 1 の形に直す際、accessor の変更で ownership 経路が壊れないことを確認する。

## 9. `property` の clone 規則を C17 に揃える

`property.cloneValue` は `[]byte` と `string` だけを特別扱いし、それ以外は shallow で返す。`Set` は immutable を名乗るが、`[]int`、map、pointer、struct 内の参照では caller と可変状態を共有する。[F26](../findings.md) と [C17](../decisions.md#c17-config-snapshot-は-codec-clone-だけで構成する) が排除した「snapshot でない値を snapshot と見せる」失敗と同型である。

- 値の型を推測する clone をやめる。C17 と同じ規則、すなわち「reference 型を扱う key は clone を宣言する。宣言がなければ key の定義を失敗させる」に揃える。
- 未知の第三者 property 型も同じ規則で扱えることを test で確認する。`property.Set` は未知 property を解釈せず保持できる必要があるため、clone 宣言のない reference 型を黙って通さない形にする。
- `Key[T].Get` が、key 不在かつ `T` が interface 型のときに単値 type assertion で panic する経路も同時に消す。

M3-2 の `metadata.Document`（entry value、artwork blob）はこの規則の上に乗せる。

## 10. capability 語彙の重複を解消する

`access` と `media/format` に `Capability` の同一 5 定数、`Alternative`、`AnyOf` が二重に存在する。[access.md](../access.md#source-capability) が Format に宣言させる capability alternative は access の capability そのものである。二重定義は必ず drift する。

- capability 語彙を `access` に一本化し、`media/format` はそれを参照する。
- `media/format -> access` の依存が架空の循環を作らないことを確認する。

## 11. consumer を持たない export を削る

[AGENTS.md](../../../AGENTS.md) の「途中で必要なくなったコードは削除する」「export する必要がなくなったコードは export しない」に従う。

- `schema.Pipe`、`schema.Tee`、`schema.NewCatalog`、`schema.Catalog`: repository 内で参照 0。queue は M5 の `internal/` が持つ責務であり、public contract にしない。typed factory が存在することの証明は、descriptor の factory closure と test で足りる。
- `schema.Descriptor.Drop/Size/Time(any)`: item ごとに `value.(T)` する erased API。参照 0 であり、[media.md](../media.md#m3-完了条件) の「type assertion は Open 時に一度だけ」に反する形なので残さない。trait は typed な `Type[T]` 経由か、型が確定した factory の内側で使う。
- `schema.ErrInvalidMarker`: 宣言のみで使われていない。無効な marker が診断なしに zero ID になる現状も直す。`plugin` の identity 導出と同じく、問題を保持して host 構築時に集約する形にする。
- 項目 1 の結果 `flow` の interface に consumer が付かないものが残った場合も同様に削る。

## 12. 命名の是正

- `buffer.Handle.Clone()` は `Share()` の別名である。この codebase で `Clone` は [C17](../decisions.md#c17-config-snapshot-は-codec-clone-だけで構成する) の deep snapshot を指すため、refcount 共有を `Clone` と呼ばない。
- `buffer.New` は `Allocate` の別名である。どちらか一方にする。
- `packet.Packet.Clone()` も実体は payload の共有なので同じ観点で見直す。

## 13. docs-check を実行される場所に載せる

`tools/cmd/docs-check` は動作するが、[AGENTS.md](../../../AGENTS.md) の Tools にも `tools/cmd/generate` にも CI にも載っていない。[quality.md](../quality.md#repository-wide-ci) は「同じコマンドを AGENTS.md、開発手順、CI で使う」と定めており、現状は作っただけで誰も実行しない。

- AGENTS.md の Tools へ command を追加し、いつ実行するか（設計文書を変更した時、milestone の完了確認）を書く。
- 既存の runner から実行できるようにする。新しい runner を増やさない。

## 14. 分類の記録を実態に合わせる

[scope.md](../scope.md) へ追記した「M3-1 の contract 分類」が実態とずれている。

- `media/format` の Format は skeleton で `Valid()` を確認しているだけで data path を通っていない。`codec.Parser` の `Parse` は一度も呼ばれていない。これらを「実際に consumer として使う contract」に挙げない。
- `flow.Processor`/`Emitter`/`Reader`/`Writer` が「宣言のみ」側にも挙がっていない。項目 1 の後に consumer を持つものと持たないものを正しく分ける。
- 項目 1〜13 の結果に合わせて全体を書き直す。

## 15. skeleton に packet 段を通す

項目 1〜14 の review で残った唯一の未達である。現在の skeleton の path は `bytes → chunk → frame → bytes` で、`packet.Packet` が port を跨いでいない。[media.md](../media.md#m3-完了条件) の完了条件は `bytes → packet → frame → packet → bytes`、この文書の項目 1 は `bytes → chunk → packet → frame → packet → chunk → bytes` である。

結果として次が未証明のまま残っている。

- `codec.Parser` は [media.md](../media.md#packetchunkparser) が第一級 component と定めるが、fixture では `Packet -> Packet` の shape を持つだけの no-op stub で `Process` を持たない。data path の consumer がない。
- [F20](../findings.md) の「container chunk と codec packet の境界も独立していない」を skeleton が証明していない。境界を跨ぐ item が存在しないため、Chunk と Packet が別型であることの実益が経路に現れていない。

対応:

- parser 相当の component に `flow.Processor[packet.Chunk, packet.Packet]` を実装させ、encoder 出力を `packet.Packet`、muxer を `packet.Packet -> packet.Chunk` にする。sink が `Chunk -> bytes` を担う。
- component 名を役割に合わせる。現在は `decoder` が `bytes -> chunk`（demux）、`encoder` が `chunk -> frame`（decode）、`muxer` が `frame -> bytes`（encode + mux）で、名前と処理が一致していない。`demuxer`、`parser`、`decoder`、`encoder`、`muxer` の 5 段に揃える。
- Chunk と Packet の境界を跨ぐ際に timestamp、sequence、payload ownership が保たれることを検査する。既存の bytes roundtrip と順序検査は維持する。
- [scope.md](../scope.md) の「M3-1 の contract 分類」を実際の path へ更新する。現在は deviation を正直に記録しているが、この項目でその deviation 自体が解消する。

あわせて、review で見つかった小さな非対称を同じ単位で片付ける。

- `property.Define` は clone 未宣言の reference 型に対し、診断を残さず zero `Key` を返す。`schema.Type` は `Problem()` で marker の問題を保持して port validation から host 診断へ流す形にしたので、`property` も同じ形に揃える。現状は `Set.With` の呼び出し時にしか surface しない。
- `flow.OptionalMultiplicity` は `Optional()`（required=false、multiplicity=One）と同じ「0 か 1」を二通りで表せる。項目 7 で required と multiplicity を独立した軸にしたため、multiplicity 側の `Optional` は重複である。どちらか一方に統一する。

## 16. erased descriptor から typed 実装を組み立てる機構を復元する

項目 11 は「public な `schema.Pipe`/`Tee` を削る」ことを求めたが、実装では `Descriptor` が持っていた **typed factory closure も一緒に消えた**。現在 `Descriptor` は identity と payload の `reflect.Type` だけを持ち、`flow.In[T]`/`Out[T]` は `schema.Type[T]` から `Identity()` と `Problem()` だけを取り出して traits を捨てている。`Port` が持つのは `schema.ID` だけである。

結果として、declaration から erased 層へ `T` を運ぶ経路が存在しない。planner/runtime が port を見ても `T` を知る手段がなく、edge に typed な queue や tee を置けない。取り得る道は「宣言時に `T` を捕捉した closure を持つ」「component の Open が typed edge を返す」「queue を `any` にする」の 3 つだが、3 番目は [hot-path 契約](../runtime.md#hot-path-性能契約) #10 に反し、2 番目は closure の置き場所が変わるだけである。どの道でも宣言時の捕捉は避けられない。

これは [media.md](../media.md#m3-完了条件) が M3 の条件として挙げた「第三者が `schema.Define[ID, T]` で独自 unit を宣言でき、その build に `pipe[T]`、tee、drop 等の型付き実装が含まれる。core は `T` を事前に知らない」そのものであり、[C2](../decisions.md) と [C11](../decisions.md) の拡張性主張が成立するかを決める箇所である。M5 まで未検証で積み上げない。

対応:

- `schema.Define[ID, T]` が `T` を捕捉した factory を `Descriptor` に残す。factory は `any` を返し、受け取り側が **Open 時に一度だけ** typed 値へ assert する。
- factory の product は unexported にする。項目 11 で削除した public `Pipe[T]`/`Tee[T]` を戻さない。queue の実装は M5 の `internal/` が持つ責務のままとする。
- `flow.Port` が `schema.ID` ではなく `schema.Descriptor` を保持する。port から factory へ到達できなければ機構が使えない。`Descriptor` は複製可能なまま保ち、項目 11 で削除した item ごとの `any` API（`Drop`/`Size`/`Time`）を復活させない。

  **訂正（適用済み）。** この項目は当初「`Descriptor` は比較可能・複製可能なまま保ち」と書いていたが、比較可能性の要求は誤りだったので取り消す。factory を持たせた結果、同じ marker と payload に対して `Define` を 2 回呼ぶと `Identity()` は等しいのに `==` は false になる（factory pointer が異なる）。`flow/flow_test.go` の `flowSchema()` のように呼び出しごとに `Define` する書き方は既にあるため、M4 の planner が port 間の schema 一致を `Descriptor` の `==` で判定すると、その書き方をした plugin の接続を静かに拒否する。

  `Descriptor` を非比較にする。zero-size の非比較 field を一つ持たせ、schema の一致判定が `Identity()` を使うことを型で強制する。`flow.Port` も非比較になるが、`Shape` は既に slice を含むため非比較であり、新しいコードで `Descriptor` と `Port` の比較に依存しているのは comparability を確認する test の 1 行だけである。この訂正は今なら何も壊さない。
- **M3 で consumer を与える。** foundation test で、core が事前に知らない第三者型について erased な `Descriptor` から typed な fan-out を組み立て、実際に item を通す。宣言だけの contract にしない。
- **queue policy を凍結しない。** factory の引数は機構の成立を示す最小限に留める。`Limit` の bytes/time window、backpressure、drain 規則は実 consumer である execution island を作る M5 が決める。

この項目の目的は queue を作ることではなく、「core が知らない `T` に対して型付き実装を構成できる」ことを M3 のうちに実証することである。

## 記録して先送りする事項

次は M3-1-fix の対象外とし、[checkpoint.md](../checkpoint.md) の注記へ記録する。

- `host.Open(identity)` は Plan/Program/transaction を経ずに component を開く public API である。[plugins.md](../plugins.md#open) と [runtime.md](../runtime.md#plan-と-open-transaction) の lifecycle では M4/M5 が Prepare/Program 経由の Open に置き換える。M3 の間だけ必要な API であることを godoc に明記し、M5 で削除する対象として記録する。

## 検証

- 項目ごと: 対象 package の test。`go test ./media/... ./flow/... ./access/... ./plugin/ ./host/ ./internal/... -race`
- 項目 2 の後: `go vet ./...` の exit status が 0 であること。`tail` 越しではなく直接確認する。
- 項目 3 の後: overflow を返すべき入力に対する test が存在し、通ること。
- 項目 7 の後: skeleton の linear 1 hop について `testing.AllocsPerRun` が 0 であること。
- 項目 13 の後: `docs-check` を AGENTS.md に書いた command で実行し、`docs/` 全体が通ること。
- 全項目の後: `go build ./...` と `go run ./tools/cmd/test-runner --simd`。runner の exit status を直接確認する。

## 中断して確認する条件

[m3-1.md](m3-1.md#中断して確認する条件) と同じ。特に次は独断で決めない。

- 項目 7 で、値型の `Input[T]` では [plugins.md](../plugins.md#plugin-authoring-api) の `Value`/`Take`/`Share` の意味を保てないと判明した場合。
- 項目 6 の宣言抽象に `media/codec` の Binding、M3-2 の metadata Binding、M3-3 の `access.Provider` のいずれかが乗らないと判明した場合。
- 項目 9 の clone 規則が、第三者の未知 property 型を保持できなくすると判明した場合。

## 完了時の記録

1. [m3-1.md](m3-1.md) の作業単位 1〜12 と [media.md](../media.md#m3-完了条件) の該当条件を逐条で再確認する。
2. `go vet ./...` と `go run ./tools/cmd/test-runner --simd` を実行する。
3. [scope.md](../scope.md) の分類節を項目 14 のとおり書き直す。
4. [checkpoint.md](../checkpoint.md) の M3 行と注記を更新する。
