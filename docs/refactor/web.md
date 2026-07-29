# JavaScript、WASM、web client

## HTTP serverの位置付け

web serverはbrowser clientとHost/wire contractを実演する非production demoである。固定された公式pluginだけを使用し、third-party plugin discovery/loadingや汎用変換serviceを提供しない。upload、temporary result、cancel、bounded resource、cleanupはdemoの暴走・残留を防ぐために維持するが、production authorization/SSRF/multi-tenant security frameworkを実装しない。

## 結論

JavaScript binding は、公式 audio plugin の一覧と現在の Go API を写した wrapper ではなく、versioned wire protocol を扱う汎用 client にする。WASM はその protocol を実装する Host distribution の一つ、example web は client の一つである。

第三者 plugin を Go/WASM binary へ static import した時、次を変更せず catalog、config editor、requested graph、Plan 表示、実行ができなければならない。

- `bindings/js` の TypeScript source
- example web の graph model と semantic validation
- server API
- foundation/core

UI は plugin が公開する inert な catalog/schema description から構築する。第三者 plugin に JavaScript UI extension の実装を要求せず、plugin 由来の任意 JavaScript、HTML、CSS を実行しない。

## 現行実装の問題

### closed catalog と Job

`bindings/js/src/types.ts` は catalog を次の固定 role に分けている。

```text
demuxers
decoders
filters
encoders
muxers
```

`ConversionSpec` も main/aux audio input、filter、codec、encoder、muxer に閉じている。video、subtitle、data、protocol Endpoint、device、bitstream filter、metadata mapping 等を追加すると TypeScript contract の編集が必要になる。

config は `Record<string, string>`、stream descriptor は `Record<string, unknown>` であり、型を開く代わりに意味と validation を失っている。plugin が Go で typed config/schema を提供しても、現在の wire surface では nested value、単位、sum type、provenance を忠実に表現できない。

### 型だけがあり runtime validation がない

`bindings/js/src/index.ts` は Go から返った JSON に `JSON.parse` して TypeScript 型を付ける。TypeScript interface は runtime boundary を検証しないため、Go/JS の version skew、欠落 field、未知 discriminator、単位変更が利用箇所で初めて壊れる。

Go の struct field 名を反映した `PipelineNode.ID` 等と、手書きの camelCase DTO が混在している。wire version、unknown field policy、schema fingerprint もない。

### 全量 memory copy

現在の API は main/aux input を `Uint8Array` として WASM へ渡し、Go 側で `[]byte` へ copy し、output を `bytes.Buffer` に全量保持してから再び `Uint8Array` で返す。

この経路は入力、Go heap、output、JS result が同時に memory を占有し、大きな media、複数 stream、長時間 job に拡張できない。browser が既に持つ `Blob`、`File`、`ReadableStream`、OPFS handle、network stream の capability も失う。

### job lifecycle

`bindings/wasm/main.go` は `context.Background()`、caller 指定 job ID、global job map、`runtime.Gosched` の busy loop で lifecycle を組み立てている。

- Host 自体の dispose がない。
- Result を呼ばない job の ownership/cleanup が曖昧である。
- callback の reentrancy と exception が state machine になっていない。
- progress queue、callback rate、page unload、worker termination の扱いが統一されていない。
- JS event loop へ制御を返すため、現行 pipeline の channel hand-off と `Gosched` に依存している。

execution island 化で channel hand-off が減れば、この偶然の scheduling 前提はさらに成立しなくなる。

### web client が第二の planner になっている

`example/web/client/src/graph/model.ts` の `compileGraph` は、port 接続数、cycle、required port、source/sink、topological order、main/aux mapping を独自に検査し、現在の `ConversionSpec` へ変換する。

これは Host の graph validator/planner と重複し、すでに次の固有前提を持つ。

- node kind は source/filter/output
- source output と output input は固定 port
- output fan-out は不可で、mixer を要求
- primary audio source を必須とする
- sink は単一
- filter descriptor の port だけを扱う

将来 Host が typed video/subtitle schema、tee、multi-output、Endpoint、dynamic topology を受け入れても、client 側が先に拒否し得る。

### 保存 graph が catalog snapshot を埋め込む

