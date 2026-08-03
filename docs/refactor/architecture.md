# 目標アーキテクチャ

この文書は repository、module、package、公開 contract、private runtime の境界を定義する。個々の component API は [plugins](plugins.md)、media model は [media](media.md)、現行 package の移行先は [inventory](inventory.md) を参照する。

## 依存原則

依存方向は次の一方向に固定する。

```text
foundation <- official/third-party plugins <- standard <- applications/CLI/WASM
      ^                    ^                     ^
      +--------------------+---------------------+---- integration

tools: product runtime から独立
```

- foundation は公式 plugin、standard、CLI、WASM、playback implementation を import しない。
- plugin は foundation の public contract だけに依存し、Host の `internal` package を import しない。
- `standard` は公式 plugin と Binding を組み合わせる composition root である。
- surface は `Host` の client/adaptor であり、planner、registry、runtime を再実装しない。
- cross-plugin/native/reference test は dependency graph の最上位に置く。
- SDK を第二の architecture layer として残さない。第三者向け helper は責務名を持つ public package、Host 実装は `internal` に置く。

## repository、module、package

[C13](decisions.md) に従い、三つの境界を分ける。

| 境界 | 責務 |
|---|---|
| repository | atomic change、review、CI、release coordination |
| Go module | version、download、dependency graph、配布 |
| package | compile、visibility、責務、import cycle |

package を分けるためだけに repository/module を分けない。import path の短さも module 分割理由にしない。完全な path は通常 import block にだけ現れ、runtime/binary 性能には影響しない。

### 設計・pre-v1期間

source を一つの monorepo に統合し、source code の Git submodule を廃止する。foundation、公式 pure-Go plugin、standard、基本 CLI は一つの product module/release train とし、contract と全公式利用側を一 commit で変更できるようにする。code と独立して更新・配布される任意取得の test/demo asset は data submodule として維持できる。`example/assets`、`example/web/assets`、FLAC conformance corpus がこれに該当する。通常 build/test の必須入力や production dependency にせず、新しい source submodule は追加しない。

```text
module github.com/godexture/godec
├─ foundation packages
├─ plugin/
├─ standard/
├─ cli/
└─ cmd/godec/

target/dependency が独立するものだけ nested module:
├─ tools/
├─ integration/
├─ bindings/
└─ optional playback/native adaptors
```

full conformance/benchmark corpus は product module に含めず、[fixtures](fixtures.md) の data submodule または manifest/cache policy で版・取得tier・provenanceを管理する。

### contract 安定後

最初の stable v1 前に必要な split を一度で行い、foundation、規格 family、standard distribution を独立 release 可能にする。

```text
github.com/godexture/godec
github.com/godexture/godec/plugin/flac
github.com/godexture/godec/plugin/mp3
github.com/godexture/godec/plugin/pcm
github.com/godexture/godec/plugin/wave
github.com/godexture/godec/standard
```

package path は設計期間から `github.com/godexture/godec/plugin/<family>` に固定する。同じ directory に後から `go.mod` を置いても import path と marker identity が変わらない構成にし、root module と nested module が同じ package を同時に提供する曖昧な移行期間を作らない。

nested module は、次のいずれかに具体的な実益がある場合だけ追加する。

- native/platform dependency を base distribution から隔離する。
- browser、device、tool 等、target/artifact が独立している。
- foundation と異なる release cadence が利用者に必要である。
- module download size の実測により selective download が必要である。

### M1 完了条件

M1 は repository、package identity、module/workspace topology を固定する milestone であり、最終 foundation contract や全 production import の層分離までは要求しない。

