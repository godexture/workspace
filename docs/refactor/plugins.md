# プラグイン contract

## 目的

プラグイン API は、次の三者を同時に満たす必要がある。

- 利用者: 標準構成は一行で使え、custom 構成も通常の Go コードで理解できる。
- plugin 開発者: ID の衝突、runtime の並行制御、CLI/WASM 固有処理を意識せず component を追加できる。
- core 開発者: plugin API を保ったまま planner、scheduler、memory、observability を交換できる。

## identity

### reflection を残す理由

Go の named type identity は package path と type declaration で定まり、同一 build 内で一意である。これを利用すれば、第三者に `com.example.foo` のような文字列 ID を選ばせたり、中央 registry へ名前を予約させたりせずに済む。これは現在の reflection 利用の正しい動機であり、捨てる必要はない。

問題は、現在の identity が設定型等の実装詳細と結び付くと、設定の refactor が component identity の変更として扱われ得る点である。また reflection を runtime dispatch にまで持ち込むと性能とエラー検出時期が悪化する。

専用の空 marker type を用意し、identity と設定 schema を分ける。

marker identity、typed config、component、definition を組み立てる現行 API は
[plugin の `ExampleNewSet`](../../plugin/example_test.go) を正本とする。

動く code は `plugin` と `host` の `Example` 関数を正本とする。この文書の例は形を示すためのもので、helper が増えれば簡潔になる。

marker type は export しなくてよい。host は `reflect.TypeFor[pluginID]()` から canonical identity を作る。概念上は次の情報である。

```text
github.com/acme/godec-foo.pluginID
github.com/acme/godec-foo.decoderID
```

外部表示や serialized `Plan` には安定した文字列表現を使うが、第三者がそれを手入力しない。Go module/package を fork すれば別 identity になるのも、供給元が変わったことを正しく表している。

### identity と version の区別

identity に version、display name、設定型を含めない。

- identity: 「どの plugin/component か」
- implementation version: build info または descriptor
- config schema version: config decoder が扱う wire schema
- alias: CLI 表示と検索に使う非一意な名前

同じ marker identity を同じ `Set` に二度追加した場合は host 構築時に error とする。暗黙の last-wins は使わない。置換したい利用者は明示的な override API で対象 identity と置換元を指定する。

### reflection の使用範囲

許可:

- `Define` 時の marker identity 取得
- config schema の構築
- property/schema の control-plane type erasure
- catalog 構築と plan compile

禁止:

- frame/packet ごとの type lookup
- edge ごとの `reflect.Value.Call`
- runtime の文字列 ID lookup
- data unit の serialize/deserialize による plugin 間受け渡し

`Program` 生成時に typed function と dense integer index へ解決する。

## 明示的な composition

global mutable registry と blank import による副作用登録を廃止する。plugin package は definition を返すだけにする。

explicit `plugin.Set` から immutable Host catalog を作る現行 API は
[host の `ExampleNew`](../../host/example_test.go) を正本とする。公式 plugin family の
具体 composition は M6 以降で同じ経路へ載せる。

標準利用者向けには公式 composition を提供する。

```go
h, err := host.New(host.Plugins(standard.Set()))
```

`Set` は persistent/immutable value とする。`Add`、`Remove`、`Override` は新しい値を返す。host 構築後の catalog も immutable にし、plan 実行中に component 集合が変わらないようにする。

```go
set := standard.Set().
    Add(acme.Plugin()).
    Override(flac.CodecIdentity(), acme.FastFLAC())
```

plugin の import、composition、host 構築が明示的になることで、test ごとに隔離された catalog を作れ、CLI と library の component 差異も追跡できる。

## 将来の discovery

core は `.so` のロード、module download、署名 trust store、marketplace を直接提供しない。将来の discovery layer は、最終的に同じ `plugin.Set` または remote component descriptor を組み立てて `Host` へ渡す。

Go の in-process plugin を後から導入する方法には platform 制約、toolchain/version 一致、ABI、unload 不可等があるため、標準 contract に固定しない。動的導入が必要になった時点で次のいずれかを上位層が選べる。

- 利用者の custom binary を生成/build し、通常の static import を使う。
- plugin を別 process として起動し、versioned wire protocol を使う。
- 対応 platform に限り Go plugin loader adaptor を使う。

どの方式でも planner に見せる capability と component identity は同じにする。

## Access Provider と Endpoint

`plugin.Set` は media component に加え、byte object を解決する `access.Provider` definition を保持できる。Provider は identity/config/descriptor/provenance/override 規則を共有するが、typed data-plane node ではない。

RTSP/HLS/device 等は seekable byte Provider に偽装せず、通常の typed Endpoint component とする。capability、probe、transaction、clock、application-owned authority の詳細は [access](access.md) を正本とする。

## component definition