`FilterData` は `FilterEntry` descriptor 全体を `localStorage` の graph に保存する。plugin upgrade、config schema/port shape の変更、plugin の削除・追加後も古い descriptor が残り、現在の Host catalog と一致しない graph を client が valid と判断できる。

graph version の検査は nodes/edges が配列かを見るだけで、field validation、migration、catalog binding、未知 node の preservation がない。

### presentation が plugin identity に固定される

filter category、表示名、色は client の `FILTER_ROLE_BY_NAME` 等へ手作業で追加される。未知 filter は動作上 fallback できるが、第三者 component が同等の検索・分類・説明 UX を得るには client の変更が必要になる。

presentation metadata を plugin contract へ無制限に入れて任意 UI code を実行するのも適切ではない。意味上の schema と、信用境界を越えて表示できる inert な hint を分ける必要がある。

## distribution と依存境界

monorepo 内では次の責務を分ける。

```text
wire schema/fixtures
        |
        +---- Go DTO/codec ---- bindings/wasm ---- standard WASM artifact
        |
        +---- TS DTO/codec ---- JavaScript protocol client
                                      |
                                      +---- example web client
```

- wire schema と compatibility fixture は一つの source of truth とする。
- JavaScript client は特定の公式 plugin 一覧を知らない。
- standard WASM artifact は `standard.Set()` を composition した公式 distribution とする。
- custom distribution は第三者 plugin を import した別 WASM artifact を作り、同じ protocol client を再利用する。
- example web client は private application であり、protocol や planner の正本にしない。

JavaScript protocol client と standard WASM artifact を同じ npm package で配布することはできるが、内部責務と version を区別する。custom artifact を使うために client source を fork させない。package 名は実装時に npm の移行案と合わせて決める。

`bindings/wasm` は browser という独立 build target/artifact のため、monorepo 内の nested Go module にする合理性がある。foundation や公式 plugin を repository/submodule として分離する理由にはならない。

## wire contract

### 一つの versioned schema

wire contract は Go live object や TypeScript interface のどちらかを暗黙の正本にせず、明示 version を持つ language-neutral schema と compatibility fixture を正本にする。そこから Go/TypeScript の DTO、decoder、runtime validator を生成するか、両実装を同じ fixture で検証する。

generator を使う場合は、version を pin し、deterministic output、golden、生成先 compile/typecheck、checked-in artifact drift を CI で検査する。config functional-option generator のように domain semantics を二重生成するものではなく、wire の機械的 codec に限定する。

envelope は少なくとも次を持つ。

```text
protocolVersion
messageKind
requestID
payload
```

- discriminator と field casing を固定する。
- unknown field、unknown message、unknown enum/tag の policy を version ごとに定義する。
- duration、timestamp、byte size、rate の単位を field 名または型で固定する。
- depth、collection count、string、binary、diagnostic の size limit を decode 前後で守る。
- error は message string だけでなく stable code、field/node/stream path、details を持つ。
- TypeScript 側も untrusted JSON を runtime validate し、型 assertion だけで通さない。

### open catalog

catalog は role ごとの固定配列でなく、open component/schema の集合とする。

```text
CatalogDTO {
  protocolVersion
  fingerprint
  components[]
  schemas[]
  presets[]
  hostCapabilities
}

ComponentDTO {
  selector
  aliases[]
  kind
  ports[]
  configSchema
  variants[]
  presentation
  provenance
}
```

`kind` は検索・表示 hint であり、Host の実行可能性を閉じた enum だけで決めない。port は identity、direction、schema、multiplicity、timing/ordering requirement、shape source を記述する。第三者 schema は stable wire identity と、UI が解釈できなくても表示・接続可否を判断できる summary を持つ。

catalog fingerprint は Host composition、component/schema version、surface-relevant descriptor から作る。server と WASM は別 Host であり、同じ catalog を持つと仮定しない。

### presentation

plugin が提供できる presentation は inert な data に限定する。

- title、short description、documentation key
- category/tag
- config field の group/order
- unit、editor hint、choice label
- host が用意した icon token
- schema/port の表示 label

