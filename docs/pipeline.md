# パイプライン構築と実行

このドキュメントでは `core/pipeline` パッケージを使って  
メディア処理パイプラインを組み立て・実行する方法を説明します。

---

## 基本概念

### Node

パイプラインを構成する各処理ユニットです。  
`node.Node` インターフェース (`Start(ctx) error`) を実装します。

| ノード種別 | 入力 | 出力 | 役割 |
|-----------|------|------|------|
| `Demuxer` | なし | `*media.Packet` | コンテナから読み込む |
| `Decoder` | `*media.Packet` | `media.Frame` | デコード |
| `Filter` | `media.Frame` | `media.Frame` | フレーム変換 |
| `Encoder` | `media.Frame` | `*media.Packet` | エンコード |
| `Muxer` | `*media.Packet` | なし | コンテナに書き出す |

### Port

ノードが持つ接続口です。

- `InPort[T]` — 入力ポート。`Pull(ctx)` でデータを受け取る
- `OutPort[T]` — 出力ポート。`Push(ctx, data)` でデータを送る

### Edge

ポートとポートを繋ぐ通信路です。  
現在の実装は `ChanEdge[T]` (バッファ付き Go チャネル, デフォルト容量 100)。

---

## パイプラインの組み立て

### `pipeline.Link()` 関数

```go
func Link[T any, A node.OutputNode[T], B node.InputNode[T]](
    nodeA A, portA string,
    nodeB B, portB string,
) error
```

- `nodeA` の `portA` という名前の出力ポートと、`nodeB` の `portB` という名前の入力ポートを接続します
- 内部で `NewChanEdge[T](100)` を生成し、両ポートに `Connect()` します
- 型はコンパイル時に自動推論されます

```go
// Demuxer(out) → Decoder(in)
err := pipeline.Link(demuxNode, "out", decNode, "in")

// Decoder(out) → Encoder(in)
err = pipeline.Link(decNode, "out", encNode, "in")

// Encoder(out) → Muxer(in)
err = pipeline.Link(encNode, "out", muxNode, "in")
```

#### ポート名

現在の実装では各 Adapter は以下のポート名を使用します:

| Adapter | 入力ポート名 | 出力ポート名 |
|---------|------------|------------|
| `DemuxerAdapter` | — | `"out"` |
| `DecoderAdapter` | `"in"` | `"out"` |
| `EncoderAdapter` | `"in"` | `"out"` |
| `MuxerAdapter` | `"in"` | — |

---

## パイプラインの実行

### `pipeline.Runner`

```go
runner := pipeline.NewRunner()
err := runner.Run(ctx, []node.Node{demuxNode, decNode, encNode, muxNode})
```

- 各ノードの `Start(ctx)` を `golang.org/x/sync/errgroup` で並行実行します
- 最初にエラーを返したノードのエラーが `Run()` の戻り値になります
- すべてのノードが正常終了すると `nil` を返します
- `ctx` をキャンセルすると、全ノードのチャネル読み書きがキャンセルされます

### 終了シーケンス

```
1. Demuxer が全パケットを送信し終えると out.Close() を呼ぶ
2. Decoder は in.Pull() で io.EOF を受け取り Flush() を呼び、flushed フレームを送信後 out.Close() を呼ぶ
3. Encoder も同様に Flush() → out.Close()
4. Muxer は in.Pull() で io.EOF を受け取り WriteTrailer() を呼んで終了
5. errgroup がすべての goroutine の終了を待って Run() が返る
```

---

## 完全な使用例

以下は WAV ファイルを読み込み、PCM データを roundtrip するパイプラインです。

```go
package main

import (
    "bytes"
    "context"
    "os"

    pcm "github.com/godexture/codec-pcm"
    "github.com/godexture/core/node"
    "github.com/godexture/core/pipeline"
    wav "github.com/godexture/format-wav"
    eng "github.com/godexture/sdk/engine"
)

func main() {
    inputData, _ := os.ReadFile("input.wav")

    // 1. Engine を作成
    demuxEngine, _ := wav.NewDemuxerEngine(bytes.NewReader(inputData))
    streams, meta, _ := demuxEngine.Analyze()

    a := streams[0].MediaAttributes.Audio
    decEngine := pcm.NewDecoderEngine(pcm.NewConfigWithAudio(a.SampleRate, a.Format, a.ChannelLayout))
    encEngine := pcm.NewEncoderEngine(pcm.EncoderConfig{})

    var out bytes.Buffer
    muxEngine := wav.NewMuxerEngine(&out)
    muxEngine.AddStream(streams[0])
    muxEngine.SetMetadata(meta)

    // 2. Engine → Node に変換
    demuxNode := eng.WrapDemuxer(demuxEngine)
    decNode   := eng.WrapDecoder(decEngine)
    encNode   := eng.WrapEncoder(encEngine)
    muxNode   := eng.WrapMuxer(muxEngine)

    // 3. ノードを接続
    pipeline.Link(node.Demuxer(demuxNode), "out", node.Decoder(decNode), "in")
    pipeline.Link(node.Decoder(decNode), "out", node.Encoder(encNode), "in")
    pipeline.Link(node.Encoder(encNode), "out", node.Muxer(muxNode), "in")

    // 4. 実行
    runner := pipeline.NewRunner()
    if err := runner.Run(context.Background(), []node.Node{
        demuxNode, decNode, encNode, muxNode,
    }); err != nil {
        panic(err)
    }

    os.WriteFile("output.wav", out.Bytes(), 0644)
}
```

---

## Engine API を直接使う場合

Node/Pipeline を使わず、Engine インターフェースを直接呼ぶ低レベルなスタイルです。  
`example/pcm.go` はこのスタイルで書かれています。

```go
// 読み込みループ
for {
    pkt, _, err := demuxEngine.ReadPacket()
    if err == io.EOF { break }

    decEngine.SendPacket(pkt)

    for {
        frame, err := decEngine.ReceiveFrame()
        if err == engine.ErrEAGAIN { break }  // まだデータが貯まっていない

        encEngine.SendFrame(frame)

        for {
            outPkt, err := encEngine.ReceivePacket()
            if err == engine.ErrEAGAIN { break }

            muxEngine.WritePacket(0, outPkt)
        }
    }
}

// フラッシュ
decEngine.Flush()
// ... 残りのフレームをドレイン
encEngine.Flush()
// ... 残りのパケットをドレイン
muxEngine.WriteTrailer()
```

### `ErrEAGAIN` と `ErrEOF`

| エラー | 意味 | 対応 |
|-------|------|------|
| `engine.ErrEAGAIN` | まだ出力できるデータがない | 再度 `SendPacket/SendFrame` してから `Receive*` を呼ぶ |
| `engine.ErrEOF` | フラッシュ完了・これ以上データなし | ループを終了する |
| `io.EOF` | `Edge.Pull()` で upstream が閉じた | ループを終了する |

---

## 将来の拡張

現在 `ResolverBundle` フィールドを持つ `NewPipeline()` が定義されていますが、  
Resolver を使った自動配線 (`ContainerResolver`, `CodecResolver`) は未実装です。  
`routing.Negotiator` (変換パス BFS 探索) と組み合わせた自動パイプライン構築が  
想定されています。