component は従来の decoder/demuxer/filter 等の固定 registry 型を増やす方式ではなく、typed port と phase を宣言する `Spec` とする。

構築と pure Compile の実行例は [plugin の Example](../../plugin/example_test.go) を正本とする。
実装は config 型 `C`、private plan 型 `P`、control-plane descriptor 型 `D` を持つ
`Spec[C, P, D]` を `WithSpec` で一度だけ type erase する。

- `C`: ユーザー設定。immutable に解決される。
- `P`: compile 済み component 固有 plan。runtime object ではない。
- `D`: port 間を流れる control-plane descriptor。media graph では `stream.Descriptor`。
- `Shape`: static shape も config 依存 shape も同じ phase で返す。
- `Compile`: 入力 descriptor と設定から出力 descriptor、requirements、cost、resource request を計算する純粋関数。
- `Suggest`: planner が不足 schema を埋める候補設定を列挙する optional hook。
- `Open`: 選択済み `P` と host service から runtime operator を一度だけ生成する。

`Suggest` と `Compile` は変換規則を別々に実装しない。`Suggest` は設定候補だけを提案し、それぞれの出力や cost は必ず同じ `Compile` を通して得る。

## lifecycle

Host build、prepared job、実行 transaction を次の lifecycle に統一する。

```text
Register
  -> Normalize/Bind Access and Endpoints
  -> Acquire/Inspect Inputs
  -> Probe
  -> Inspect
  -> Shape
  -> Compile
  -> Optimize
  -> Describe Plan/Build Program
  -> Begin Output Transactions
  -> Open Operators/Endpoints
  -> Run
  -> Finalize
  -> Flush/PrepareCommit/Commit
  -> Close
```

### Register

marker identity、descriptor、config schema、port shape、capability を検証し、immutable catalog を作る。欠陥 component を黙って除外せず host 構築を error にする。

### Bind と Acquire

Reference と Endpoint request を catalog definition へ binding し、input Access session と read-only endpoint capability を取得する。これは probe/inspect に必要な I/O を含む prepared job phase であり、component の semantic `Compile` とは分ける。output transaction、live endpoint、media operator はまだ Open しない。

### Probe と Inspect

Format component だけが source の shared bounded immutable view を読む。Probe は evidence/追加 range request を返し、Inspect は選ばれた候補が stream topology、carrier、properties を読み取る。I/O capability は alternative requirement として明示し、隠れた type assertion にしない。

### Shape

入力数で出力 port 数が変わる demuxer、mixer、splitter 等だけが topology を確定する。通常の一入力一出力 Processor には見せない。

### Compile

semantic transformation を記述する唯一の phase である。副作用を持たず、同じ入力で何度呼ばれても同じ結果になる。planner は候補探索、bridge 挿入、再検証のために繰り返し呼べる。

component が直接満たせない場合は、文字列 error でなく構造化 requirement を返す。

solver は bridge 候補を挿入して再度同じ `Compile` を呼ぶ。

### Open

最終的に選ばれた component のみを一度開く。I/O handle、buffer grant、task group、clock、diagnostics 等は明示的な narrow service として渡す。全 service を引ける service locator は渡さない。

Open は scope 内で transaction として行う。途中で失敗したら、既に開いた component、Endpoint、resource、output transaction を逆順に閉じ、sink を Abort する。

### Run、Finalize、Close

Run は compile 済み規則を再計算しない。Finalize は encoder の遅延 packet、muxer index/header patch、metadata flush 等を処理する。その後に sink Flush/Sync/PrepareCommit/Commit を行う。Close は resource release だけを担当し、出力成功を意味しない。

EOF は edge close で表し、`PacketKindStreamEnd` のような data packet sentinel に最終 codec parameters を混ぜない。最終値は `Finalize` の明示 contract で渡す。

## plugin authoring API

通常の plugin 開発者には、一 item の変換を中心にした `Processor` を提供する。

現行の typed port と ownership の最小経路は
[flow の Example](../../flow/example_test.go) を正本とする。`Processor` は同じ
`Input`/`Emitter` 上にあり、scheduler や channel を plugin API へ露出しない。

`Input` は借用 view であり、次を明示する。

- `Value()`: 呼び出し中だけ読む
- `Take()`: ownership を取得し、出力へ移す
- `Share()`: fan-out や非同期保持のため retained handle を得る

利用者が通常経路で `Release` を手書きしなくてよいようにする。

複雑な parser、decoder、mixer、seekable demuxer には typed Reader/Writer と host task group を扱う `Operator` を提供する。両 API は同じ port/schema/lifecycle を使い、別 runtime を作らない。

## config と capability

config の唯一の外部 contract は typed schema とする。完全な Go value と疎な surface patchを分け、`default < preset < explicit` の順で immutable に解決する。入力依存の `auto` は config mutation ではなく `Compile` が Plan に記録する。generated functional options は廃止する。詳細は [config](config.md) を参照する。

