# Roadmap checkpoint

> 実装進捗: **4 / 12 マイルストーン完了（M0〜M3）**

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
| M3 | 完了 | M3-1 の walking skeleton、M3-2 の metadata model/Binding と raw preservation、M3-3 の `media/side`、`stream.Event`、access の Reference/Provider/transaction/spool/probe/snapshot、typed endpoint trait/device/query を実装した。scheme conflict は既存 `plugin.Declaration`/catalog で検証し、device scan・permission・network side effect は追加していない。固有の完了条件は [media.md](media.md#m3-完了条件)、[access.md](access.md#m3-完了条件)、[scope.md](scope.md#m3-完了条件) を参照する。 |
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
- erased descriptor から typed 実装を組み立てる機構は M3 の担当とする。一度 M5 へ先送りする判断をしたが撤回した。`flow.Port` が `schema.ID` しか持たないと declaration から erased 層へ `T` を運ぶ経路がなく、M5 が必ず `media/schema` と `flow` の public API を開け直すことになる。[media.md](media.md#m3-完了条件) はこれを M3 の完了条件として挙げており、[C2](decisions.md) と [C11](decisions.md) の拡張性主張が成立するかを決める箇所でもある。M3-1 で `schema.Descriptor`、port 経由の factory、typed queue/fan-out consumer を実装し、queue policy だけを M5 へ残す。exported な `schema.Queue`/`Fanout` は erased factory の返り値を Open 時に一度だけ assert するための最小 interface であり、runtime の queue contract ではない。bounded 化、backpressure、cancel を持つ実 queue は M5 の `internal/` が持ち、その時点でこの interface を置き換えるか返り値型を変えるかを決める。`Descriptor` は非比較とし、schema の一致判定は `Identity()` が担う。同じ marker への `Define` は呼び出しごとに別の factory closure を捕捉するため、`==` は同一 schema を別物と報告してしまう。
- `host.Open`/`Catalog.Open` は M3 の walking skeleton のためだけの public API だったため、M5 まで残さず M3 で削除した。skeleton は `internal/catalog` から index を作って `plugin.Component.Open` を呼ぶ。component を identity 一つで開くことは public な capability ではなく、[runtime.md](runtime.md#plan-と-open-transaction) のとおり M4/M5 が Program から依存順に開く。削除予定の API を公開したままにしない。
- M4 着手前に、refactor 後の三者の体験と概念の一貫性を評価し、次を是正した。(1) [experience.md](experience.md) が 22 文書中ただ一つ完了条件を持たず、`standard.Convert` の最短経路も plugin 開発者向け helper も散文にしか存在しなかったため、M6（利用者の最短経路と plugin 開発者の負担実測）と M9（三者の体験と受け入れ test）へ完了条件を割り当てた。(2) `config.Schema` の identity だけが手書き文字列で、しかも衝突検査も無かった。foundation の他の identity がすべて marker 由来である中で第三者に一意な文字列を考えさせる唯一の箇所だったため、`config.Struct[Marker](factory)` の形へ移した。C8 の規則が foundation 全体で例外なく成立するようになった。
- M4 着手前に計画そのものの妥当性を評価し、検証の時期を前倒しした。(1) 最も risk の高い仮定（open schema と planner が実規格に耐えるか）が M6 まで検証されない構造だったため、container を持たない実 PCM を M4 の成果へ移した。M6 はそこへ WAVE container を足す。(2) M6 が「初めて現実に当たる」milestone でありながら同時に旧 WAVE/PCM を消す順序だったため、conformance corpus 通過 → standard/integration/testkit → 旧経路削除 の順を規則化した。(3) 中心目標である拡張性の実証が最終 milestone だったため、M6 と M9 へ gate を前倒しし、M11 は維持確認に変えた。(4) [findings.md](findings.md) の 53 件に担当 milestone が無く完了判定できなかったため担当列を追加した。(5) `audio.md` の converter 配置の仮定が M8 まで検証できなかったため、合成 filter chain で挿入数を数える gate を M4 完了条件へ入れた。
- M4 着手前に計画と実装の全体監査を行い、次を是正した。(1) M4〜M11 に milestone 固有の完了条件が無かったため、[planner.md](planner.md#m4-完了条件) と [runtime.md](runtime.md#m5-完了条件) へ M4/M5 を追加し、[refactor.md](../refactor.md#実装ロードマップ) へ「完了条件は着手前に書く」規則を足した。M6 以降は着手直前に書く。(2) [findings.md](findings.md) の「新経路で解消」注記が一件も運用されていなかったため、M2/M3 で構造的に排除済みの 15 finding へ注記した。(3) [capability.md](capability.md) の M3 担当行に確認済み test を記録した。(4) 呼び出し元も実装者も無い export を削除した（`timing` の rescale 自由関数 3、`access` の capability interface 8、`packet.NewChunk` と `NewTimestampedChunk` の重複）。`access` は capability を interface と string 定数で二重に表現しており drift しうる状態だった。
- `tools/cmd/docs-check` は AGENTS.md に載せた standalone command とし、`generate` や `differential` と同じ扱いにする。自動 gate への配線は [quality.md](quality.md#repository-wide-ci) の `documentation` gate として M10 の root CI が担当する。それまでは設計文書を変更した時と milestone の完了確認で手動実行する。
- M3-2 では `media/metadata` の Document/RawBlock/Blob/Mapping/Binding、`media/tag` の現行 ID3/Vorbis/RIFF INFO 共通 key、static `stream.Descriptor.Metadata`、typed metadata event の declaration、foundation の raw-preserving encoding consumer を実装した。実 plugin encoding の移行と loss report の surface 表示は M6/M7 の担当である。
- M3-3 では consumer を持つ `media/side` と `stream.Event` だけを data path に接続し、Access/Endpoint の残りは宣言型に留めた。Provider binding/acquire/probe は M4、transaction 実行/rollback と spool 挿入は M5、具体 Provider/Format は M6、device/session Endpoint は M9 が担当する。clock、latency、drop/reconnect、exclusive/shared、hotplug、`AllOrNothing` は consumer が現れるまで凍結しない。
- `plugin/identity` の import path snapshot test は、公式 plugin へ marker identity を導入する M6/M8 で marker ベースの test へ置き換える。あわせて置き場所も見直す。M2 で `plugin/` 直下に foundation の `package plugin` が入ったため、現在は `plugin/flac` 等と並んで規格 family の一つに見える。[architecture.md](architecture.md#依存原則) が想定する最上位 `integration` module が適切な位置である。
- [C17](decisions.md#c17-config-snapshot-は-codec-clone-だけで構成する) を実 config で検証し、規則を維持すると決めた。FLAC encoder config は 18 field 中 17 を登録でき、`Apodizations []func([]float64)` だけが canonical 表現を持てず登録に失敗する。C17 が過剰なのではなく、closure が parameter を捕捉して `Tukey(0.5)` と `Tukey(0.9)` を区別できなくしている実装漏れを検出したものである。data 表現へ移す作業は [capability.md](capability.md#挙動変更の記録) の B4 として M8 が担当する。prototype は捨て、一般の contract test として `config/function_test.go` に残した。
- C17 検証で分かった M4/M8 への申し送り。`plugin/audio` の `MixerConfig` は空 struct で、port 数は config になく実行時の入力数から導出している。[config.md](config.md#動的-field-と-topology) が求める「安定した config 型に count または typed port definition を持たせ、解決済み config を `Shape` が読む」は移行ではなく新規設計である。M3 は静的 port shape だけを作り、config が port 数を決める動的 `Shape` phase と port 数の出所は M4 が決める（consumer が planner 側にしかないため）。equalizer の可変長 band は現在 comma 区切り string で、C17 は通るが型付き slice への移行は M8 が担当する。
- 明示 schema の登録コストは実測で 1 field あたり 3 行前後（FLAC encoder は struct 23 行に対し登録 154 行）。ただし現行は help/range/単位が `config_options.go` と struct tag に分散しており、それらを含めた総量では schema 化で減る見込みである。[config.md](config.md#public-model) が M8 へ割り当てた tag adaptor は、scalar のみの config には効くが equalizer のような conditional/可変長を含む config には明示 schema が必要、という前提で判断する。
- `bindings/wasm/bindings_gen.go` は `gowasm-bindgen` の出力で、gofmt 非準拠のまま checked in されている。format すると生成器の出力と乖離して drift 検出が誤発火するため、意図的に未 format で維持している。[supply.md](supply.md#generate) の generate phase が求める「生成物を format/typecheck/compile して checked-in state と比較する」形は、build recipe に gofmt を挟む M9/M10 で満たす。
- repository の line ending は `.gitattributes` の `* text=auto eol=lf` で固定した。これがないと Windows checkout が CRLF になり、`gofmt -l` が数百 file を誤検出して format を gate にできない。`.gitignore` と `LICENSE` は CRLF で commit されているため、次に `git add` した時点で LF へ正規化される。