任意 HTML、script、CSS、remote URL を catalog から実行・挿入しない。すべて plain text と allowlist token として扱い、長さを制限する。presentation がなくても generic renderer で全機能へ到達できることを conformance test にする。

audio waveform、subtitle timeline、video preview 等の高度な editor は application 側の optional schema adapter として追加できる。ただし adapter がなくても unknown schema/component を graph へ保持し、Host へ送れる。

## requested graph editor

### 保存するもの

保存 graph は catalog descriptor の snapshot ではなく、利用者の intent だけを持つ。

```text
GraphDocument {
  version
  catalogFingerprintAtSave
  nodes[] {
    nodeID
    componentSelector
    configPatch
    position
    label
  }
  edges[] {
    fromNode
    fromPort
    toNode
    toPort
  }
  endpointRequests[]
  mapping
  policy
}
```

resolved descriptor、default 展開値、runtime Plan、progress は保存しない。default は現在の schema から再解決し、明示値だけを patch として保存する。

読み込み時は現在の catalog へ再 binding する。

1. canonical selector と schema/config version を照合する。
2. compatible alias/migration が一意なら候補として提示する。
3. component/port/field が存在しなければ node を削除せず unresolved placeholder として保持する。
4. migration が意味を変える場合は黙って適用せず warning と差分を表示する。
5. 保存時 catalog fingerprint と現在値が異なることを表示する。

### validation の正本

client は操作中の即時 feedback として、catalog から機械的に分かる範囲だけを lint できる。

- dangling node/edge
- duplicate node ID
- 既知 port の direction/multiplicity
- config wire shape

schema compatibility、dynamic shape、mapping、bridge insertion、resource、policy、stream topology、Endpoint capability、cycle semantics の最終判断は `Host.Prepare/Plan` が行う。client は独自の `compileGraph` で別の semantic rule を実装しない。

editor は requested graph と Host が返した resolved Plan を並べて表示する。auto-insert node を requested graph へ暗黙保存しない。

### open node model

`source/filter/output` を wire/editor model の閉じた union にしない。Access reference、Endpoint request、component node、sink request を open descriptor で表し、UI は catalog/schema trait に応じた affordance を選ぶ。

標準 web application が「音声変換」用の簡単な source/filter/output view を提供することは問題ない。ただしそれは open GraphDocument への projection であり、未知 component を消したり Host へ送れなくしたりしない。

## browser I/O と lifecycle

### handle

概念上の API:

```text
createHost(options)                       -> Promise<HostHandle>
host.catalog()                            -> Promise<CatalogDTO>
host.bindInput(Blob | ReadableStream | ReferenceDTO)
host.bindOutput(WritableStream | OPFSHandle | memory policy)
host.plan(JobDTO, AbortSignal?)           -> Promise<PlanDTO>
host.start(JobDTO, AbortSignal?)          -> JobHandle
job.events()                              -> AsyncIterable<EventDTO>
job.result()                              -> Promise<ResultDTO>
job.cancel(reason?)
job.dispose()
host.dispose()
```

- handle ID は binding が生成し、caller の文字列衝突に依存しない。
- state transition を HostStarting/Ready/Running/Finishing/Succeeded/Failed/Canceled/Disposed として検証する。
- cancel/dispose は idempotent にする。
- callback exception は Go panic や orphan job に変換せず、subscription failure と job lifecycle を分ける。
- worker termination/page unload は全 handle を cancel/dispose する。
- result を取得しない job も explicit dispose または Host policy の TTL で解放される。

### streaming

`Uint8Array` の convenience は小さな input/output 用に残せるが、primary path にしない。

- `Blob`/`File` は slice/range read を共有する。
- transferable `ReadableStream`/`WritableStream` が使える環境では worker 境界で利用する。
- transfer 不可の fallback は bounded chunk protocol と backpressure を持つ。
- seekable input が必要なら Blob/OPFS の random read または policy-controlled spool を使う。
- output は stream、Blob/OPFS reference、small-memory result を選べる。
- JS↔WASM の copy 回数、peak JS heap、WASM heap、in-flight chunk 数を計測する。

browser adaptor は local path や任意 network authority を仮定しない。`fetch` を使う場合も、browser/caller が取得して渡した stream/reference または application が composition した Provider を使う。