capability は巨大な boolean manifest ではなく、accepted/emitted schema、property constraint、port multiplicity、I/O requirement、variant、effect、resource/latency estimate の組み合わせで表す。index は候補の絞り込みだけを担当し、最終判断は同じ `Compile` が行う。variant と再現性の契約は [performance](performance.md) を参照する。

## trust と障害境界

in-process plugin は host と同じ権限を持つ。host は次を行うが、sandbox とは呼ばない。

- `Run`/execution island の入口で panic を recover し、diagnostic と job failure に変換する。
- host task group で開始された task の cancel/join を追跡する。
- resource grant と queue limit を適用する。
- `Open`/`Finalize`/`Close` の error を集約する。

plugin が独自に作った goroutine の panic、無限 loop、`unsafe` による memory corruption、process exit は封じ込められない。強い隔離が必要な場合は別 process adaptor を使う。

panic recovery を frame ごとに `defer` してはならない。execution island または長寿命 task の境界で一度だけ設ける。

## custom host template

custom host は generator 固有形式ではなく、通常の 20〜30 行の Go `main` とする。

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/acme/godec-extra"
    "github.com/godexture/godec/cli"
    "github.com/godexture/godec/host"
    "github.com/godexture/godec/standard"
)

func main() {
    os.Exit(run(context.Background(), os.Args[1:]))
}

