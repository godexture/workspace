# アーキテクチャ概要

## 設計原則

godec は plugin 可能な negotiated media pipeline です。

- core は codec、container、filter 固有の config や実装を知らない
- plugin の ID は role と config 型の reflection から registry が強制的に生成する
- semantic config と execution resource を分離する
- topology 全体を確定してから CPU budget を配分する
- Node の ownership と Close を Geometry / Pipeline が一貫して管理する
- map 順序、worker 完了順、resource 配分に依存しない決定的な結果を目指す

## 主要 package

```text
core/
├── domain/      media、metadata、manifest、time
├── node/        Node、typed Port、Edge、mandatory lifecycle
├── registry/    PluginKey、Manifest、Resource contract、Registry
├── resolver/    role ごとの manifest 選択
├── routing/     ConversionSpec から ordered topology を negotiation
├── pipeline/    Geometry、Builder、Pipeline、ChanEdge
└── factory/     custom Registry / Resolver を組み立てる Provider

sdk/
└── engine/      plugin 向け Engine API と Node Adapter

plugins/
├── codec-*/
├── format-*/
└── filter-*/
```

依存方向は Application → Plugin / SDK → Core → Domain です。Core から個別 Plugin への依存はありません。

## 登録と選択

```text
named config type
      │
      ▼
Registry.Register(config, manifest)
      │  validate config identity + manifest contract
      ▼
PluginKey(role, reflect.Type)
      │
      ▼
Resolver ── capability / codec / probe / exact config key
```

`PluginKey` は registry 内でのみ生成され、plugin が手動 ID を注入する API はありません。Registry は snapshot を key 順に列挙するため、同順位の選択も決定的です。

## Negotiation

標準 topology は次の順です。

```text
Demuxer → Decoder → Filter[0] → … → Filter[n] → Encoder → Muxer
```

Negotiator は次の phase を順に実行します。

1. Demuxer を解決・作成し、入力 stream を調べる
2. Decoder manifest と出力 profile を解決する
3. 明示された Filter を config key で解決し、指定順に profile を伝播する
4. Encoder manifest と出力 profile を解決する
5. Muxer manifest を解決する
6. 全 transform の resource request を一度に配分する
7. resource budget を factory options として渡し Node を構築する
8. Node と Edge 定義を所有する Geometry を返す

途中で失敗した場合、作成済み Node は逆順に Close されます。

## Topology-aware resource budget

Plugin は並列実行可能かだけを宣言します。

```go
Resources: registry.ResourceRequest{Parallelism: true}
```

CPU budget は decoder / filter / encoder のうち実際に topology に含まれる並列対応 stage へ均等配分されます。余りは stage 順に 1 ずつ配ります。budget より stage 数が多い場合も各 stage に最低 1 を渡し、1 は同期実行を意味します。

```text
budget 8, parallel stages [decoder, filter0, encoder]
→ [3, 3, 2]
```

Resource は semantic config ではありません。同じ入力と config は割り当てが変わっても同一出力でなければなりません。

## データフロー

```text
io.ReadSeeker
     │
 Demuxer ── *Packet ── Decoder ── Frame ── Filter* ── Frame ── Encoder ── *Packet ── Muxer
                                                                                       │
                                                                                   io.Writer
```

Node 間は bounded `ChanEdge` で接続され、context cancellation と backpressure を伝播します。Codec Adapter は upstream EOF で Flush し、残りの出力を drain してから downstream Edge を閉じます。

## Ownership と lifecycle

```text
Negotiator
   │ success: ownership transfer
   ▼
Geometry
   │ Builder.Build success
   ▼
Pipeline
```

すべての Node は `Start(context.Context) error` と idempotent な `Close() error` を実装します。

- Geometry を破棄すると、Build 前の Node を逆順に Close
- Build 失敗時は Builder が全 Node を Close
- Pipeline.Run は全 Start を errgroup で実行
- 最初のエラーまたは外部 Close で context を cancel
- 全 Start の終了後、Pipeline が全 Node を逆順に一度だけ Close
- Run error と Close error は両方保持

Pipeline は single-use です。この制約により再利用時の閉じた Edge、消費済み Engine、二重 cleanup を排除します。

## Plugin worker lifecycle

並列 Plugin の pool は次を満たします。

- process-global ではなく instance-owned
- constructor では起動せず、最初の並列 work で lazy start
- parallelism 1 は goroutine/channel を使わない
- queue は bounded で backpressure を維持
- Flush は受理済み work を完了させる
- Close は worker を停止して join し、複数回呼べる

これにより、pipeline 内の各 stage が独立に `GOMAXPROCS` worker を作る oversubscription を防ぎます。

## メモリ管理

`media.Packet` と `media.AudioFrame` は参照カウントで管理します。共有する場合は `Retain`、所有権を手放す場合は `Release` を呼び、参照数 0 で backing buffer を pool に返します。Pipeline lifecycle と media resource lifecycle は別であり、Node.Close が未解放 Frame を暗黙に正当化するものではありません。
