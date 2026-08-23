# 利用 surface

## product boundary

primary productは開発者がGo applicationへ組み込むlibraryである。CLIとWASMは公式surfaceにできるが、HTTP serverは同じHost APIを示す小さなdemo/referenceであり、production変換serviceとして配布・supportしない。

demo serverは固定された公式plugin、upload/temporary output、基本的なsize/concurrency/cancel/cleanupに限定する。third-party plugin loading、汎用URL fetch、multi-tenant authorization、production hardeningは要件にしない。HTTP DTOを将来のstable RPC ABIとも約束しない。

## Job model

単一 input、単一 audio stream、暗黙の出力一つを前提にした conversion API を置き換える。

```go
type Job struct {
    Inputs  []Input
    Outputs []Output
    Maps    []Mapping
    Policy  Policy
}
```

### Input

- Reference、owned/borrowed Source、typed Endpoint の tagged choice
- format hint
- Access Provider/config と I/O capability policy
- auxiliary named input
- probe budget
- input-specific options

### Output

- Reference、transactional Sink、typed Endpoint の tagged choice
- format/codec request
- transactional output policy
- metadata policy
- output-specific options

Reference/byte object と RTSP/HLS/device 等の session Endpoint を同じ `io.Reader` にしない。direct reader/writer は ownership を `Own`/`Borrow` で明示する。詳細は [access と endpoint contract](access.md) に記載する。

### Mapping

stream selector と output stream の関係を明示する。

- input index
- program/stream selector
- media/schema constraint
- language/disposition selector
- copy/transcode/filter request
- output order
- optional/required

selector は最初に canonical stream IDs へ解決し、曖昧さや未一致を compile diagnostic にする。CLI の `-map`、library の typed builder、WASM/HTTP DTO は同じ `Job` へ正規化する。

### 最短経路の convenience

`Job` の tagged choice と `access.Reference` は一般ケースに必要な表現だが、最も多い「1 file を 1 file へ変換する」利用にそれを要求しない。次の水準を M6 の成果に含める。

```go
err := standard.Convert(ctx, "in.flac", "out.wav")
```

- path から Reference への解決は convenience が行い、利用者に URL 構文を要求しない。
- `job.File(path)` 相当の短縮 constructor を用意し、`access.Parse` を必須にしない。
- convenience は `Job` を組み立てて同じ `Host` を呼ぶだけとし、別経路の planner や既定を持たない。
- 2 段目以降（codec 指定、filter、mapping、policy、custom Set）へは同じ `Job` を露出させて連続的に移行できる。