- product source とその履歴が monorepo に入り、source code の Git submodule が残っていない。
- data/asset gitlink は上記3件を独立した任意取得dependencyとして明示し、通常build/testからの分離、固定revision、license、未取得時の挙動が記録されている。
- 公式 plugin の最終 package path が `plugin/<family>` に固定され、後の family module split で import path と reflection/marker identity が変わらない。
- root product module と target/dependency が独立する nested module だけで一方向の module DAGを作る。
- clean checkout から、未追跡 `go.work` や事前生成済み実行fileに依存せず、tracked source/manifestを使って全moduleのbuild/test/generateを起動できる。
- Go native scalar/SIMD、WASM target、JS type/test の対象とskipを区別して検証し、package/repository metadataも新しいmonorepo pathを指す。
- module DAG の完了条件は設計期の repository 内 composition に限定する。`bindings/wasm`、`example/go`、`example/web/server` の zero pseudo-version と relative `replace` は正直な設計期表現だが、`replace` directive は依存する側からは無視され、zero pseudo-version は公開 repository から解決できない。したがって M1 は次だけを要求する。
  - clean checkout 内で `go.work` を使う通常 build/test/generate が成功する。
  - 各 nested module directory で `GOWORK=off go build ./...` が relative `replace` 経由で成功し、`go.work` だけに依存した import を持たない。
  - repository 外からの install、downstream consumer build、publish 順序と real version pin は M10 の release gate で扱い、M1 には要求しない。

`core`/`sdk` の責務再編は M2/M3、Format/Codec/Metadata 間の直接importを Binding/compositionへ移す作業は M8 で行う。ただし、その移行で公式 plugin の公開 package path を再変更しない。

## foundation package

`contract` という巨大 package は作らず、責務ごとの小さな package に分ける。名前は仮称だが、境界は次のとおりである。

| 領域 | package path | 責務 |
|---|---|---|
| media control plane | `media/schema`、`media/property`、`media/timing`、`media/stream`、`media/metadata`、`media/tag` | open identity、immutable property、time base、stream descriptor、semantic metadata |
| media data plane | `media/packet`、`media/side`、`media/buffer`、`media/audio`、`media/video`、`media/subtitle` | typed unit、side data、ownership |
| media extension | `media/format`、`media/codec` | Probe/Inspect、Carrier、Parser/Binding |
| graph contract | `flow`、`component` | typed port、Processor/Operator、Compile/Open definition |
| identity/config | `plugin`、`config`、`diagnostic` | marker identity、Set、typed schema、構造化診断 |
| I/O extension | `access`、`endpoint` | byte object capability/transaction、live/session/device trait |
| application API | `resource`、`job`、`host`、`testkit` | request/grant、normalized Job、public façade、conformance |

media 領域だけを `media/` 配下へ置き、それ以外は root に置く。単独では意味が読み取れない語（`side`、`property`、`buffer`、`tag`、`stream`、`schema`）は media に集中しており、`media` は容器のための造語ではなく実在する domain 語だからである。逆に `app` や `io` のような容器語を作って `host` や `access` を沈めると、`godec/host` より読みにくくなり、[AGENTS.md](../../AGENTS.md) の「一単語でより明確な意味を持つ語を先に検討する」に反する。

この配置は概念の衝突も解消する。`config.Schema`（設定 schema）と `media/schema`（data unit schema）が path で区別され、`media/audio`（frame 型）と `plugin/audio`（processor 実装）も紛れない。

`flow` と `component` は media 専用ではなく第三者の非 media schema にも使うため `media/` 配下へは置かない。2 package のために `graph/` を作る実益は薄いので root に置く。ただし両者は「component がどう繋がるか」という一つの概念なので、M3 で 1 package へ統合する余地を残す。

public package を増やすのは、第三者が実装・宣言・検証する contract がある場合だけとする。codec 内部の bitstream/parser table、Host scheduler、surface helper の都合で public package を作らない。

## private Host runtime

次は foundation module 内に置いても public contract にしない。

```text
internal/
├─ catalog/   validated immutable component index
├─ access/    Provider binding, prepared sessions, spool
├─ probe/     bounded shared probing
├─ inspect/   source topology inspection
├─ plan/      public Plan construction
├─ solve/     constraint solving and bridge selection
├─ graph/     validated compiled graph
├─ run/       scheduler, execution islands, lifecycle
├─ memory/    allocator, pools, queue storage
├─ task/      tracked task groups
├─ commit/    output transaction coordination
└─ observe/   local counters, event snapshots
```

