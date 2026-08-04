# package inventory

この文書は現行 package/asset/tool の移行台帳であり、進捗は管理しない。全体の状態は [checkpoint.md](checkpoint.md)、移行後の package 境界は [architecture](architecture.md) を正本とする。

この一覧は、調査時点で `core`、`sdk`、`plugins`、`cli`、`bindings`、`example`、`tools` 配下に production `.go` file を持つ directory を対象にしている。内部 package が同じ処遇になる場合は `{...}` でまとめる。

再監査した母数:

| area | production Go directories | production Go files |
|---|---:|---:|
| `bindings` | 1 | 2 |
| `cli` | 3 | 16 |
| `core` | 14 | 109 |
| `example` | 5 | 9 |
| `plugins` | 61 | 229 |
| `sdk` | 21 | 84 |
| `tools` | 13 | 25 |
| total | 118 | 474 |

加えて `bindings/js` の6個、`example/web/client` の47個のTypeScript/JavaScript source、root/npm/container/build manifest、testdata/example assetを対象にした。Go workspaceは16 moduleで、runtime/core/pluginの11 moduleが一つのrequirement graph強連結成分に入っている。

分類:

- **置換**: 責務は必要だが contract/実装を新設計で作り直す。
- **移動**: 実装の中心は再利用し、依存方向に合う package へ移す。
- **統合**: 分散した同一責務を一つへ集める。
- **維持**: API review は行うが、algorithm と package の価値を維持する。
- **非公開**: 利用者/plugin author の contract ではないため `internal` にする。
- **削除**: 新設計で責務が消えるか、標準 library/別 package で十分である。

## core

| 現在 | 分類 | 目標 |
|---|---|---|
| `core/core.go` | 置換 | global default registry/factory façade を削除し、`host.New` と明示 `plugin.Set` にする |
| `core/domain/manifest` | 分割・置換 | `component`、`format`、`codec`、`config`、`property` の小 contract へ分ける |
| `core/domain/media` | 分割・置換 | `schema`、`stream`、`packet`、`audio`、`timing`、`side`、`buffer` へ分ける。package-global packet/audio-frame/byte poolはHost/Job allocatorとinstance workspaceへ移す |
| `core/domain/media/pcm` | 移動 | format validation/pack/unpack を `audio`/`dsp` 側へ集約し、core→SDK 依存をなくす |
| `core/domain/metadata` | 置換 | open `metadata.Document` と `tag` vocabulary。閉じた Bundle/key method は削除 |
| `core/domain/time` | 置換 | `timing`。integer timestamp、time base、checked rescale を完成させる |
| `core/factory` | 削除 | candidate の runtime factory 試し起動を `Compile`/`Open` と Host composition へ置換 |
| `core/internal/clone` | 削除 | reflection clone を immutable value、builder、明示 Clone へ置換 |
| `core/internal/xsync` | 非公開・再評価 | catalog freeze 後にも必要な用途だけ残す。generic global map として公開しない |
| `core/node` | 置換 | public `flow` contract。typed schema port と Processor/Operator を提供 |
| `core/pipeline` | 非公開・置換 | `internal/graph`、`run`、`observe`。channel-per-node を execution island へ置換 |
| `core/registry` | 分割・置換 | marker identity/Set は `plugin`、validated index は `internal/catalog` |
| `core/resolver` | 非公開・置換 | pure Compile を評価する `internal/solve` と shared bounded `internal/probe`。候補ごとの `Seek(0)` を削除 |
| `core/routing` | 非公開・置換 | `internal/plan`/`graph`。negotiation と runtime object 生成を分離 |
| `core/test` | 移動 | 公式 plugin 横断 test を最上位 `integration` module へ移す |

`core` の package は既存名を rename して残すのではなく、foundation contract の縦断経路を新設した後に旧 package ごと削除する。

## SDK