[experience](experience.md#progressive-disclosure) の 1 段目「path と output を指定」を実際に 1 行で満たすためのものであり、これが無いと利用者は最初の変換のために Host、Job、Reference、URL 構文を同時に学ぶことになる。

source acquisition、bounded probe、Inspect と実行を同じ snapshot/session で結ぶ primary API は `Host.Prepare` とする。`Host.Run` は Prepare + Run の convenience、`Host.Plan` は resource を閉じる read-only convenience とし、保存した Plan だけで後日同じ input を無検証実行できるとは約束しない。

## default behavior

設定の省略は「適当な encoder を選ぶ」ことではない。

1. source を probe/inspect する。
2. sink/extension の format request が明示されていなければ入力 format を優先する。
3. filter や schema 変更がなければ各 stream を copy する。
4. transcode が必要でも入力 codec が target format に入るなら同 codec を優先する。
5. 不可能な時だけ standard policy で codec を選び、理由を `Plan` に出す。
6. attachment、subtitle、data、unknown stream も policy に従い、黙って捨てない。

「入力と同じ」は byte-for-byte 同一とは限らない。remux により header/order が変わる場合と、raw stream copy が可能な場合を Plan で区別する。

## catalog

catalog は host 構築時に検証された immutable snapshot である。

利用者向け view:

- plugin/component identity
- alias/display name
- capability/schema
- config schema/default/preset
- inert presentation hint
- license/build provenance
- selected/overridden status
- catalog fingerprint と host capability

runtime factory、mutable manifest、reflect type は返さない。invalid entry を黙って omit せず、host 構築 error にする。

検索 alias の重複は許容できるが、CLI が一意に選べない場合は候補を表示して canonical selector を要求する。

catalog を demuxer/decoder/filter/encoder/muxer ごとの固定配列にせず、open component/schema の集合にする。presentation は plain text、category、field order、allowlist icon token 等の inert data に限定し、third-party plugin 由来の任意 JavaScript/HTML/CSS を surface が実行しない。

## CLI

CLI は host を組み立てる application library と command を分ける。

```text
cli/             parser, job normalization, rendering
cmd/godec/       standard.Set を import する公式 binary
cmd/godec-play/  Oto 等を含む optional playback binary
```

CLI layer の責務:

- args/config を `Job` に変換
- `Plan` preview
- progress/diagnostic の terminal 表示
- signal/cancel
- overwrite、dry-run、transaction policy の選択
- exit code の分類

CLI layer に planner、registry、plugin factory を持たせない。

### transactional output

file output は target と同じ filesystem 上の temporary file に書き、flush、sync、close がすべて成功した後に rename/replace する。ただし temporary object、replace、commit/abort の実装は CLI ではなく file Access Provider が所有する。CLI は確認 UX と Job policy だけを担当する。

- overwrite policy を事前に固定
- Windows の replace semantics を扱う（`os.Rename` が `MoveFileEx` の replace に写る。ACL/attribute は継承しない）
- failure/cancel では元 target を残す
- multi-output commit の部分成功を report する
- stdout/non-seekable sink では rollback 不可能であることを Plan に示す

S3 multipart 等も同じ Sink transaction contract を使う。複数 output で真の atomic commit を提供できない場合は、partial commit risk と各 output の最終状態を result に残す。

## custom distribution

通常利用者は公式 binary または `standard.NewHost()` を使う。custom plugin 利用者だけが通常の Go `main` で composition する。独自 template format や generator は不要である。

標準 bundle の階層:

- `standard/base`: transcode に必要な pure-Go plugin のみ
- `standard/audio`: 公式 audio codec/filter/format
- `standard/all`: 公式の安定 plugin
- playback/native adaptor: 明示 opt-in

実際の package 名は API 設計時に一語の明確な名前を再検討する。

## WASM

WASM binding は synchronous Go function 内で `runtime.Gosched` を回して JavaScript の制御復帰を待つ形をやめる。

JavaScript 向け API:

```text
createHost(options) -> HostHandle
plan(job)            -> Promise<PlanDTO>
start(job)           -> JobHandle
events(handle)       -> async iterator / callback
cancel(handle)
result(handle)       -> Promise<ResultDTO>
dispose(handle)
```

要件:

- job ごとの明示 handle と状態機械
- `AbortSignal` 連携
- progress/diagnostic の bounded queue
- dispose の idempotency
- input/output の全量 copy を避ける chunk/stream adaptor
- JS callback 中の reentrancy 規則
- memory limit と large-file failure の明示
- page unload 時の cancel/dispose

Go の live `Plan`、manifest、error struct をそのまま JS へ渡さず、versioned DTO に変換する。

JavaScript client は特定の standard plugin 一覧を知らない汎用 protocol client とする。standard WASM artifact と custom plugin を import した WASM artifact の両方で再利用できる。全量 `Uint8Array`/`bytes.Buffer`、Host/Job state、wire validation の詳細は [JavaScript、WASM、web client](web.md) に定義する。

## HTTP と example web

example server は非 production demo だが、process/diskを無期限に占有しない bounded lifecycle を持つ。

- upload/body size limit
- 同時 job 数と worker limit
- per-job timeout/cancel
- job/result TTL
- temporary file cleanup
- disconnect 後の retention/cancel policy
- SSE replay cursor または loss policy
- bounded event history
- filename/content-type validation
- partial output の扱い

example 固有の job store を core runtime lifecycle と混ぜない。server は `Host` の client であり、job queue/persistence は application 層に置く。

web graph editor は catalog descriptor 全体や resolved Plan を保存せず、component selector、config patch、edge、mapping、policy からなる requested graph を保存する。client の独自 `compileGraph` を semantic authority にせず、catalog 由来の即時 lint と Host の `Plan` を分ける。server/WASM backend を切り替えた時は catalog fingerprint/capability を handshake し、保存 graph を切替先へ再 binding する。

## wire protocol

CLI JSON、WASM、HTTP、将来の remote plugin は同じ Go struct をそのまま共有しない。language-neutral な versioned wire schema と compatibility fixture を設け、Go/TypeScript の DTO、codec、runtime validator を同じ契約へ結び付ける。

- stable string ID
- explicit version
- unknown field policy
- duration/timestamp の単位
- binary/blob の参照方法
- error/diagnostic code
- capability schema の表現

wire DTO から domain `Job`/`Plan` への変換は boundary で一度だけ行う。内部 property key、reflect type、function pointer、live buffer を wire に漏らさない。

TypeScript interface は runtime validation の代わりにならない。unknown field/discriminator、size/depth、version skew を boundary で検査する。完全な wire、catalog、graph persistence の規則は [JavaScript、WASM、web client](web.md) に従う。

remote plugin protocol は discovery と隔離が本当に必要になった時に別設計する。初期 wire package を将来の RPC ABI と約束しない。

## observability surface

すべての surface は同じ snapshot/event を表現だけ変えて扱う。

- plan event
- phase transition
- progress by input/output/stream
- structured diagnostic
- resource summary
- performance counters
- result and loss report

CLI は terminal renderer、WASM は DTO、HTTP は SSE/WebSocket/JSON renderer となる。surface ごとに runtime を polling して独自計算しない。

## playback

playback は transcode core の sink plugin/adaptor として実装できるが、公式 pure-Go transcode bundle の dependency にはしない。device library は platform/native 条件を持ち得るためである。

playback sink は realtime policy、clock、backpressure、underrun diagnostic を宣言し、offline file muxer と同じ成功条件を装わない。

## 将来の install/discovery

高位の manager を将来追加する余地を、次の安定点で確保する。

- canonical plugin/component identity
- descriptor/provenance
- immutable `Set`
- versioned `Plan` DTO
- optional remote boundary

manager は source repository、binary/build cache、trust policy を扱い、最終的に custom distribution または remote endpoint を選ぶ。foundation は package manager、marketplace、署名 UX を引き受けない。
