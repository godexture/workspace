# decision ledger

この文書を、product/architecture 判断の正本とする。`Confirmed` 以外を決定済みとして実装しない。

## Confirmed

### C1. plugin の基本導入は static import

Go package を import し、明示的な plugin Set へ加える。global `init` registry は使わない。

install/discovery は将来考慮するが、transcoding foundation が package manager/marketplace を直接持たない。

### C2. FFmpeg を代理できる拡張性を目標にする

audio だけでなく、video、subtitle、data、timed event 等を第三者 plugin/schema で追加できる構造にする。

### C3. in-process third-party plugin は利用者が信頼する

Host は panic recovery、cancel、task/resource tracking を行うが sandbox とは呼ばない。強い隔離が必要な場合は将来の別 process adaptor とする。

### C4. default output は入力を維持する

指定がない限り、入力と同じ format、codec、stream mapping、metadata を可能な範囲で維持する。copy/remux が可能なら不要な decode/encode をしない。

### C5. 公式 codec は pure Go

公式 production codec で CGO を必須にしない。`unsafe` と CGO 不要 SIMD は許容する。native/CGO は test、reference adaptor、optional/third-party implementation で利用できる。

### C6. copyleft を公式配布物から避ける

可能な限り MIT とし、公式 artifact は dependency license、SBOM、provenance を検査する。

### C7. 数値誤差を許容する高速実装を選択可能にする

処理速度が向上する場合、ユーザーが Fast/Stable/Portable 等の policy を選べる。timestamp/order correctness、frame 欠落/重複は数値誤差の許容に含めない。

### C8. reflection identity の利点を維持する

第三者に衝突しない文字列 ID を考えさせない。plugin/component 専用 marker type の Go type identity を利用し、config type、version、alias から分離する。

### C9. metadata key/規格は core から開く

core は Key/Document/Origin/RawBlock contract のみを持ち、共通 vocabulary は `tag`、第三者 key/encoding は plugin として追加する。

### C10. metadata の表現不能項目は warning

既定は best effort + structured warning/loss report とし、黙って捨てない。変換不能で job を失敗させる strict mode は opt-in。

### C11. 万能 `Frame` を廃止する

`audio.Frame`、`video.Frame`、`subtitle.Cue`、第三者 schema を typed port で接続する。tee、queue、discard、observation 等の全型共通処理は schema trait から型別に構築する。

### C12. 後方互換層を残さない

新しい縦断経路へ公式利用側を同時に移し、旧 factory/resolver/routing/registry/SDK abstraction を削除する。

### C13. monorepo と段階的な module/release 境界

product sourceは一つのmonorepoに統合し、source codeのGit submoduleを使わない。codeと独立して更新・配布され、通常build/testの必須入力にならないtest/demo assetにはdata submoduleを許容する。現行の`example/assets`とFLAC conformance corpusは維持でき、M10で一律削除しない。M1 時点の重複 `example/web/assets` は M5 で canonical asset へ統合した。repository、module、packageを同じ境界として扱わない。

設計・pre-v1期間:

- foundation、公式pure-Go plugin、standard、基本CLIは一つのproduct module/release trainでatomicに変更する
- tools、native/reference integration、WASM等の独立targetだけをnested moduleにする
- full conformance/benchmark corpusはproduct moduleへ含めない

public contractとfamily境界が安定した後:

- foundationを最下層moduleとして独立させる
- MP3、FLAC等の公式pluginは規格family単位のnested moduleとして独立release可能にする
- standard distributionはtested plugin version setを依存として固定する
- third-party pluginは常に独立moduleとしてfoundation public contractだけへ依存できる

package pathは最初から`plugin/<規格>`に固定し、family directoryへ同じpathの`go.mod`を置いてもimport pathとmarker identityが変わらない構成にする。公開後の曖昧なmodule splitを避けるため、独立releaseを開始するfamilyは最初のstable v1より前に切り出す。

MP3/FLACは親bundleの下でcodec/format/parser実装を`internal` subpackageに分け、親が`Plugin()`、`Codec()`、`Format()`等を提供する。WAVE/PCM等、Bindingだけで結ばれる独立規格は別packageにする。flat repository namespaceのための`codec-`/`format-` prefixは廃止し、複合語の方が明確な正式名称は無理に短縮しない。

これにより設計中は不要なversion/tag調整を避け、最終的なselective download、独立release、foundationとの一方向dependencyを失わない。

### C14. Access Provider と typed Endpoint を foundation contract に含める