| 現在 | 分類 | 目標 |
|---|---|---|
| `sdk/audio` | 移動・置換 | foundation `audio` の typed sample frame/authoring utility。filter ごとの byte↔float conversion と Source alias をなくし、schema region 単位の converter、in-place/COW ownership を導入 |
| `sdk/bits` | 維持・移動 | FLAC/MP3 等から再利用する public bitstream utility。boundary checked/validated private fast pathへ分け、undocumented `production` tagでassertionを消す二重semanticsは廃止 |
| `sdk/buffer` | 分割 | generic `Ring` は reusable utility として維持候補。旧 engine の EAGAIN/Retainer に結合した `Queue`/`Slot` は削除し、host queue/operator-local storage へ置換 |
| `sdk/catalog` | 置換 | immutable `host.Catalog` view。live registry/manifest を返さない |
| `sdk/cliflag` | 分割・置換 | assignment syntax は `cli/internal`、field decode/describe は foundation `config.Schema` の surface projection へ。任意 struct reflection binder は削除 |
| `sdk/config` | 統合・置換 | foundation `config` の Schema/Patch/Resolved/canonicalization。string map と CLI reflection への依存を切る |
| `sdk/conversion` | 統合・置換 | foundation の `job` と `host` façade。`InputSet{io.ReadSeeker}`/`BuildPlayback` を typed Access/Endpoint と Prepared Job に置換 |
| `sdk/date` | 移動 | `tag.Date`。metadata vocabulary の値型として部分日付を表す |
| `sdk/dsp`、`sdk/dsp/fft` | 維持・移動 | public algorithm utility。reference/scalar/optimized variant を分離し、exact/bounded contract と代表 benchmark を持つ。小さな差を採用根拠にする場合だけ paired comparison を行う。exported mutable CPU feature global は削除し、Host の immutable snapshot から Compile 時に選択 |
| `sdk/engine` | 削除 | core と並立する Send/Receive/EAGAIN abstraction を Processor/Operator へ置換 |
| `sdk/hash` | 分割・削除 | CRC8/16 は FLAC polynomial 固有なので `plugin/flac/internal/crc` へ移す。未使用 FNV は削除 |
| `sdk/optional` | 削除 | 現在は `date` のみ。`tag.Date` の presence mask または `(value, ok)` API に内包し、panic する `Unwrap` を公開しない |
| `sdk/parallel` | 移動・非公開 | 現在は FLAC の ordered worker completion 専用。FLAC internal または host task primitive へ置き、汎用 SDK API にしない。auto worker は Plan 前に解決し、順序/reduction contract を variant に記録 |
| `sdk/pool` | 分割・非公開 | foundation buffer allocator/internal memory と plugin workspace に分ける。process-global size-class `sync.Pool`をpublic contract/resource managerにせず、Host/Job grant、bounded worker/instance workspace、zeroed/complete-overwrite leaseへ置換 |
| `sdk/profiling` | 移動 | CLI/example の file collision helper なので surface internal へ置く |
| `sdk/testutil`、`sdk/testutil/audio`、`nodes`、`pcm` | 分割 | public conformance `testkit` と、公式 plugin を import する `integration` fixture へ分ける |
| `sdk/timer` | 削除 | example 一箇所のみ。`time.Now`/`time.Since` を直接使う |

`sdk` という独立した第二 architecture layer は最終的に残さない。第三者向け utility は foundation module の明確な package として置き、host 内部 utility は `internal` にする。stable module split後の公式familyもfoundationのpublic packageだけへ依存し、同じrepository path配下であることを理由にfoundation `internal`をimportしない。

## official plugins

plugin の数値 algorithm は優先度を下げ、まず contract と依存方向を置換する。algorithm を移動する際も package の凝集度を上げ、codec/format 間の内部都合だけで public API を増やさない。

