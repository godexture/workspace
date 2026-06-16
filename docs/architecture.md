# アーキテクチャ概要

## 設計思想

godec は **プラグイン可能なメディア処理パイプライン** として設計されています。

- **フォーマット (Container)** の読み書きは *Demuxer / Muxer* が担当する
- **コーデック** の変換は *Decoder / Encoder* が担当する
- **フィルタ処理** は *Filter* が担当する
- これらを **Node** として抽象化し、**Edge** (チャネル) で接続してパイプラインを構成する
- プラグインは実装クラスを **Registry** に登録するだけでよく、コアロジックを変更する必要はない

## リポジトリレイアウト

```
packages/
├── core/               # コアフレームワーク (github.com/godexture/core)
│   ├── domain/         # ドメインモデル（エンティティ・値オブジェクト）
│   │   ├── manifest/   # プラグイン能力宣言 (Capability, ProbeScore, NodeType)
│   │   ├── media/      # メディアデータ (Frame, Packet, StreamInfo, Profile, Codec ...)
│   │   ├── metadata/   # タグ/メタデータ (Bundle, KeyXxx)
│   │   └── time/       # タイムスタンプ (Rational, Pts, Dts)
│   ├── node/           # ノードインターフェース (Node, Filter, Encoder, Decoder, Muxer, Demuxer)
│   ├── pipeline/       # パイプライン組み立て・実行 (Link, Runner, ChanEdge)
│   ├── registry/       # プラグイン登録管理 (Registry[V], Bundle, Manifest)
│   ├── resolver/       # 自動解決ロジック (Demuxer/Decoder/Encoder/Muxer Resolver)
│   ├── routing/        # 変換パス決定・探索 (Negotiator, Router, Candidate)
│   ├── internal/
│   │   └── xsync/      # スレッドセーフ Map (内部使用)
│   └── test/           # 統合テスト
│
├── pkg/                # SDK ユーティリティ (github.com/godexture/sdk)
│   ├── engine/         # Engine インターフェース＋Adapter (プラグイン実装を Node に変換)
│   ├── pool/           # バイトスライスプール
│   ├── hash/           # FNV-1a ハッシュ
│   └── bits/           # ビット操作 (stub)
│
├── plugins/            # 同梱プラグイン
│   ├── codec-pcm/      # PCM / G.711 コーデック (github.com/godexture/codec-pcm)
│   ├── filter-audio/   # オーディオフィルタ stub (github.com/godexture/filter-audio)
│   └── format-wav/     # WAV コンテナ (github.com/godexture/format-wav)
│
├── cli/                # CLI ツール stub (github.com/godexture/cli)
├── example/            # 使用例 (example/pcm.go)
└── go.work             # Go ワークスペース定義
```

## データフロー

標準的な **トランスコード** パイプラインのデータフローを示します。

```
┌──────────┐   *Packet   ┌──────────┐   Frame    ┌──────────┐   Frame    ┌──────────┐   *Packet   ┌──────────┐
│  Demuxer │ ──────────► │  Decoder │ ─────────► │  Filter  │ ─────────► │  Encoder │ ──────────► │  Muxer   │
│(container│             │ (codec)  │             │(optional)│             │ (codec)  │             │(container│
│  reader) │             └──────────┘             └──────────┘             └──────────┘             │ writer)  │
└──────────┘                                                                                        └──────────┘

    ↑ io.ReadSeeker                                                                                      ↑ io.Writer
```

各ノード間の接続は **Edge** (バッファ付きチャネル) を介して行われます。  
`pipeline.Link()` を呼ぶことで Edge が自動的に生成・接続されます。

## レイヤー構造

```
┌─────────────────────────────────────────┐
│             Application                 │  example/, cli/
├─────────────────────────────────────────┤
│          Plugin Layer                   │  plugins/{codec-pcm, format-wav, filter-audio}
│   (register.go → init() で自動登録)     │
├─────────────────────────────────────────┤
│           SDK Layer                     │  pkg/engine (Engine → Node Adapter)
│                                         │  pkg/pool, pkg/hash
├─────────────────────────────────────────┤
│          Core Framework                 │  core/{node, pipeline, registry, resolver, routing}
│                                         │
│  ┌──────────┐ ┌──────────┐             │
│  │ pipeline │ │ registry │             │
│  │  Runner  │ │  Bundle  │             │
│  └──────────┘ └──────────┘             │
│  ┌──────────┐ ┌──────────┐             │
│  │ resolver │ │ routing  │             │
│  └──────────┘ └──────────┘             │
├─────────────────────────────────────────┤
│         Domain Layer                    │  core/domain/{media, metadata, manifest, time}
│  (純粋なエンティティ・値オブジェクト)    │
└─────────────────────────────────────────┘
```

## 並行処理モデル

- `pipeline.Runner.Run()` は各ノードの `Start()` を **errgroup** で並行実行する
- ノード間の通信は **バッファ付きチャネル** (`ChanEdge`, デフォルトバッファ 100) を使用
- 上流ノードが EOF を返すと `edge.Close()` が呼ばれ、下流ノードは `io.EOF` を受け取り終了する
- すべてのノードが終了するまで `Runner.Run()` はブロックする
- キャンセルは `context.Context` を通じて全ノードに伝播する

## メモリ管理

- `media.Packet` および `media.AudioFrame` は **参照カウント** で管理される
- バッキングストアは `pkg/pool` の **サイズ別バイトスライスプール** から取得
- `Retain()` / `Release()` によって所有権を明示的に管理する
- `Release()` によって参照カウントが 0 になると、プールにメモリが返却される

## 依存グラフ (Go モジュール)

```
cli ──────────────────────────────────────────────────────► (none currently)

example ──┬──► core
          ├──► codec-pcm ──► core, sdk
          └──► format-wav ──► core, sdk

codec-pcm ──┬──► core
            └──► sdk

format-wav ──┬──► core
             └──► sdk

core ──────────► golang.org/x/sync

sdk (pkg) ──────► core
```