この境界により、queue、scheduler、fusion、allocator、metrics accumulator、panic boundary を変更しても plugin source を変更せずに済む。

## 公式 plugin の凝集単位

規格 family を親 package/module とし、Codec、Format、Parser 等の role は component identity または `internal` subpackage で分ける。

```text
plugin/
├─ flac/
│  ├─ plugin.go
│  └─ internal/{codec,format,parser,bitstream}
├─ mp3/
│  ├─ plugin.go
│  └─ internal/{codec,format,parser,bitstream}
├─ pcm/
├─ wave/
├─ audio/
├─ id3/
└─ vorbiscomment/
```

通常利用者は親 package だけを import する。

```go
set := plugin.NewSet(
    mp3.Plugin(),
    flac.Plugin(),
)
```

必要な component だけを選ぶ場合も同じ親 package から取得する。

```go
set := plugin.NewSet(
    mp3.Codec(),
    mp3.Parser(),
)
```

同じ family に置くことと、Format/Codec/Parser の contract を融合することは別である。共有 bitstream invariant は `internal` で凝集し、外部利用の具体的な需要がある場合だけ小さな public API を公開する。

WAVE と PCM、MP4 と H.264、Ogg と Vorbis のように Binding だけで結ばれる独立規格は別 package にする。flat repository で同名 role を区別するための `codec-`/`format-` prefix は不要になる。一語を優先するが、`vorbiscomment` のように複合語の方が明確な正式名称は短縮しない。

## public contract と private state

Public:

- schema と immutable descriptor
- typed Reader/Writer、Processor/Operator
- component Spec、plugin Definition/Set
- config、diagnostic、resource、Access/Endpoint contract
- Job、Plan、Host、testkit

Private:

- candidate index と solver state
- dense node/edge ID と typed call path
- Program、queue、execution island、scheduler
- pool/workspace layout と metric accumulator
- plugin invocation/panic boundary

mutable state は `Host -> Job -> component/worker -> item lease` の最小 owner に置く。固定 CRC/Huffman table 等は private read-only data として共有できるが、global registry、書換可能 CPU feature、mutable default、process-wide resource pool は持たない。詳細は [runtime](runtime.md) を参照する。

## Plan と Program

`Plan` は利用者向けの immutable な説明である。component identity、stream mapping、自動挿入、config、policy、variant、effect/loss、resource、diagnostic、input snapshot を持ち、versioned DTO へ変換できる。

`Program` は Host 内部の実行 capsule である。dense index、typed function、specialized edge、allocator、execution island、plugin-private compile result を保持し、serialize しない。

```text
Job + input snapshot + Catalog
          |
          v
        Plan       public / explainable
          |
          v
       Program     private / executable
```

説明可能性のために data plane が string ID、reflection、property map、wire DTO を参照してはならない。

## 移行規則

- 現行 package の rename だけで新境界を装わず、最小の新縦断経路を先に作る。
- 公式利用側を新経路へ移した時点で旧 package と wrapper を削除する。
- deprecated alias、互換 module、二重 registry、旧新 planner を残さない。
- reusable algorithm と Host-specific mechanism を同じ utility package に混在させない。
- module split 前でも、規格 family は foundation の public package だけに依存させる。
- 現行 directory ごとの処遇は [inventory](inventory.md)、監査根拠は [findings](findings.md) を正本とする。

## 文書全体の完了条件

この節は最終 architecture の gate であり、M1 単独の完了判定には上記「M1 完了条件」だけを用いる。

- foundation から plugin/standard/surface への依存がない。
- plugin family と surface を foundation contract から独立して build/test できる。
- monorepo 内の source module graph が一方向で、source code の Git submodule を必要としない。data submodule は任意取得の test/demo asset に限る。
- public package が第三者 contract、private package が Host/implementation detail に対応する。
- package path と marker identity を変えず、stable v1 前に family module split を実施または不要と判断できる。
- runtime internal を交換しても、公式・第三者 plugin の public API を変更しない。