| 現在 | 分類 | 目標 |
|---|---|---|
| `plugins/codec-flac` | 統合 | `plugin/flac/internal/codec`。親`flac`がpublic component definitionを提供し、encoder/decoder algorithm、SIMD、workspaceは非公開に維持 |
| `plugins/codec-flac/internal/config` | 分割・統合 | public user config/schema と private compile 済み algorithm config を分ける。generated option は削除 |
| `plugins/codec-flac/internal/{decoder,encoder,flac}` | 維持・非公開 | pure-Go algorithm と並列実装を維持。integer exact path と FMA bounded-difference path を別 variant contract とし、Host task/resource/typed audio contract に接続 |
| `plugins/format-flac` | 統合 | 同じ`plugin/flac`親のinternal format/parser実装。親が別componentとして公開し、codec contractとは分離 |
| `plugins/format-flac/{frame,seektable,streaminfo}` | 再評価 | parser/format/codec 間で共有する型は同 module に凝集。第三者 contract でなければ internal 化 |
| `plugins/format-flac/internal` | 維持・置換 | demux/mux/probe logic を Format/Carrier/Parser/Finalize contract へ接続 |
| `plugins/codec-mp3` | 統合 | `plugin/mp3/internal/codec`。親`mp3`がpublic component definitionを提供し、decode algorithmは非公開に維持 |
| `plugins/codec-mp3/internal/{domain,mp3,mp3/domain,mp3/layer12,mp3/layer3}` | 維持・非公開 | algorithm/domain を plugin internal に凝集し、generic media domain と混同しない |
| `plugins/format-mp3` | 統合 | 同じ`plugin/mp3`親のinternal elementary format/parser実装。親が別componentとして公開し、ID3 encodingを直接所有しない |
| `plugins/format-mp3/{header,scan}` | 再評価 | Parser 実装として同 module に凝集。外部 parser author に必要な面だけ公開 |
| `plugins/format-mp3/internal` | 維持・置換 | probe/demux/mux/parser を新 lifecycle へ接続 |
| `plugins/codec-pcm` | 維持・統合 | `plugin/pcm`。PCM、ADPCM、G.711 component を同一規格 family として提供 |
| `plugins/codec-pcm/internal/{adpcm/bits,adpcm/ima,adpcm/ms,g711}` | 維持・非公開 | algorithm 固有 utility と table を維持。host/plugin contract を import させない層を作る |
| `plugins/format-wav` | 移動 | `plugin/wave` Format。PCM/MP3/ADPCM の直接 import/list を Binding へ移す |
| `plugins/format-wav/internal` | 維持・置換 | RIFF chunk parse/write、probe、carrier の責務へ集中 |
| `plugins/format-wav/params` | 移動・再評価 | WAVE carrier property と codec Binding parameter。WAVE 固有 public contract のみ残す |
| `plugins/filter-audio` | 移動 | `plugin/audio` の typed `audio.Frame` processors。旧 `engine.FilterEngine` wrapper は削除 |
| `plugins/filter-audio/internal/config` | 分割・統合 | processor ごとの typed config/schema に分け、string list/dynamic slot を nested/repeated value に置換 |
| `plugins/filter-audio/internal/{compressor,convert,convolver,dcoffset,delay,equalizer,fade,gain,gate,linear,mixer,normalize,remix,resample,retime,reverb,trim}` | 維持・接続変更 | 数学的 algorithm は原則維持。typed frame/block、variant の数値・chunk/reduction contract、Host task/resource に接続 |
| `plugins/metadata-id3` | 移動 | `plugin/id3` Metadata Encoding。Document/common vocabulary との parse/marshal mapping を提供 |
| `plugins/metadata-id3/{id3v1,id3v2,internal/id3text}` | 維持・整理 | version 固有 byte encoding は凝集。Carrier を知らない実装にする |
| `plugins/metadata-vorbiscomment` | 移動 | `plugin/vorbiscomment` Metadata Encoding。Vorbis codec と曖昧になる一語化を避け、FLAC 等の format を import しない |

各 plugin root の `register.go` は副作用登録をやめ、marker identity と component definition を返す `plugin.go` に置換する。`config_options.go` と config 用 `go:generate` directive は削除し、typed config struct と schema を責務別 file に置く。

### plugin test directory

| 現在 | 分類 | 目標 |
|---|---|---|
| `plugins/*/test/config` | 統合 | testkit の config fixture。production package と同じ validation を通す |
| `plugins/codec-flac/test` | 分割 | small codec fixture は plugin、format との roundtrip は integration。full conformance corpus は任意取得のdata submoduleとして維持し、通常testとrequired conformance testを明示的に分ける |
| `plugins/codec-mp3/test/minimp3` | test-only 維持 | reference adaptor として optional/native test 境界に置く。公式 production graphへ含めない |
| `plugins/*/test/snapshot-generator` | 移動 | review 対象 artifact を作る integration/tool command。production module に含めない |
| `plugins/codec-pcm/test/testdata` | 置換 | 300 MB 超の source/decimal sample snapshot を procedural small vector、streaming digest、metric、failure diff へ置換。origin/license を manifest 化 |
| `plugins/codec-mp3/test/testdata` | 分割 | small reference vector は plugin、full/native comparison corpus は任意取得のdata submoduleまたはintegration manifest/cache |
| `core/test/assets` | 移動・重複削除 | foundation から公式 WAVE/PCM asset dependency を除去し、cross-plugin fixture は integration へ移す |

## CLI、bindings、examples