foundationへ具体的なprotocol/device実装を入れず、第三者が実装するためのtyped contractだけを置く。

ただし一つの汎用 protocol interface にはしない。

- file、HTTP object、S3 object 等は Reference を byte Source/Sink session に解決する Access component trait
- RTSP、RTP、HLS、DASH 等は clock/dynamic topology を持つ typed Endpoint component
- camera、microphone、speaker 等は Device trait を持つ typed Endpoint component
- direct reader/writer は owned/borrowed Access adaptor
- source acquisition、probe、Plan、Run を結ぶ primary API は Prepared Job
- concrete HTTP/S3/device 実装と install/discovery は別 package/上位 product

Provider/Endpoint の definition が Set に存在することと、application が実際に渡す Provider/handle・OS権限を分ける。foundation に Job ごとの権限 engine は設けない。詳細は [access と endpoint contract](access.md) に記載する。

### C15. offline既定はFastかつRepeatable

offline jobの既定presetは`Fast`とする。presetはcomponentへ渡すruntime modeではなく、HostがCompile前に次のpolicyへ展開する。

`Fast` は component config や plugin が直接分岐する enum ではなく、Host が次へ展開する named preset とする。

- Goal: throughput
- Accuracy: 規格上 exact が必要な処理は exact、それ以外は variant が宣言した tolerance
- Repeatability: `Repeatable`
- Artifact: final byte identityは要求しない
- Implementation: 公式 pure-Go、`unsafe`/SIMD/FMA/parallel を許可。native は別 policy
- Continuity: preserve。drop/conceal は許可しない
- Resources: Job/Host limit 内で worker 等を自動解決

選択 variant、CPU feature、worker、block/partition、seed、実効 policy を Plan へ固定する。timestamp/order、frame/sample の欠落・重複、lossless semantics、validation は緩めない。同一 execution signature の byte reproducibility が必要なら `Stable`、宣言した architecture/thread domain を越える byte reproducibility が必要なら `Portable` を選ぶ。

`Realtime` はArtifact再現性levelの第四値ではなく、主に Goal/Continuity/Resources の preset とし、Accuracy/Repeatability/Artifactの各policyと組み合わせる。詳細は [性能と再現性](performance.md) に記載する。

schedule由来のbounded variationを持つ`Variable` variantは明示opt-inとする。Fast/Stable/Portableごとの実装を複製せず、一つのvariantが複数policyを満たせるcontractにする。preset名はRunへ渡さず、sample/pixel/symbol loopにmode branchを置かない。

### C16. foundationはAccess権限管理を提供しない

このprojectのprimary productは、開発者がGo applicationへ組み込むlibraryである。変換用HTTP serverをproduction productとして提供しない。

- foundationのAccess contractはSeek、ReadAt、Snapshot、transaction等のI/O capabilityとdependency injectionを表す
- file/network/deviceへ何を許可するかは、組み込みapplication、渡されたProvider/handle、OS/container/browserの責務とする
- path、scheme、host、CIDR、credential等のpermission DSLやJobごとのauthority engineをfoundationに設けない
- in-process pluginは利用者が信頼するため、Providerだけを制限してsandboxと見せない
- 強い分離が必要なら将来の別process/OS sandbox adaptorで扱う

official convenience package/CLIはlocal file、stdin/stdout、利用者が構成したHTTP client等を提供できる。browser WASMはbrowserから渡されたFile/Blob/Stream/fetch resultとbrowser sandboxへ従う。

HTTP serverは固定された公式pluginだけを使う小さなdemo/reference implementationとする。upload、temporary output、cancel、bounded size/concurrency、cleanup等の事故防止は行うが、汎用remote URL resolver、third-party plugin loading、production向けauthorization/SSRF policy、multi-tenant securityをproject contractにしない。production利用を表明しない。

### C17. config snapshot は codec `Clone` だけで構成する

任意の Go 値を推測して複製する generic reflection clone を使わない。[F26](findings.md) のとおり、reflection clone は unexported field を静かに shallow copy し、snapshot でない値を snapshot と見せる。

reference 型を扱う codec は `Clone` を宣言し、宣言がなければ schema 登録を失敗させる。snapshot は default factory が fresh な値を返すことと、登録 field ごとの codec `Clone` だけで構成する。登録されていない field は canonical/fingerprint にも snapshot にも参加しないため、`C` に未登録の field があれば schema 登録を失敗させる。

### C18. secret は surface 表現に出さず、redaction marker を値にしない

