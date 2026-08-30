# media と metadata

## 二つの層

media model は、plan を作る control plane と、packet/frame を運ぶ data plane を分ける。

Control plane:

- stream topology
- format/codec/schema descriptor
- time base
- metadata document
- property と capability
- component requirement

Data plane:

- encoded packet/chunk
- decoded audio/video/subtitle/custom unit
- timed event
- buffer ownership
- PTS/DTS/duration

control plane では open property set や一部 type erasure を許容する。data plane では具体型と compile 済み edge を使う。

この分離は概念の説明ではなく package 依存の制約である。data plane の package は control plane の package を import しない。両者が共有するのは `media/buffer`、`media/timing`、`media/key` だけであり、いずれも `internal/{marker,snapshot}` にしか依存しない。詳細は [architecture](architecture.md#foundation-内部の依存方向) を正本とする。

## open typed schema

現在の `LinkAny` は `Packet` と `Frame` に分岐し、`MediaAttributes.Video` は未実装である。この形で stream kind を増やすたびに core を編集すると、第三者拡張という目標を満たせない。

schema を値型として登録し、port は schema の具体型で結ぶ。

現行の schema 宣言と typed port の構築は
[schema の `ExampleDefine`](../../media/schema/example_test.go) と
[flow の `ExampleNewShape`](../../flow/example_test.go) を正本とする。

第三者も同じ仕組みで独自 unit を宣言できる。

非 audio payload も同じ `schema.Define` を使う。第三者 payload を通す実行例は
[schema の `ExampleDefine`](../../media/schema/example_test.go)、audio の具体例は
[audio の `ExampleNewFrame`](../../media/audio/example_test.go) を正本とする。

`schema.Type[T]` は identity と runtime traits を持つ。traits は fan-out、drop、queue accounting、timestamp scheduling が必要な時だけ呼ぶ。観測や byte limit が無効な linear path では `Size` を毎回計算しない。

万能な `Frame` interface は廃止し、`audio.Frame`、`video.Frame`、`subtitle.Cue` を別 schema にする。generic filter は、受け入れる schema constraint を明示するか、schema ごとの implementation を登録する。

### 現行 `Frame` の監査結果

現行 `core/domain/media.Frame` が共通化する操作は `Retain`、`Release`、`Pts` だけである。意味的な処理をする production code は `*media.AudioFrame` へ type assertion しており、audio filter、PCM/FLAC encoder、playback、観測処理等で型不一致を runtime error にしている。`core/pipeline.LinkAny` も `*Packet` と `Frame` の二種類を hard-code する。

したがって現在の `Frame` は複数 media の有用な抽象ではなく、「audio 型を interface の後ろへ隠している」状態である。これを video/subtitle に拡張すると、接続自体は compile できても各 filter が runtime に拒否する API になる。

### 全データ型に共通する処理

次の処理は schema を問わず必要になる。

- tee/split と fan-out
- discard/null sink
- queue、buffer、backpressure
- passthrough
- progress、metrics、trace
- cancel 時の drain/drop

これらは payload の意味を変える filter ではなく、host の搬送処理である。schema の `Fork`、`Drop`、`Size`、`Timing` trait を使って型別 operator を構築する。

timestamp shift、trim、segment、rate control は全型へ無条件に適用しない。audio の PTS は sample、video は frame duration/reordering/keyframe、subtitle は開始・終了区間を持ち、同じ操作名でも正しい semantics が異なる。component が対応 schema と timing rule を明示する。

### Go で control plane だけを型消去する方法

planner は異なる `schema.Type[T]` を同じ catalog/graph に格納する必要があるが、data item を `any` で運ぶ必要はない。`schema.Descriptor` は marker identity と payload Go 型だけを control plane へ運び、typed trait は `schema.Type[T]` に残す。component 登録時に `T` が確定した execution closure を捕捉する。

概念例:

schema identity、payload 型、typed trait の分離は
[schema の `ExampleDefine`](../../media/schema/example_test.go) を正本とする。component は
`plugin.WithReader` / `WithProcessor` / `WithWriter` で `schema.Type[T]` を execution binding へ
渡し、Program Open 時に operator interface を一度だけ検証する。bounded queue と fan-out は
この closure の内側で typed のまま構築し、item ごとの型消去を導入しない。

`schema` package 自身は queue、fan-out、scheduler を構築しない。それらは Host runtime の private 実装であり、plugin contract は typed Reader/Writer/Processor と trait 宣言だけを持つ。type assertion は Program の Open 時だけであり、item ごとには行わない。

```text
catalog / planner: schema ID と payload type
registration:      typed execution closure と traits を捕捉
Open:              operator/link 型を一度検証
Run:               Reader[T] -> Processor[I,O] -> Writer[O]
```

この方式なら、第三者が `schema.Define[unitID, acme.Unit]` と typed execution binding を宣言した build に、`acme.Unit` 用の delivery、bounded edge、fan-out/drop が生成される。core が `acme.Unit` を事前に知らなくてもよく、hot path で reflection、string lookup、serialize、`any` map を使わない。専用 marker により、payload 型の refactor と schema identity も分離できる。

## stream descriptor

stream kind の closed enum と全属性を詰め込んだ `MediaAttributes` を廃止する。

現行の immutable descriptor の構築と property 参照は
[stream の `ExampleNewDescriptor`](../../media/stream/example_test.go) を正本とする。

`property.Set` は immutable な control-plane 値であり、audio sample rate、video pixel format、subtitle language、codec parameters 等を typed key で保持する。未知 property をコピーできるが、component は requirement として宣言した property だけを解釈する。

hot path の unit がこの map を毎回持たない。stream-local な不変情報は compiled edge/port に保持する。

## time

- stream ごとに一つの integer time base を持つ。
- PTS、DTS、duration は型を区別した integer とする。
- rescale は checked integer arithmetic を使い、overflow と rounding policy を明示する。
- control plane で必要なら rational を使えるが、item ごとに `big.Rat` を使わない。
- timestamp が不明な状態を `0` と混同せず optional/flag で表す。
- decoder reorder、encoder delay、filter latency を descriptor/plan に反映する。

## Packet、Chunk、Parser

format が読む単位と decoder が必要とする packet は常に同じではない。WAVE 内の MP3、H.264 Annex B/AVCC、AAC ADTS、FLAC の resync 等に対応するため、Parser/packetizer を第一級 component とする。

```text
Format Reader -> container Chunk -> Parser -> codec Packet -> Decoder
Decoder -> Frame -> Encoder -> codec Packet -> optional Bitstream Filter -> Format Writer
```

すでに packetized された container では Parser は identity または不要である。bitstream filter は `Packet -> Packet` component であり、decoder/encoder とは独立する。

`Chunk` と `Packet` の payload、Access boundary の byte handle、positioned write はすべて同じ immutable `buffer.Bytes` view を返す。view は private backing に対する `Len` / `At` / zero-copy `Slice` / `From` と copy/read/compare 操作だけを持ち、`[]byte` を公開・保持しない。payload size に比例する読み取りは `Blocks` を使う。`Blocks` は caller 所有の scratch へ block 単位で `CopyTo` し、backing を公開せずに lifetime 検査を block ごと一度で済ませる。`At` は呼出しごとに lifetime を検査するため単発の参照専用であり、`Len` は検査せず記録済みの範囲を返す cheap accessor である。originating lease と範囲だけを持つため owner 解放後は無効になり、view が allocation lifetime や allocator grant を暗黙に延長しない。raw mutable backing は `buffer.Edit` または未公開 allocation を初期化する `WriteLease` の明示 writer path にだけ存在する。この制約により、borrow、read-only handle、fan-out 後の shared handle のどこから読んでも COW を迂回できない。

未知の codec tag/sample entry は raw carrier として保持し、対応 Binding がないことを structured diagnostic にする。

## Format、Codec、Carrier、Metadata

混同しやすい概念を次の六つに分ける。

### Format

container または elementary stream の framing、stream table、interleave、seek、header/footer を扱う。例: RIFF/WAVE、MP4、Matroska、native FLAC stream。

Format は「この byte 列が何であるか」を読み書きするが、その byte 列が file、HTTP、S3、memory のどこから来たかは扱わない。Source/Sink capability と transaction は [Access](access.md) が所有する。Format は特定 decoder、metadata parser、Access Provider を直接 import しない。

### Codec

media の圧縮/表現方式である。例: PCM、MP3、FLAC、H.264。`Codec` という語は media codec に限定し、ID3/Vorbis Comment には使わない。

### Carrier

format または codec bitstream 内にある、外部 payload を置く物理的な slot/envelope である。

例:

- RIFF の `LIST/INFO` chunk
- WAVE の `id3 ` chunk
- MP3 stream の先頭/末尾 tag area
- FLAC metadata block
- MP4 atom/sample entry
- 将来の video SEI message

Carrier は payload の意味を決めない。format-owned carrier だけでなく codec/bitstream-owned carrier もあるため、`ContainerID` 一つを owner として埋め込まない。

同じ理由により Carrier は `media/carrier` という独立 package に置き、`media/format` の内部型にしない。format の内部型にすると codec/bitstream が所有する carrier が format 由来に見え、`media/metadata` が carrier identity 一つのために `media/format` を import することになる。format と codec は対等に carrier を宣言し、metadata Binding はその identity を key として使う。

carrier identity は [C8](decisions.md) に従い marker 型から導出する。carrier は第三者が宣言する identity であり、`metadata.Bind` はその値をそのまま composition の declaration key に使うため、手書き文字列のままでは二つの plugin が同じ `"id3"` を選んだだけで衝突する。foundation の identity で第三者に一意な文字列を考えさせる箇所を残さない。これは `config.Schema` の identity を marker 由来へ移したのと同じ適用であり、carrier だけを例外にしない。規格側の値である `format.Tag`（WAVE の `0x0055` 等）は identity ではなく data なので対象外である。

carrier は owner field を持たない。owner を表す文字列は読む側が存在せず、marker から導いた identity の package path が宣言元をそのまま示す。format と codec のどちらが所有するかを型で区別する必要が生じるのは、MP3 の先頭 tag area のように codec bitstream 側が carrier を宣言する M8 であり、その実 consumer を前にして決める。宣言だけの field を先に凍結しない。

### Metadata Encoding

carrier payload の byte 表現を semantic document へ parse/marshal する規格である。例: ID3v2.4、Vorbis Comment、RIFF INFO。

### Metadata Document

format に依存しない semantic entry の列である。title、artist、picture のような共通概念と、第三者固有 key、origin、未解釈 raw block を保持する。

### Binding

carrier と metadata encoding、または container codec tag と media codec/parser の対応を結ぶ composition-time declaration である。Format/Codec/Encoding 本体のどれにも対応表を埋め込まない。

## 関係の全体像

関係は継承ではなく、二つの独立した経路と Binding で表す。

```text
encoded media:

Source
  -> Format
       -> stream carrier/tag
       -> Codec Binding
       -> optional Parser
       -> Packet
       -> Codec
       -> decoded schema

metadata:

Format or codec bitstream
  -> Carrier
       -> Metadata Binding
       -> Metadata Encoding
       -> Document
       -> shared keys / third-party keys / raw blocks
```

WAVE 内の MP3 と ID3 を例にすると次の通りである。

```text
RIFF/WAVE Format
  ├─ fmt tag 0x0055
  │    └─ codec Binding -> MP3 Parser -> MP3 Packet -> MP3 Decoder
  └─ "id3 " chunk Carrier
       └─ metadata Binding -> ID3v2 Encoding -> Document
```

Format は WAVE chunk と tag の物理表現を知るが、MP3 decoder や ID3 parser を import しない。MP3 Codec は音声を decode するが、RIFF chunk を知らない。ID3 Encoding は tag payload を解釈するが、それが WAVE chunk、MP3 stream 先頭、別の carrier のどこに置かれたかを知らない。standard composition が二種類の Binding を足して初めて一つの経路になる。

native FLAC のように一つの規格名が format と codec の両方を含む場合も、component と責務は分ける。同じ plugin package に凝集させることと、一つの interface に融合することは別である。

## codec binding

format が認識する tag/sample entry と、codec/parser/parameter schema の関係を standard composition で登録する。

binding の現行 API と codec/parser target の保持は
[codec の `ExampleBind`](../../media/codec/example_test.go) を正本とする。公式 WAVE、
MP3、PCM、MP4 の具体 binding は各 plugin を移植する milestone で追加する。

このため、第三者 codec を WAVE に追加する際に WAVE plugin も core も編集しなくてよい。第三者は新しい Binding を `Set` に足すだけである。

binding key が重複して異なる codec を指す場合、host build を失敗させる。利用者が意図的に置換する場合だけ explicit override を使う。

## metadata document

単純な `map[reflect.Type]any` では、同一 key の複数値、順序、元の frame/block、未知 payload、部分編集を表現しにくい。document は ordered entry と provenance を持つ。

現行 API は slice の直接 mutation を許さない。ordered/repeated entry を builder から
構築する経路は [metadata の `ExampleNewBuilder`](../../media/metadata/example_test.go)、
immutable payload は同じ file の `ExampleNewBlob` を正本とする。

実際の API では slice の直接 mutation を許さず、persistent/immutable value または builder を使う。

`RawBlock` は source anchor と opaque payload を同じ順序付き集合に記録する。`NewSourceBlock` は semantic entry の
完全な `Origin` が参照できる元 bytes であり、carrier、encoding、block が一致する同一 Document の anchor だけを
参照できる。`NewRawBlock` は unknown frame、vendor field など Origin から参照できない opaque bytes である。同じ
format owner は opaque bytes を byte exact に保持できるが、foreign opaque block を表現できない出力は error にして
黙って落とさない。metadata を編集した時だけ影響する source block を再 encode する。

artwork の大きな byte slice は entry clone のたびに複製せず、immutable `Blob`/reference-counted buffer を参照する。

## open key と共通 vocabulary

core contract は `KeyID`、`Entry`、`Document` の仕組みだけを定義し、Title 等の concrete key enum を持たない。

公式の任意 package `tag` が共有 vocabulary を提供する。

[tag の Example](../../media/tag/example_test.go) が、共通 vocabulary、partial date、
host 検証用 declaration の現行 API を実行する。

第三者も core を変更せず固有 key を定義できる。

第三者 key の定義と clone 規則は
[key の `ExampleDefine`](../../media/key/example_test.go) を正本とする。core の enum や
registry を変更せず、同じ key を metadata と side data の双方で使える。

現行 `Bundle` の `single`/`multiple` が unexported method を要求する方式は、外部 package に key family を追加させられないため置換する。

### key 機構は一つ、容器は三つ、key 型は二つ

marker から identity を導き、[C17](decisions.md#c17-config-snapshot-は-codec-clone-だけで構成する) の宣言 clone 規則を課し、erased accessor を提供する機構は `media/key` に一つだけ置く。この機構を容器ごとに書き写さない。

容器は三つあり、意味が違うので分離を維持する。

| 容器 | 表すもの | scope | 多重度 |
|---|---|---|---|
| `property.Set` | stream の control-plane 属性 | stream/descriptor | 単一値 |
| `metadata.Document` | 順序、重複、origin、raw block を持つ semantic document | asset/program/stream/chapter | 順序付き複数値 |
| `side.Data` | 単一 packet/frame に付く値 | item | 順序付き複数値 |

`Document` の scope、carrier origin、raw block は stream や asset を記述する control-plane の概念なので `side.Data` には現れない。逆に `side.Data` は item ごとの hot path に載るため、値を持たない item が追加 allocation も間接参照も負わない表現にする。

key 型は三つではなく二つにする。**容器が key へ要求するものが二種類しかないためである。**

| | 宣言 clone | canonical encoder | 理由 |
|---|---|---|---|
| `property.Set` | 必要 | **必要** | [planner](planner.md#descriptor-state) の descriptor fingerprint に canonical property key/value として参加する |
| `metadata.Document` | 必要 | 不要 | Plan に載るのは Binding、Mapping、loss であって entry の値ではない |
| `side.Data` | 必要 | 不要 | item 局所であり control plane に出ない |

したがって `metadata.Document` と `side.Data` は `key.Key[T]` をそのまま共有する。同じ marker 宣言が document metadata と side data の両方で通ることは、別途 API を足すのではなくこの構造から従う。第三者の `ReplayGain` を、asset の metadata としても frame ごとの side data としても、一度の宣言で使える。

`property.Key[T]` は同じ機構の上に canonical encoder の宣言義務を加えた別型とする。canonical 表現を作れない property は fingerprint を不安定にし、planner の memoization と Plan の再現性を壊す。これは [config](config.md#immutability-と-canonicalization) が「canonical form を作れない field は schema 登録を失敗させる」として config field に課している規則と同一であり、規則の適用条件は同じ「値が fingerprint に入るか」である。config field と stream property は入り、metadata entry と side data は入らない。

canonical encoder を全 key へ義務付けると、artwork のように canonical 表現を持たない metadata key が宣言できなくなる。逆に optional にすると、canonical を持たない key を `property.Set` へ入れた時点で初めて失敗する。宣言時に検出できるものを実行時へ送らない。

### key identity の重複を検出する

`key.Define[gainID, float64]` と `key.Define[gainID, string]` のように、同じ marker を異なる payload 型で宣言することは bug である。identity は marker からのみ導き payload 型を含めないため（[C8](decisions.md)、payload 型の refactor で identity を変えないための規則）、この重複を Go の型で防ぐことはできない。

容器側は既に fail-closed である。`property.Set`、`metadata.Document`、`side.Data` はいずれも格納時の型を保持し、key の型と一致しない entry を返さない。したがって誤った型の値が読み出されることはない。残る害は、Plan と diagnostic に同じ名前で別物が現れること、`key.ID` の一致が実在しない Mapping を可能に見せることである。

検出は、二つの宣言を同時に見られる唯一の場所、すなわち host 構築時に行う。key は既存の `plugin.Declaration` に載せ、`internal/catalog` が codec Binding や Provider scheme と同じ経路で conflict を報告する。key 専用の registry を新設しない。

- 同じ marker を同じ payload 型で複数回宣言することは無害とし、異なる payload 型で宣言した場合を host 構築 error とする。namespace は `property` と `metadata`/`side` で共有し、容器をまたいだ重複も検出する。
- 宣言しない key も動作する。その場合は検証と catalog 表示の対象にならない。公開しない private key に宣言を強制しない。

`media/tag` の共通 vocabulary は宣言をまとめて公開し、`standard` composition がそれを含める。
host-time conflict の実行例は
[plugin の `ExampleDeclareKey`](../../plugin/example_test.go) を正本とする。

#### 宣言の構築は `media/key` に置かない

`key.Declare(k)` が `plugin.Declaration` を返す形は採らない。`media/key` は data plane の閉包に属し、[architecture](architecture.md#foundation-内部の依存方向) が依存を `internal/{marker,snapshot}` だけに制限しているため、`plugin` を import した時点で `media/packet -> media/side -> media/key -> plugin -> {config, flow, diagnostic}` となり、control plane と data plane の分離が崩れる。

宣言は composition 時の行為であり、その呼び出し元は常に control plane の plugin package である。したがって構築子は composition を所有する `plugin` に置き、`plugin` が `media/key` を import する。依存は一方向で、`media/key` は何も import し返さない。

```text
media/key   機構（identity、宣言 clone、erased view）。plugin を import しない
plugin      Declaration の構築。media/key を import する
catalog     conflict の検出と報告
```

`property.Key[T]` と `key.Key[T]` は同じ erased view を返し、一つの構築子と一つの namespace を共有する。容器ごとに構築子を分けない。

#### `plugin.Declaration` の target を一般化する

現在の `Declaration` は target を component identity の列とし、`internal/catalog` が全 target の catalog 実在を検査する。key 宣言の target は component ではなく payload の Go 型なので、この形のままでは載せられない。payload 型を marker 由来 identity として表すこともできない。`string` や `[]byte` のような predeclared/unnamed 型は package path を持たず、[C8](decisions.md) の canonical identity を導けないためである。

したがって `Declaration` の target を「catalog に実在すべき component」と「payload を識別する Go 型」を区別できる形へ一般化する。conflict の判定規則は両者で同一（同じ key に異なる target 集合が現れたら error）なので、検出経路は一つのままである。target が component かどうかを暗黙の前提にしていた箇所を明示にする変更であり、codec Binding、metadata Binding、Provider scheme の意味は変わらない。

### 多重度の宣言は持たない

`Title` は単一値、`Artist` は複数値、という規格上の差を key は宣言しない。

この情報の consumer は、出力 carrier が一値しか持てない時に畳み込みが loss かどうかを判断する metadata encoding であり、それが現れるのは実 ID3/Vorbis Comment を移す M8 である。それ以前に凍結しない理由は、多重度が二値とは限らないためである。ID3v2.4 の text frame は複数値を、`COMM`/`USLT` は language/description を伴う値を正当に持て、Vorbis Comment も複数の `TITLE` を持てるため、実際に必要な区分は「単一 / 複数 / 修飾子付き複数」になり得る。実 encoding を前にせずに選ぶと作り直しになる。

M8 が [capability](capability.md) の ID3/Vorbis Comment 移行でこれを決める。それまで `Values` と `First` の両方を全 key に提供し、規格上の制約は encoding 側が持つ。

## metadata binding と変換

carrier と encoding を binding する。

carrier と encoding component の binding は
[metadata の `ExampleBind`](../../media/metadata/example_test.go) を正本とする。公式
carrier/encoding の組は M6/M8 の standard composition で追加する。

parse の流れ:

```text
Format/bitstream
  -> Carrier(raw payload)
  -> selected Metadata Encoding
  -> Document entries + origin + unknown raw blocks
```

marshal の流れ:

```text
Document
  -> target Carrier capabilities
  -> selected Metadata Encoding
  -> payload
  -> Format/bitstream writer
```

異なる規格同士の一般的な変換は、規格間の全組み合わせを直接書くのではなく、共通 vocabulary を hub にする。

```text
ID3 TIT2 -> tag.Title -> Vorbis TITLE
RIFF IART -> tag.Artist -> ID3 TPE1
```

これで encoding が N 個に増えても N² 個の converter は不要になる。

ただし任意の第三者 key が同じ意味かどうかは自動判定できない。必要な場合は optional な Mapping component を追加する。

方向、lossiness、priority、typed conversion を明示する現行 API は
[metadata の `ExampleMap`](../../media/metadata/example_test.go) を正本とする。

Mapping は source key、target key、lossiness、priority を宣言する。曖昧な変換を host が推測しない。

## metadata scope

静的 document には scope を持たせる。

- asset/container
- program
- stream
- chapter

時刻に沿って変化する metadata は static `Document` に詰めず、typed timed-event stream とする。これにより字幕、章 event、放送 metadata、video side data を同じ data-plane 原則で扱える。

## loss と strictness

出力 carrier/encoding が entry を表現できない場合は三段階で処理する。

1. 同じ format owner の opaque block を保持できるなら lossless copy
2. 共通 key または明示 Mapping で表現できるなら変換
3. どちらも不可能なら loss report

default は best effort + structured warning とし、黙って捨てない。`StrictMetadata` では 3 を job failure にする。

diagnostic は key だけでなく origin、target carrier、理由、選ばれなかった mapping を含める。

## default output

無指定出力では入力 format/codec/stream と metadata raw block の保持を優先する。media model が保証するのは、copy可否、Binding、raw preservation、loss/effect を planner が判断できる descriptor を提供することである。

具体的な候補順、自動挿入、明示 output との競合は [planner](planner.md#default-transcode-の探索)、利用者向け既定動作は [surfaces](surfaces.md#default-behavior) を正本とする。

## M3 完了条件

M3 は media control plane と data plane の型、metadata model、Binding を foundation package として新設する milestone である。runtime と ownership の実行は M5、公式 plugin の移行と実際の Format/Codec 実装は M6/M8 の担当であり、M3 には要求しない。I/O 側の条件は [access](access.md#m3-完了条件)、拡張点の網羅は [scope](scope.md#m3-完了条件) を参照する。

component Spec のうち M3 が確定するのは port shape と `Open` までとする。`Compile` と `Suggest` は planner という consumer が入る M4 が確定する。walking skeleton は `Open` が返した operator を test 内の手書き駆動 loop で流し、descriptor 変換規則を持たない。これは [C21](decisions.md#c21-foundation-package-は-media-領域だけを-grouping-する) のとおり `component` package ではなく `plugin.Component` へ足す。

typed frame は `media/audio` だけを M3 で実装する。`media/video` と `media/subtitle` は実 consumer が現れる milestone まで作らず、第三者相当の schema fixture で拡張性を検査する。`media/audio` を先に作るのは、[audio](audio.md#原則) の「filter が内部で sample format を decode/encode しない」という性能契約が `media/buffer` の plane layout と ownership に依存し、両者を離して決めると M5/M6 で作り直しになるためである。

- 第三者が `schema.Define[ID, T]` で独自 unit を宣言でき、typed component binding から delivery、fan-out、drop 等の型付き実装を作れる。core は `T` を事前に知らない。
- schema identity が payload の Go 型ではなく marker 型から導出され、payload 型の refactor と identity が分離している。
- 万能 `Frame` interface と閉じた stream kind enum が新 package に存在しない。`audio.Frame` に加え、`video.Frame`/`subtitle.Cue` 相当の非 audio unit を core 無変更で別 schema として宣言でき、port を schema の具体型で結べる。後者は foundation の test fixture で検査し、`media/video`/`media/subtitle` package は作らない。
- `plugin.Component` が port shape と `Open` を持ち、`flow` の typed port を宣言できる。`component` package を新設しない。
- 型消去は catalog/planner の descriptor と Program Open の execution token に限定され、item ごとの reflection、文字列 lookup、`any` map、serialize を必要としない。type assertion は Open 時に一度だけ行う形になっている。
- `stream.Descriptor` が schema identity、time base、immutable `property.Set`、`metadata.Document` を持ち、未知 property を解釈せずに保持できる。
- `timing` が integer time base と型を区別した PTS/DTS/duration を持ち、rescale が checked arithmetic で overflow と rounding policy を明示する。timestamp 不明を `0` と混同しない optional/flag を持つ。
- Format、Codec、Carrier、Metadata Encoding、Metadata Document、Binding が別の型として分かれ、Format が特定の decoder/metadata parser/Access Provider を import しない構造になっている。
- container chunk と codec packet が別の型で、Parser を第一級 component として宣言できる。bitstream filter を `Packet -> Packet` として表現できる。
- codec Binding と metadata Binding を composition 時に登録でき、同じ binding key が異なる対象を指す場合に host 構築を失敗させる。意図的な置換は明示 override だけで行う。
- `metadata.Document` が順序付き entry、完全な source `Origin`、source/opaque を区別する `RawBlock` を保持し、slice の直接 mutation を許さない。第三者が core を変更せず固有 key を定義でき、共通 vocabulary は `tag` package が持つ。
- `media/side` が packet/frame の immutable side data を提供し、第三者 key の clone 規則を `media/metadata` と共有する。side data を持たない item は追加 allocation や間接参照を必要とせず、`media/stream.Event` は live topology の既定 policy を暗黙に選ばない。
- metadata Mapping が source key、target key、lossiness、priority を宣言でき、host が曖昧な変換を推測しない。
- metadata scope（asset/program/stream/chapter）を表現でき、時刻に沿って変化する metadata は static document ではなく typed event stream として宣言できる。
- artwork 等の大きな byte slice が entry の複製ごとに copy されず、immutable blob 参照を共有する。
- 上記を unit/property test で検査する。第三者相当の schema/key/Binding fixture を含め、公式 plugin を import しない。
- **walking skeleton が通る。** test 用の trivial な schema、format、codec、metadata encoding を foundation の test 内に定義し、`bytes → packet → frame → packet → bytes` が新 contract だけで端から端まで流れる。実 format を使わず、planner と runtime も使わない。component は `plugin.Component` の `Open` が返した operator として繋ぎ、駆動 loop と接続順だけを test が持つ。この経路は M4 と M5 が planner/runtime を差し込む間も壊さず、M6 が trivial component を実 WAVE/PCM へ置換する。
- 各 contract が「walking skeleton が実際に使うもの」と「宣言だけで consumer を持たないもの」に分類され、後者が一覧できる。consumer を持たない contract は型を最小限にとどめ、詳細を凍結しない。

M3 では次を未完了事項として残す。descriptor から実際の Format/Codec を駆動する経路は M6/M8、`Compile` が capability 不足を structured diagnostic にする経路は M4、loss report の surface 表示は M7 で扱う。

walking skeleton を要求する理由は、consumer のいない contract を M3〜M5 の 3 段積み上げないためである。M2 では `config.SchemaView` に resolver が必要だと判明したのが「M4 がどう使うか」を検討した時点であり、それまでの review では検出できなかった。M3 は package 数がさらに多く、同じ失敗の影響が大きい。

## M6 完了条件

M6 は最初の実 container 経路が動く milestone である。M4 の実 PCM へ WAVE を足し、demux → decode → encode → mux を新設計だけで通す。I/O 側の条件は [access](access.md#m6-完了条件)、composition と拡張性 gate は [plugins](plugins.md#m6-完了条件)、testkit と conformance は [quality](quality.md#m6-完了条件)、利用者と plugin 開発者の体験は [experience](experience.md#m6-完了条件) を正本とする。

対象 family は WAVE と linear PCM だけとする。MP4 と multi-stream は M7、MP3/FLAC/audio filter は M8 が担当し、`media/video` と `media/subtitle` の frame 型は [scope](scope.md#初期実装と将来拡張) のとおり作らない。

### 作業単位

M6 は 5 文書に跨る最大の milestone であり、単位を分けずに着手すると「WAVE が動くまで何も動かない」区間が長くなる。分割の判定規則は M4 と同じく **各単位が端から端まで green の実行経路を残すこと** とする。

| 単位 | 内容 | 単位終了時に動くもの |
|---|---|---|
| M6-0 | Access/Endpoint を component の trait にする合成 contract の是正。`ProviderRole`、Provider manifest、`Requirements`、`endpoint.Component`、`host.Providers`/`Endpoints` の削除と acquire 契約の追加 | `plugin.Set` 一つで両方向 Provider を含む合成が表現できる。data path は M5 のまま変わらない |
| M6-1 | write 側 capability と narrow view、`plugin/file` Provider、prepared session の acquire と実 capability 再検証、transactional file output と temporary file の cleanup | M5 の PCM 経路が direct resource ではなく file Reference で通り、出力が temporary file と replace で commit される |
| M6-2a | Prepare の順序是正（Acquire → Inspect → Compile）、Inspect contract、WAVE demux と RIFF chunk 解析 | 明示指定した WAVE file を読んで raw PCM を書き出せる |
| M6-2b | sink 側の positioned write schema、file sink の適用、spool adapter と spool quota、WAVE mux | WAVE file を書ける。逐次書きのみの sink でも spool 経由で正しい header を書ける |
| M6-2c | RIFF INFO、未知 chunk の raw preservation、codec Binding による parser/decoder 選択 | metadata roundtrip と tag 駆動の codec 選択が通る |
| M6-3 | 二段階 binding、候補間で共有する bounded probe、自動 format 選択、逐次入力の prefix replay | 入力 format を明示せずに WAVE を content evidence で判別し、evidence が無い入力は明示 hint がある場合だけ raw PCM を選ぶ |
| M6-4 | `standard` composition、public `testkit` の最小形、`integration` module、out-of-tree 相当 plugin の拡張性 gate | 公式 composition から Host を作れ、第三者 plugin が core 無変更で同じ経路に載る |
| M6-5a | `job` の Format hint/request、Format trait の拡張子宣言、`standard.Convert` | 一行の library 呼び出しで file から file への変換ができ、出力 format が指定どおりに選ばれる |
| M6-5b | Run 単位の bounded event delivery、`cli`/`cmd/godec`、体験の実測 | 公式 binary で変換でき、実行中の progress と cancel が働く |

順序は依存で決まる。M6-1 は M6-0 が作る trait 契約を、M6-2a は M6-1 の実 byte session を、M6-2b は M6-2a の WAVE header 知識を、M6-2c は両方向の経路を、M6-3 は判別対象となる 2 つの実 Format を、M6-4 は検証対象の実 plugin を、M6-5a は composition、M6-5b は file convenience と実行 event consumer をそれぞれ必要とする。

M6-2 を 3 つに割ったのは、読み経路と書き経路で必要な機構が違うためである。読みは Inspect と Prepare の順序是正を要し、書きは positioned write schema と spool adapter を要する。一つの単位にまとめると「WAVE が両方向とも動くまで何も green にならない」区間が生まれ、上の判定規則を満たせない。

**Inspect は M6-2a が担当し、Probe は M6-3 に残す。** 「既知 format の header を読む」ことと「どの format か決める」ことは別の操作であり、前者は明示指定された WAVE が実 consumer になる。Inspect を後段に残すと、M6-2 は WAVE の sample properties、time base、codec tag、metadata、未知 chunk を fixture か config から捏造するしかなく、M6-1 が [access](access.md#m6-完了条件) の carrier descriptor 規則で除去した形を再導入することになる。

M6-0 は M6 着手時の contract 監査で見つかった不整合の是正であり、新機能を作らない。実装を始めてから合成 API を変えると差分の由来が追えなくなるため、file I/O を含む M6-1 と混ぜず独立させる。詳細は [access](access.md#m6-完了条件) の Provider trait 条項と [plugins](plugins.md#m6-完了条件) の合成条項を正本とする。

### media 側の条件

- WAVE が `media/format` の Format として宣言され、RIFF chunk 境界、`fmt `/`data` chunk、padding、size 上限を実規格として扱う。移植参照は `_legacy/plugin/wave/internal` にある。
- **`RIFF`/`data` size の後追い patch で header 長が決して変わらない。** mux は先頭に `ds64` と同サイズの `JUNK` chunk を予約し、payload が 4 GiB を超えた場合だけ `JUNK` を `ds64` へ書き換えて RF64 にする。旧実装が持つ「header 長が変わったため patch できない」失敗（`_legacy/plugin/wave/internal/muxer.go`）が新経路に存在しない。sink capability による経路選択は [access](access.md#m6-完了条件) を正本とする。
- **予約 slot の `JUNK` も入力由来 byte として保持する。** 同じ位置・同じ size の non-zero `JUNK` は合法な RIFF であり、writer が作る空 slot と区別できなければならない。inspect はそれを専用 anchor の raw chunk として保持し、mux は自前の空 slot を作る代わりにその byte 列をそのまま同じ slot へ書き戻す。したがって RIFF → RIFF の roundtrip は byte 一致し、繰り返しても増殖しない。RF64 化した場合だけ `ds64` がその slot を占めるため保持していた byte は失われる。header 長は data size 確定前に固定されるため他所へ移せず、この loss は [capability](capability.md) の B8 に記録した契約上の例外とする。M7 の loss report が実 consumer になった時点で、この置換を report 対象にする。
- **container framing と codec packet の schema を共有 vocabulary が所有する。** `packet.Chunk` の schema は `media/format`、`packet.Packet` の schema は `media/codec` が持ち、`plugin/pcm/linear` 固有の宣言を削除する。WAVE demux が出した chunk を PCM parser が受け取れること、codec Binding が format tag から任意の parser を選べることは、どちらも schema identity が plugin 横断で一致していることを前提にしている。plugin 固有 identity のままでは接続が `graph.schema-mismatch` になり、埋める bridge も存在しない。Access boundary の byte schema は [access](access.md#m6-完了条件) のとおり `access` が所有する。所有先の分割はこの文書の Format/Codec/Carrier の責務分割にそのまま従う。
- **metadata Encoding が component trait として振る舞いを持つ。** M5 時点の `metadata.Binding` は `plugin.Declaration` の alias で宣言しかなく、Parse/Marshal の契約は foundation test の private interface にしかない。したがって catalog は Binding の衝突と target 実在しか検査できず、encoding の振る舞いを取得できない。このままでは WAVE が RIFF INFO を自分で parse することになり、Format と Metadata Encoding を Binding で分離するこの文書の設計に反する。context-aware で純粋な `Parse`/`Marshal` を trait に持たせ、Binding target が trait を持たない場合は Host 構築時の composition diagnostic にする。
- **Format は carrier ID と raw payload だけを知る。** Host が carrier ID から Binding target を解決し、Format には catalog 全体ではなく narrow な metadata resolver を渡す。WAVE が具体 encoding component を import しない。[runtime](runtime.md#host-service) の「全 service や catalog を自由に取得できる Host/service locator は渡さない」に従う。
- **Parse/Marshal は Open を要求しない。** Parse は Inspect の最中、つまり Compile より前に必要であり、`Open` は Program 確定後なので、operator として取得する形では循環する。trait であれば composition 時に解決でき、[scope](scope.md#m6-の-contract-分類) の trait 判定規則にも合致する。payload grant も要求しない control-plane 操作とする。
- **Inspect が compile より前に走り、header から得た事実だけで descriptor が確定する。** WAVE の sample properties、time base、codec tag、metadata、未知 chunk は header にしかない。`stream.Descriptor` は compile 時に確定する immutable 値なので、runtime の demux が後から更新することはできない。したがって Prepare は Acquire → Inspect → Shape/Compile の順で進み、[runtime](runtime.md#planner-pipeline) の planner pipeline と一致する。M5/M6-1 時点では acquire が Compile より後だったが、M6-2a でこの順序へ再構成済みである。`host/inspection_test.go` が Inspect 結果を Compile へ渡す現在の経路を固定する。
- **codec Binding が実際に parser/decoder の選択を絞る。** Inspect が確定した descriptor に codec tag を載せ、catalog が Binding を tag で引けるようにし、solver が tag に対応する Parser/Decoder を選ぶ。M5 時点の solver は入力 schema だけで候補を索引するため、tag と無関係に同じ schema を受ける Parser/Decoder がすべて候補になる。PCM だけの間は不可視だが、MP3/FLAC が入る M8 で誤選択になる。
- **tag による絞り込みは codec/parser の選択にだけ効かせる。** 入力 descriptor が codec tag を持つとき除外するのは「別 tag の codec/parser として宣言された候補」に限り、codec Binding を持たない component（converter、resampler、bitstream filter 等）は従来どおり候補に残す。tag を全候補の filter にすると solver の一般性が失われる。
- codec Binding が WAVE の format tag と `plugin/pcm/linear` の Parser/Decoder を結び、PCM component 側を container 向けに書き換えない。container が codec の packetization を直接知る経路（[F20](findings.md)）が新経路に現れない。
- 未知 chunk が `metadata.RawBlock` として解釈されずに保持され、WAVE → WAVE の roundtrip で byte 列と順序が復元される。
- RIFF INFO の metadata encoding が `media/metadata` の Document/Origin/RawBlock と `media/carrier` の carrier identity を使い、重複 key と順序を保存する。共通 key への写像は `media/tag` の vocabulary を使う。
- RIFF INFO の marshal は document の編集を検出して扱いを分ける。未変更 document は元 carrier の bytes をそのまま返し、編集時も unchanged known child の raw bytes、padding、順序、未知 child の `RawBlock` を保持し、変更または削除された semantic child だけを再 encode/除去する。`plugin/wave` の `matchInfoEntries`/`matchInfoSubsequence` は duplicate native entries を origin/value と document order で deterministic に対応付け、semantic edit を raw のまま黙って返さない。完全に同一の native・value・origin duplicate は per-occurrence identity を持たないため byte-level 個体識別をしない、という deterministic limit を契約として明記する。
- `media/format` の Probe と Inspect が実 consumer を得る。Probe は bounded な先頭 range だけで判定し、Inspect が選択後に stream descriptor、time base、metadata carrier を読む。
- M3 の trivial な schema/format/codec/metadata encoding が実 WAVE/PCM に置換され、`bytes → packet → frame → packet → bytes` の経路に test 専用 component が残らない。恒久 harness の役割は `integration` の end-to-end test が引き継ぐ。
- WAVE/PCM の lossless roundtrip が exact で一致する。判定方法は [capability](capability.md) の該当行に従う。
- **新規 export ごとに、呼び出し元を示すか、宣言のみとして [scope](scope.md#m6-の-contract-分類) へ consumer を作る milestone とともに記載する。**
- **先行 milestone から引き継いだ宣言を監査する。** [refactor.md](../refactor.md#実装ロードマップ) の「引き継いだ責務を完了条件へ再掲する」に従い、M3〜M5 が「宣言のみ」「M6 が担当」とした項目を検索し、実 consumer、完了条件、さらに先へ送る明示記録のいずれかへ一件ずつ対応付ける。
- 上記を unit/property test と `integration` の end-to-end test で検査する。

M6 では次を未完了事項として残す。format 間・multi-stream の無指定 output に適用する copy/remux 既定（[capability](capability.md#挙動変更の記録) の B1）、metadata の loss report、`metadata.Mapping` の適用、seek plan は M7。MP3/FLAC/audio filter と variant selection は M8。M6 の単一 WAVE → WAVE は packet/chunk を保持する format 固有の direct path を実 consumer とし、decoder/encoder の roundtrip を選ばない。この fast path を format 横断の既定 policy、mapping、loss report と取り違えない。

size 不明 header を書く streaming 出力（旧実装が非 seekable sink へ書いていた `0xFFFFFFFF`）は M6 では提供しない。規格上不正な出力を既定にしないためであり、需要が確認された milestone が opt-in と `Plan` の warning を伴って追加する。M6 の非 seekable sink は spool 経路で正しい header を書く。

## 文書全体の完了条件

この節は media/metadata contract の最終状態を示す gate であり、個別 milestone の完了判定には各 milestone 固有の条件を用いる。M3 の判定には上記「M3 完了条件」だけを使う。

- 第三者が core を変更せず、新しい schema、unit 型、Format、Codec、Parser、Carrier、Metadata Encoding、key、Binding、Mapping を追加できる。
- 万能 `Frame` と閉じた stream kind enum が存在せず、port が schema の具体型で結ばれる。
- data plane の package が control plane の package を import せず、marker 由来 typed key の機構が `media/key` に一つだけ存在する。canonical encoder を要求するのは fingerprint に参加する key だけである。Carrier が `media/format` の内部型になっていない。
- 同じ marker を異なる payload 型で宣言した key が host 構築 error になり、専用 registry を持たない。
- 型消去が control plane の descriptor と typed execution registration に限定され、item ごとの reflection、文字列 lookup、`any` map、serialize を必要としない。
- container chunk、codec packet、decoded unit、side data、static metadata、timed event が別の型として分かれている。
- timestamp が integer time base で表され、rescale が overflow と rounding policy を明示し、不明を `0` と混同しない。
- Format が Access Provider、decoder 実装、metadata parser を import せず、Binding だけが composition 時に結ぶ。
- metadata が順序、重複 key、origin、未解釈 raw block を保持し、artwork を entry 複製ごとに copy しない。
- 表現できない metadata が黙って消えず、raw preservation、明示 Mapping、loss report のいずれかになる。
- 無指定出力で copy/remux と情報保持が優先される判断材料を descriptor が提供する。