| 現在 | 分類 | 目標 |
|---|---|---|
| `cli` | 維持・境界変更 | `Host` を注入される CLI library。standard plugin を直接 import しない |
| `cli/internal` | 置換・整理 | args→Job、Plan/diagnostic rendering、overwrite/policy UX。resolver/catalog と file transaction 実装は Host/Access Provider へ移す |
| `cli/internal/play` | 分離・置換 | Oto typed Endpoint/専用 command。`PlaybackSink` の別 pipeline を削除し、pure-Go standard transcode dependency から外す |
| `bindings/wasm` | 置換 | Promise/JobHandle/AbortSignal/chunk Access adaptor と versioned DTO。全量 `[]byte`/`bytes.Buffer` を必須にしない |
| `bindings/js` | 置換 | 固定 role/standard plugin を知らない versioned protocol client。language-neutral wire schema から DTO/runtime validator を得て、standard/custom WASM artifact を差し替え可能にする |
| `bindings/js/src/generated` | 生成境界 | wire/binding generator と TinyGo version を pin し、deterministic drift/typecheck/browser test を行う。生成物を protocol の正本や手修正箇所にしない |
| `bindings/js/scripts/build.ts` | 置換 | `go run ...@version` と未固定TinyGoへのhidden dependencyをroot build manifest/Go tool directiveへ移し、fetch済みtoolだけでWASM/workerをbuild |
| `example/go` | 置換 | `standard.NewHost()` の最小 library example。独自 timer/profiling package を不要にする |
| `example/web/server` | 維持・境界変更 | Host client application。job store、quota、TTL は application layer |
| `example/web/server/internal/api` | 置換 | versioned DTO、bounded input、disconnect cancel、SSE loss policy |
| `example/web/server/internal/jobs` | 維持・強化 | concurrency/TTL/cleanup を持つ application store。core lifecycle を複製しない |
| `example/web/server/internal/testutil` | test-only | web fixture として example 内に維持 |
| `example/web/client` | 維持・置換 | catalog-driven Host client。server/WASM capability handshake、requested/resolved graph 表示、generic schema editor を持ち、audio-only wire model を所有しない |
| `example/web/client/src/graph` | 置換 | descriptor snapshot と独自 semantic `compileGraph` を削除。selector/config patch/catalog fingerprint を保存し、Host Plan を正本にして unresolved node と migration を扱う |
| `example/web/client/src/conversion/backend` | 維持・整理 | server/worker transport を共通 Event/Plan/Result DTO へ正規化する adaptor。progress/planning semantics を再実装しない |
| `example/assets`、`example/web/assets` | 維持・整理 | 同一asset repositoryを各exampleが独立して取得できるdata submoduleとして維持できる。固定revisionの関係を明示し、web buildも同じ検証済みsourceを使い、大容量demoはoptionalにする |
| `example/web/{client,server}/Dockerfile` | 置換 | root monorepo context、frozen Bun lock、current Go source、digest-pinned base、verified local assetでhermetic build。remote Git `ADD`とfloating tagを使わない |
| `example/web/compose.yaml`、`Makefile`、`scripts` | 整理 | rootのdocumented dev/build commandへ接続し、platform固有helperは薄いadaptorにする |

## tools

| 現在 | 分類 | 目標 |
|---|---|---|
| `tools/cmd/config-generator` | 削除 | typed schema と重複する functional option generator を廃止 |
| `tools/internal/config-generator/{generator,parser,types}` | 削除 | 単一 preset bug は確認済み。移行中に再実行する場合だけ最小修正し、移行完了後は残さない |
| `tools/cmd/enum-generator` | 維持 | string enum generation。出力の deterministic/golden/compile test を持つ |
| `tools/internal/enum-generator/generator` | 維持 | deterministic output と compile test |
| `tools/internal/enumscan` | 統合・再評価 | config generator 削除後に enum generator だけが使うなら、その internal package へ統合 |
| `tools/pkg/table-generator` | 維持・命名再評価 | plugin の `go:generate` から使う cross-module utility。生成元/provenance を付ける |
| `tools/cmd/generate` | 維持・強化 | bounded deterministic repository generator runner |
| `tools/cmd/test-runner` | 維持・修正 | Scanner 上限、parse failure、child cancel、structured result を修正 |
| `tools/cmd/bulk` | 置換後削除候補 | 多数 repository/module の version 操作は monorepo release plan へ統合。残る一般操作だけ workspace tool にする |
| `tools/internal/workspace` | 簡素化 | nested module matrix の列挙に限定。source submodule/repository 横断の複雑性を除く。data submoduleの取得はtest/demo tier側で扱う |
| `tools/internal/cli` | 維持・非公開 | tool command 共通 error/exit helper |
| root `package.json`/`bun.lock` | 維持・整理 | JavaScript workspace と toolchain lock の正本。Go/WASM release manifest と npm artifact provenance へ接続 |
| root build/tool manifest | 新設 | Go/TinyGo/Bun/TypeScript、external reference tool、container base、artifact input/output、license policyを固定 |