secret は redaction が本質であり、surface 表現の decode と encode を逆関数にできない。したがって次の二つを両方守る。

- 構造化 codec の surface encode は secret field を出力しない。decode 側はその field を未指定として扱う。
- redaction marker を値として decode することを error にする。marker が表示、保存 graph、手入力のどこから来ても secret にならない。

前者だけでは手入力の経路が残り、後者だけでは secret を含む config の roundtrip が常に失敗する。人間向けの表示は wire 表現と別に持つ。

### C19. composition の error は `Set` が保持し `host.New` が集約する

`plugin.Set` の `Add`/`Remove`/`Override` は error を返さず、新しい `Set` だけを返して chain できる。重複 identity、無効な override target、存在しない削除対象は `Set` が diagnostic として保持し、`host.New` が他の diagnostic と一緒に報告する。

composition 時に error を返すと呼び出し側が握りつぶせてしまい、壊れた composition が痕跡なく消える。これは [F28](findings.md) の「未導入と壊れた plugin を区別できない」と同じ失敗である。schema builder、component、definition がすべて採る「問題を保持して host 構築時に集約する」方針に揃える。

### C20. descriptor は plugin 単位で必須とし、`DisplayName` だけ継承しない

component descriptor の未設定 field は親 plugin の descriptor から引き継ぐ。family の全 component へ同じ version や license を書かせない。

ただし `DisplayName` は component ごとに異なるべき唯一の field なので継承せず、未設定なら marker 名を表示に使う。継承すると family の全 component が同じ表示名になり、catalog 上で区別できなくなる。

### C21. foundation package は media 領域だけを grouping する

media 領域を `media/` 配下へ置き、それ以外は root に置く。単独では意味が読み取れない語（`side`、`property`、`buffer`、`tag`、`stream`、`schema`）が media に集中しており、`media` は容器のための造語ではなく実在する domain 語である。

- `media/`: `schema`、`property`、`timing`、`stream`、`metadata`、`tag`、`packet`、`side`、`buffer`、`audio`、`video`、`subtitle`、`format`、`codec`
- root: `plugin`、`config`、`diagnostic`、`flow`、`access`、`endpoint`、`resource`、`job`、`host`、`testkit`

`app` や `io` のような容器語を作って `host` や `access` を沈めると `godec/host` より読みにくくなるため、全面 grouping はしない。`flow` は media 専用ではなく第三者の非 media schema にも使うので media 配下へ置かず、1 package のために `graph/` も作らない。

当初この項目が併記していた `component` package は新設しない。M2 の `plugin.Component` が既に identity、descriptor、型消去した config schema を持つため、そこへ port shape と `Open` を足す。別 package の `component.Spec` を作ると「誰が提供するか」と「何をするか」が二重管理になり、`component` が `plugin.Identity` を要求した時点で import cycle にもなる。`flow` は port shape、typed Reader/Writer、Processor/Operator、Input/Emitter を持ち、`plugin` から一方向に参照される。

この配置により `config.Schema`（設定 schema）と `media/schema`（data unit schema）、`media/audio`（frame 型）と `plugin/audio`（processor 実装）が path で区別される。M2 が作った root の 4 package は移動しない。

## Deferred without blocking the first implementation

### D1. dynamic install の方式

custom static binary、別 process plugin、platform-specific loader のどれを使うかは install/discovery product を設計する時に決める。foundation は immutable Set/descriptor/identity を用意する。

### D2. remote plugin wire protocol

in-process contract を先に安定させる。CLI/WASM/HTTP DTO を将来の RPC ABI と約束しない。

### D3. live dynamic topology の既定 policy

contract は `FiniteStatic`、`LiveStatic`、`LiveDynamic` と stream event を表現できるようにする。新 stream を follow/ignore/fail のどれにするかは live input 実装時に surface/job policy として確定する。

### D4. hardware accelerator の標準提供範囲

third-party/native variant を同じ Codec contract で追加できるようにする。公式 distribution にどの device adaptor を含めるかは pure-Go base と別に判断する。

## 更新規則

- 新しい product 判断が必要になった場合は実装を止め、確認待ちとして記録する。
- 明示的に承認された判断だけを `Confirmed` へ追加する。
- 実装詳細から生じた product 判断を暗黙に確定しない。
- `Deferred` が first implementation の public contract を変えると判明した場合は確認待ちへ昇格する。
- 設計文書が ledger と矛盾する場合は ledger の status を優先し、文書を修正する。