func run(ctx context.Context, args []string) int {
    plugins := standard.Set().Add(extra.Plugin())

    h, err := host.New(host.Plugins(plugins))
    if err != nil {
        fmt.Fprintln(os.Stderr, err)
        return 1
    }
    defer h.Close()

    return cli.Run(ctx, h, args)
}
```

通常利用者は公式 binary を使うだけでよい。library 利用者は `standard.NewHost()` の convenience を使える。custom plugin を追加したい利用者だけが上記 composition を書くため、global registry を廃止しても一般利用者の負担は増えない。

CLI library は `Host` を引数として受け取り、公式 `cmd/godec` が `standard` を import する。Oto 等の playback dependency は transcode 標準 bundle から分離し、専用 command/adaptor に置く。

## descriptor と配布情報

plugin descriptor は runtime identity と分けて次を持てる。

- display name、homepage、source repository
- implementation version/build provenance
- SPDX license expression
- pure-Go/CGO/native dependency の属性
- optional digest/signature metadata
- component 一覧と alias

第三者 plugin 全体に license policy を強制しない。一方、公式 `standard` と公式 binary は allowlist、SBOM、NOTICE、source provenance を release gate にする。

## M2 完了条件

M2 は identity、immutable `Set`/Catalog、構造化 diagnostic の contract を foundation package として新設する milestone であり、公式 plugin の移行、旧 `core/registry`・`sdk/catalog` の削除、port/`Compile` の検証までは要求しない。config 側の条件は [config](config.md#m2-完了条件) を参照する。

- `plugin` package が marker type の Go type identity から canonical identity を導出し、identity に version、display name、config 型、alias を含めない。
- 同じ marker identity を同じ `Set` へ二度加えた場合に error になる。暗黙の last-wins や registration order 依存を持たず、置換は対象 identity を指定する明示 `Override` だけで行う。この error は `Set` が保持し、`host.New` が集約報告する。composition 時に error を返して呼び出し側が握りつぶせる形にしない。壊れた composition が痕跡なく消えることは [F28](findings.md) と同じ失敗である。
- `Set` が immutable な persistent value であり、`Add`/`Remove`/`Override` が元の値を変更せず新しい `Set` だけを返す。composition を chain して書ける。新 package が package-level mutable registry と `init` による副作用登録を持たない。
- `internal/catalog` が host 構築時に検証済み immutable index を作り、invalid entry を黙って除外せず host 構築 error にする。「未導入」と「壊れた plugin」を区別できる（[F28](findings.md)）。未 Build の zero config schema を持つ component もここで失敗させる。
- host 構築 error が component identity と対象 field/descriptor path を含む aggregate な構造化 diagnostic として返り、最初の一件で打ち切らない。
- `Override`、`OverridePlugin`、`Remove` の対象 identity が存在しない場合、diagnostic が探した identity を path に持ち、`Set` 内の近い identity を候補として示す。selector から component を解決する surface API は M9 で追加するため、その経路の候補提示もそこで扱う。
- component definition は identity、descriptor、config schema、alias、provenance を検証する。port shape、`Compile` purity、`Suggest` bounded 性の検証は M3/M4 で追加するため、この時点では definition を不透明な値として保持してよい。
- component は config schema を description だけでなく型消去した resolver として保持し、catalog 側から `Patch` を `Resolved` へ解決できる。型消去した schema は `config.Schema[C]` からしか作れないようにし、第三者が valid を騙る実装を差し込めないようにする。
- descriptor は plugin 単位で必須とし、component 単位では未設定 field を親 plugin の descriptor から引き継ぐ。family の全 component へ同じ version や license を書かせない。ただし `DisplayName` は component ごとに異なるべき唯一の field なので継承せず、未設定なら marker 名を表示に使う。
- 公式 plugin package が definition を返す API の形を確定する。`flac.Plugin()`、`flac.Codec()` のように関数で返す形とし、`var Plugin = ...` の変数形は使わない。実際の公式 plugin の移行は M6/M8 で行う。
- 複数 `Host` を同一 process で構築しても catalog、default、CPU feature、resource state が互いに影響しない。
- reflection の使用が `Define` 時の identity 取得、config schema 構築、catalog 構築に限定される。
- 上記を marker identity、duplicate、explicit override、invalid definition、複数 Host isolation の unit/race test で検査する。公式 plugin を import しない。

M2 では次を未完了事項として残す。`plugin/identity` の import path snapshot test は、公式 plugin へ marker identity を導入する M6/M8 の時点で marker ベースの test へ置き換える。各 plugin の `register.go`、旧 `core/registry`、`sdk/catalog` は M5 末尾の切断で一括削除する（[inventory](inventory.md#m5-の切断)）。

## M6 完了条件

M6 は公式 composition と第三者拡張が初めて実 plugin で成立する milestone である。component contract 自体は M2〜M5 で確定しているため、M6 が確認するのは「その contract の上で実 family と第三者 plugin が同じ経路に載るか」である。作業単位は [media](media.md#m6-完了条件) を正本とする。

- **拡張は component に付き、`plugin.Set` が唯一の合成値である。** Access Provider と Endpoint を独立した合成入力にせず、宣言する対象の component へ `plugin.ComponentOption` として付ける。M5 の `plugin.WithReader`/`WithProcessor` が typed execution binding を component へ付けたのと同じ形であり、`plugin` に増えるのは marker key 付きの trait slot 一つだけである。`access` が `plugin` を import している以上 slot 自体は型消去になるが、取り出し口は `access`/`endpoint` が型付きで提供し、foundation に kind の switch を残さない。
- **利用者が拡張の種類を知らなくてよい。** 第三者は Provider を含む plugin でも `acme.Plugin()` 一つを配り、利用者は `standard.Set().Add(acme.Plugin())` と書く。合成の入口を種類ごとに分けないため、`host.Providers`/`host.Endpoints` option と `endpoint.Component` を削除する。
- `standard` package が `Set() plugin.Set` と `NewHost(extra ...plugin.Definition)` を提供し、WAVE、linear PCM、file Provider の definition と codec/metadata Binding をまとめて載せる。`host.New(host.Plugins(standard.Set()))` が公式 composition の完全な形になる。利用者が family ごとの import と Binding 登録を手で並べなくてよい。
- **方向は trait の付き先が決める。** source trait は 0-in/1-out の component、sink trait は 1-in/0-out の component に付く。方向を手書き宣言する enum を持たない。M5 時点の `access.SourceSinkRole` は構築できるが決して bind されない dead value であり、宣言と実態が食い違いうることが実証されている。
- 公式 plugin package が `wave.Plugin()` のように definition を関数で返す。`var Plugin = ...` の変数形を使わない。M2 で決めた形の最初の実適用である。
- **out-of-tree 相当 plugin の拡張性 gate を通す。** `integration` から、core と surface の source を一切変更せずに新しい schema、Format、Codec、Metadata Encoding、Access Provider を追加し、標準 `Set` へ足した Host で実際の変換が通ることを test にする。第三者側が書くのは marker、config schema、`Compile`/`Open`、trait、Binding だけであり、利用者側は `Add` 一つで足りる。
- 旧 `plugin/identity` の import path snapshot test を置き換える。実 declaration の marker identity と package path を `integration` 側から検査し、公式 family の identity が package 移動で黙って変わらないことを固定する。
- 公式 plugin が別の公式 plugin を直接 import しない。WAVE は PCM の実装型ではなく codec Binding を通じて Parser/Decoder へ到達する。
- 通常 component の source に global registration、衝突回避のための手書き文字列 ID、goroutine/channel、scheduler、手動 `Release`、surface DTO が現れない。概念数の実測は [experience](experience.md#m6-完了条件) が担当する。
- 上記を `integration` の test で検査する。foundation package は公式 plugin を import しない。

M6 では次を未完了事項として残す。MP3/FLAC/audio filter の family 移行と公式 plugin 間の直接依存の解消は M8、dynamic install と remote plugin protocol は [decisions](decisions.md#deferred-without-blocking-the-first-implementation) のとおり延期する。