## package を新設するもの

現行 directory の移動だけでは得られないため、新設する。path は [C21](decisions.md#c21-foundation-package-は-media-領域だけを-grouping-する) の grouping に従う。`担当` 列は新設する milestone であり、その後の拡張は別 milestone が行う。

| path | 責務 | 担当 |
|---|---|---|
| `diagnostic` | warning、loss、structured error | M2 完了 |
| `config` | typed Schema、Patch、Resolved、field Codec、canonical fingerprint | M2 完了 |
| `plugin` | marker identity、descriptor、immutable Set、component Spec | M2 完了（Spec は M3） |
| `host` | public Catalog/Plan/Run façade | M2 完了（Plan/Run は M4/M5） |
| `media/schema` | open typed schema、trait、typed runtime factory | M3 |
| `media/property` | immutable open property key/set | M3 |
| `media/timing` | integer time base、typed PTS/DTS/duration、checked rescale | M3 |
| `media/stream` | open descriptor、program/stream scope、dynamic event | M3 |
| `media/packet` | container chunk と codec packet | M3 |
| `media/buffer` | aligned backing buffer、plane layout、ownership handle | M3 |
| `media/audio` | typed sample frame `Frame[S]` | M3 |
| `media/metadata` | Document、Origin、RawBlock、Mapping、metadata Binding | M3 |
| `media/tag` | shared semantic vocabulary | M3 |
| `media/format` | Probe/Inspect/Carrier contract | M3 |
| `media/codec` | Parser、Parameters、container codec Binding | M3 |
| `media/side` | packet/frame side data | M3 |
| `flow` | typed port、Reader/Writer、Processor/Operator、Input/Emitter | M3 |
| `access` | Reference、Provider、byte Source/Sink capability、ownership、transaction | M3 |
| `endpoint` | realtime/session/device clock、topology、backpressure trait | M3 |
| `media/video`、`media/subtitle` | typed frame/cue | 実 consumer を持つ milestone |
| `resource` | coarse Request/Grant | M4/M5 |
| `job` | normalized source/sink/mapping/policy | M4 |
| `testkit` | public plugin conformance | M6（最小形）、M10（完成） |
| `standard` | official component と codec/metadata Binding の composition | M6 |
| `integration` | cross-module、reference adaptor、surface end-to-end test | M6 |
| `integration/corpus` | external conformance/benchmark corpus の data submodule または manifest/cache、license、size、任意取得の test tier | M10 |
| `internal/{catalog,marker,snapshot,access,probe,inspect,plan,solve,graph,run,memory,task,commit,observe}` | host implementation | `catalog` は M2、`marker`/`snapshot` は M3 完了、他は M4/M5 |

`component` package は新設しない。component Spec は `plugin.Component` が持つ。理由は [C21](decisions.md#c21-foundation-package-は-media-領域だけを-grouping-する) を参照する。

## M1 で使う棚卸し条件

- 旧 repository/submodule の product source と履歴が monorepo に揃い、欠落した package、generator source、build manifest がない。
- 現行 production/test/tool/surface package が、この文書の移行先または削除対象のいずれかへ割り当てられている。
- 公式 plugin は M1 で確定する `plugin/<family>` path へ配置し、同じ family 内の Codec/Format/Parser 実装を後から `internal` へ再編しても公開 import path と marker identity が変わらない。
- M2以降へ残す責務再編・直接import・旧API削除を未完了事項として列挙し、M1完了と取り違えない。

## 文書全体の移行完了条件

この節は棚卸し対象をすべて新 architecture へ移し終えた時の gate であり、M1 単独には適用しない。

- 現行 production package は上記のいずれかへ割り当てられている。
- `sdk`、旧 factory/resolver/routing/registry、global façade は互換層として残らない。
- reusable algorithm と host-specific mechanism を同じ「SDK utility」に混在させない。
- plugin の数学的 algorithm は contract 置換と独立して検証できる。
- test/native/reference tool は production dependency graph に入らない。
- public subpackage は第三者が利用する明確な contract がある場合だけ残る。
- full corpus と巨大 snapshot が product module zip/通常 test に含まれず、同一 example asset が重複保存されない。
- manifest外tool、floating image、remote asset downloadがrelease buildに残らず、artifactのsource/tool/license/digestを追跡できる。