## server/client backend

server backend と WASM backend は同じ TypeScript interface を実装できるが、同じ capability、catalog、resource limit、result transport を持つとは限らない。

接続時に protocol version、catalog fingerprint、host capability、limit を handshake する。graph を backend 切替後にそのまま「valid」とせず、切替先 catalog へ再 binding して Plan を取り直す。

HTTP は upload/result を JSON base64 にせず、stream/multipart/object reference と Job DTO を分離する。event は cursor と bounded replay/loss policy を持つ。client は SSE/WebSocket/worker event を共通 `EventDTO` へ正規化するだけで、progress を再計算しない。

## build と release

現在の build は `gowasm-bindgen@v1.1.0` を command 上で pin している一方、TinyGo executable/version と生成 worker/runtime の組み合わせを artifact metadata に固定していない。

目標:

- Bun/TypeScript/TinyGo/Go/bindgen の version と source digest を repository manifest/lock に固定
- generated Go/TS/worker file の source と command を annotation
- clean checkout で生成し、差分なしを CI で検査
- standard WASM、worker、JS、type/validator、license/SBOM の digest を一つの release manifest に記録
- generic client protocol version と standard artifact compatibility を package metadata に記録
- test output や local profile を source artifact に混入させない
- source map/debug artifact の公開 policy を明示

generated file を checked in するかは build availability と review UX で決められるが、正本は wire schema/Go source/build recipe であり、生成物を手修正しない。

## test

### wire

- Go encode → TypeScript decode と逆方向の golden
- supported version、future unknown field、unknown discriminator
- integer/float limit、duration/timestamp unit、large/deep payload rejection
- diagnostic path と redaction
- catalog fingerprint と canonical ordering

### plugin openness

third-party 相当 fixture として、新しい component kind、video/subtitle/独自 schema、nested custom config、可変 port を追加する。

- JavaScript client source を変更せず catalog を decode できる。
- generic editor が node/port/config を表示・保存・再読込できる。
- server/WASM の Plan を表示できる。
- presentation metadata がなくても機能を失わない。
- malicious/oversized display metadata が HTML/script/CSS として実行されない。

### graph persistence

- catalog version upgrade
- component/port rename
- plugin removal/reinstall
- alias collision
- unknown node/field preservation
- explicit migration と拒否
- server/WASM backend 切替

### browser

- concurrent Host/job
- cancel、dispose、double dispose、Result 未取得
- callback exception/reentrancy
- page unload/worker crash
- large Blob、chunked stream、slow sink、backpressure
- WASM memory limit、quota、spool failure
- real browser の worker/main-thread差

## performance

JSON/schema decode は catalog/Plan/event の control plane に限定する。media chunk ごとに Job DTO、config schema、catalog、string map を再解釈しない。

- binary を JSON/base64/string に変換しない。
- transferable と bounded chunk pool を優先する。
- progress event は batch/sampling し、frame ごとの postMessage を行わない。
- graph editor の catalog index は fingerprint ごとに一度構築する。
- large descriptor/artwork/Plan は immutable reference と pagination/summary を使う。

## 完了条件

- JavaScript catalog が固定 demuxer/decoder/filter/encoder/muxer 配列に閉じていない。
- Job/graph が audio main/aux、単一 sink、固定 role を必須にしない。
- TypeScript と Go の wire version、runtime validation、compatibility fixture が一つの契約を共有する。
- graph persistence が live descriptor を埋め込まず、selector/config patch と catalog fingerprint を保存する。
- semantic graph validation と auto insertion は Host に一元化される。
- third-party component/schema の追加に JavaScript/web client の変更を要求しない。
- plugin presentation がない場合も generic UI を利用でき、任意 plugin UI code を実行しない。
- server/WASM backend ごとの catalog/capability差を handshake と再 binding で扱う。
- large input/output の primary path が全量 `Uint8Array`/`bytes.Buffer` copy を要求しない。
- Host/Job handle、AbortSignal、event、result、dispose の状態機械が明示される。
- WASM/npm artifact から source、toolchain、dependency、license、wire version を追跡できる。
