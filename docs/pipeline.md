# パイプライン構築と実行

`core/pipeline` は Node の接続だけでなく、構築から終了までの所有権を管理します。

## Node と接続

すべての `node.Node` は次の lifecycle を実装します。

```go
type Lifecycle interface {
    Start(context.Context) error
    Close() error
}
```

`Start` は処理を実行し、`Close` は node が所有する goroutine、buffer、file などを解放します。`Close` は複数回呼ばれても安全でなければなりません。SDK の Engine Adapter はこの契約を実装し、Engine が `Close() error` を持つ場合は一度だけ転送します。

Node 間は型付き Port と Edge で接続します。

| Node | 入力 | 出力 |
|---|---|---|
| `Demuxer` | — | `*media.Packet` |
| `Decoder` | `*media.Packet` | `media.Frame` |
| `Filter` | `media.Frame` | `media.Frame` |
| `Encoder` | `media.Frame` | `*media.Packet` |
| `Muxer` | `*media.Packet` | — |

`pipeline.Link` は既定容量 100 の `ChanEdge` を作ります。容量を制御する場合は `LinkWithBufferSize` を使います。

```go
if err := pipeline.Link(demuxer, "out", decoder, "in"); err != nil {
    return err
}
```

## Geometry と ownership

`Geometry` は未接続の Node と Edge 定義を所有します。

```go
geometry := pipeline.NewGeometry()
if err := geometry.AddNode("decoder", decoder); err != nil {
    return err
}
if err := geometry.AddNode("encoder", encoder); err != nil {
    return err
}
if err := geometry.AddEdge("decoder", "out", "encoder", "in"); err != nil {
    return err
}
```

Geometry を使わない場合は `Close` します。`Builder.Build` が成功すると ownership は Pipeline へ移り、以後 `Geometry.Close` は Node を閉じません。Build 中の検証・接続に失敗した場合は Builder が全 Node を逆順に閉じます。

```go
defer geometry.Close()

conversion, err := pipeline.NewBuilder().Build(geometry)
if err != nil {
    return err
}
```

空 ID、nil Node、重複 ID、不完全な Edge、互換性のない Port はエラーになります。

## Description と観測モード

Negotiator は demuxer、decoder、明示 filter、自動 bridge、encoder、muxer の role、plugin、有効 configuration、入出力 stream、resource 割当を `Geometry` に登録します。`Geometry.Description()` は Build 前、`Pipeline.Description()` は ownership 移譲後の解決済み構造を返します。返り値は複製されるため、呼び出し側で変更しても実行中の Pipeline には影響しません。

```go
description := geometry.Description()

conversion, err := pipeline.NewBuilder().Build(
    geometry,
    pipeline.WithObservation(pipeline.ObservationProgress),
)
```

観測モードは次の3段階です。

| mode | 実行経路 |
|---|---|
| `ObservationOff` | 全 edge を通常の `ChanEdge` へ直接接続し、collector や sampler を作らない |
| `ObservationProgress` | 選択入力の progress-source edge だけを包み、item 数と最大メディア時刻を atomic 更新する |
| `ObservationMetrics` | 全 edge の item、payload byte、audio sample、最大メディア時刻と、全 node の状態・時間を記録する |

`Pipeline.Snapshot()` は実行前、並行実行中、完了・失敗・キャンセル後のいずれでも安全に呼び出せます。Off/Progress で収集対象外の値はゼロまたは `unobserved` です。

## CLI のパイプライン観測

`godec convert` は次の option を持ちます。実変換の表示は標準エラー、dry-run の構造だけは標準出力へ書きます。

| option | 動作 |
|---|---|
| `--progress=auto\|always\|never` | `auto` は TTY のみ250ms間隔で同じ行を更新する。非TTYの `always` は1秒ごとに改行する |
| `-v`, `--verbose` | plugin、configuration、parallelism、全入力・選択・予定出力 stream、node/edge 構造を表示する |
| `--metrics` | 成功・失敗・キャンセル時に timing、I/O、node、edge、Go runtime 統計を表示する |
| `--dry-run` | 出力を作らず negotiation と Build を検証し、解決済み構造を標準出力へ表示する |

`--dry-run` と `--metrics` は同時指定できません。dry-run 中は progress を開始しません。`auto` が非TTYかつ metrics 無効なら Build 前に `ObservationOff` が選ばれます。

進捗率は progress-source stream の最大メディア時刻を `StreamInfo.Duration` で割った値を優先します。尺が未知なら入力 `ReadSeeker` の論理位置とファイルサイズ、どちらも使えなければ item 数と経過時間を表示します。

## Pipeline

`Pipeline` は single-use です。全 Node の `Start` を `errgroup` で並行実行し、正常終了・エラー・キャンセルのいずれでも、全 Node の終了後に逆順で `Close` します。実行エラーと Close エラーは `errors.Join` で両方返します。

```go
if err := conversion.Run(ctx); err != nil {
    return err
}
```

`Close` は次の用途に使います。

- Build 後、Run 前に処理を中止する
- 実行中の Pipeline をキャンセルし、終了を待つ
- Run 後に安全に再度 cleanup を要求する

```go
defer conversion.Close()
```

実行中に `Close` すると Pipeline の内部 context がキャンセルされます。各 Node は `Start` 内の Edge 操作や長時間処理で context を尊重しなければなりません。

手動で接続済みの Node も、所有権を Pipeline に渡して実行します。

```go
conversion, err := pipeline.New(demuxer, decoder, encoder, muxer)
if err != nil {
    return err
}
return conversion.Run(ctx)
```

## 正常終了シーケンス

1. Demuxer が出力 Edge を閉じる
2. Decoder が `io.EOF` を受けて Flush し、出力を drain して Edge を閉じる
3. Filter と Encoder も同様に Flush・drain する
4. Muxer が `WriteTrailer` を実行する
5. Pipeline が全 `Start` の終了を待つ
6. Pipeline が Muxer から Demuxer の順に `Close` する

途中でエラーが発生すると errgroup の context がキャンセルされ、同じ Close シーケンスへ合流します。

## Negotiator を使う例

```go
geometry, err := godec.NewNegotiator().NegotiateConversion(ctx, routing.ConversionSpec{
    Input:       input,
    Output:      output,
    TargetCodec: media.CodecFLAC,
    MuxConfig:   flacformat.NewMuxerConfig(),
})
if err != nil {
    return err
}
defer geometry.Close()

conversion, err := godec.NewBuilder().Build(geometry)
if err != nil {
    return err
}
defer conversion.Close()

return conversion.Run(ctx)
```

Negotiator は失敗時に構築済み Node を閉じ、成功時に ownership を Geometry へ渡します。

## Engine API を直接使う場合

Engine を直接使う場合、呼び出し側が `Flush`、出力の drain、Engine が実装する `Close` を管理します。

| エラー | 意味 |
|---|---|
| `engine.ErrEAGAIN` | 現時点で出力がない |
| `engine.ErrEOF` | Flush 後の出力をすべて返した |
| `io.EOF` | upstream Edge が閉じた |
