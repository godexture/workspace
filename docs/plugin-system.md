# プラグインシステム

godec は core が codec・container・filter 固有の実装を知らない plugin architecture を採用しています。Plugin は config 型、manifest、factory を登録し、core は capability と汎用 resource contract だけを扱います。

## Plugin の種類

| Role | Manifest | Node |
|---|---|---|
| Demuxer | `registry.DemuxerManifest` | `node.Demuxer` |
| Decoder | `registry.DecoderManifest` | `node.Decoder` |
| Filter | `registry.FilterManifest` | `node.Filter` |
| Encoder | `registry.EncoderManifest` | `node.Encoder` |
| Muxer | `registry.MuxerManifest` | `node.Muxer` |

## Reflection 由来の強制 ID

Plugin 開発者は ID を指定しません。Registry は登録時に次の組から `PluginKey` を生成します。

```text
(manifest role, reflect.TypeOf(config) を非 pointer 化した型)
```

`PluginKey` のフィールドと manifest 内の key は非公開で、手動 ID や文字列 ID への fallback はありません。これにより、ID の生成忘れ、衝突、rename 後の文字列不整合を避けられます。

Config は package で宣言された named concrete type でなければなりません。Registry は次を拒否します。

- nil、typed nil pointer
- builtin、匿名 struct、map、slice、interface
- 同じ role と config 型の重複登録

Value と pointer は同じ key になります。

```go
type DecoderConfig struct {
    Strict bool
}

if err := godec.Register(MustNewDecoderConfig(), decoderManifest); err != nil {
    panic(err)
}
```

Config に ID 用メソッドを実装する必要はありません。Config の reflection は登録・検索・診断だけで使われ、設定値の生成や mutation、codec の hot path には使われません。

## Manifest の契約

登録時に role 共通契約が検証されます。

- 全 role: 空でない `Name`
- Demuxer: `Probe` と `Factory`
- Muxer: `Factory`
- Decoder / Filter: `InputRequirements` と `Factory`
- Encoder: `InputRequirements`、`Supports`、`Factory`

不完全な manifest は plugin の `init` 時点で失敗するため、処理開始後に nil function panic にはなりません。

## Semantic config と execution resources

`Configuration` は出力内容を決める semantic config です。worker 数などの実行戦略を config に混ぜてはいけません。同じ semantic config と入力は resource 配分に関係なく同じ出力を生成する必要があります。

並列処理を利用できる transform は manifest で宣言します。

```go
TransformManifest: registry.TransformManifest{
    BaseManifest: registry.BaseManifest{Name: "my-decoder"},
    InputRequirements: registry.SingleInputRequirements(registry.StaticRequirements(myCapability{})),
    Resources: registry.ResourceRequest{
        Parallelism: true,
    },
}
```

Negotiator は decoder、明示された全 filter、encoder の topology を確定してから、pipeline 全体の budget を配分します。Factory は semantic config と input stream から実際の output stream を返し、resource grant は Factory ではなく `registry.Preparer.Prepare` で受け取ります。

```go
Factory: func(
    stream media.StreamInfo,
    options registry.TransformFactoryOptions,
) (node.Decoder, media.StreamInfo, error) {
    config, err := engine.ResolveConfig[internalConfig, DecoderConfig](options.Config)
    if err != nil {
        return nil, media.StreamInfo{}, err
    }
    output := stream.Clone()
    output.Codec = media.CodecLPCM
    return engine.WrapDecoder(newDecoder(config)), output, nil
}
```

並列対応 node は `Prepare(registry.ResourceGrant)` で shared worker pool を受け取ります。pool が nil の場合は同期実行することを推奨します。

## Decoder plugin の例

```go
import "fmt"

type flacCapability struct{}

func (flacCapability) Match(stream media.StreamInfo) bool {
    return stream.Type == media.MediaAudio && stream.Codec == media.CodecFLAC
}
func (c flacCapability) Diagnose(stream media.StreamInfo) error {
	if c.Match(stream) { return nil }
	return fmt.Errorf("not FLAC audio")
}

func init() {
    if err := godec.Register(MustNewDecoderConfig(), registry.DecoderManifest{
        TransformManifest: registry.TransformManifest{
            BaseManifest: registry.BaseManifest{
                Name:        "my-flac-decoder",
                Description: "FLAC decoder",
            },
            InputRequirements: registry.SingleInputRequirements(registry.StaticRequirements(flacCapability{})),
            Resources: registry.ResourceRequest{Parallelism: true},
        },
        Factory: func(
            stream media.StreamInfo,
            options registry.TransformFactoryOptions,
        ) (node.Decoder, media.StreamInfo, error) {
            resolved, err := engine.ResolveConfig[internalConfig, DecoderConfig](options.Config)
            if err != nil {
                return nil, media.StreamInfo{}, err
            }
            output := stream.Clone()
            output.Codec = media.CodecLPCM
            return engine.WrapDecoder(newDecoder(stream, resolved)), output, nil
        },
    }); err != nil {
        panic(err)
    }
}
```

Encoder factory は target codec も受け取ります。

```go
type EncoderFactory func(
    input media.StreamInfo,
    target media.CodecID,
    options registry.TransformFactoryOptions,
) (node.Encoder, media.StreamInfo, error)
```

Filter factory は Decoder と同じ options を受け取り、`media.Frame` を入出力します。

## Engine と Node lifecycle

SDK の Adapter を使うと Engine API を Node に変換できます。

```go
return engine.WrapDecoder(decoderEngine)
```

Factory は cheap な config 検証と output `StreamInfo` の決定だけを行います。resource 依存または高コストな初期化が必要な node は optional な `registry.Preparer` を実装し、Pipeline が `Start` 前に `Prepare(registry.ResourceGrant)` を一度呼びます。Node 自体は `Start(context.Context) error` と idempotent な `Close() error` を実装します。Engine が resource を所有する場合は `Close() error` を実装してください。Adapter が Pipeline の Close を Engine へ一度だけ転送します。

Worker pool は instance-owned かつ lazy にし、constructor や package `init` で goroutine を開始しないでください。`Flush` は受理済みの仕事を drain し、`Close` は正常・エラー・キャンセルの全経路で resource を解放します。

## Resolver

Demuxer は Probe score、Decoder は input capability、Encoder は target codec により選択されます。同順位の場合も Registry の key 順で決定的です。

```go
decoderResolver := resolver.NewDefaultDecoderResolver(
    registry.Decoders,
    resolver.WithPriority(mycodec.DecoderConfig{}, 100),
)
```

Muxer と Filter は config 型から reflection key を導出し、該当 manifest を直接選択します。型が違う config は `engine.ResolveConfig` でも fail-fast し、zero config へ黙って変換されません。

## Negotiated conversion

Filter は config で明示し、指定順に topology へ入ります。

```go
geometry, err := negotiator.NegotiateConversion(ctx, routing.ConversionSpec{
    Input:       input,
    Output:      output,
    DecodeConfig: mycodec.MustNewDecoderConfig(),
    Filters: []routing.FilterSpec{
        {Config: resample.NewConfig()},
        {Config: normalize.NewConfig()},
    },
    TargetCodec: media.CodecFLAC,
    EncodeConfig: mycodec.MustNewEncoderConfig(),
    MuxConfig:    flacformat.MustNewMuxerConfig(),
    Resources: registry.ResourceBudget{
        Parallelism: runtime.GOMAXPROCS(0),
    },
})
```

`Resources.Parallelism == 0` は `runtime.GOMAXPROCS(0)` を意味します。Negotiator は全 manifest と profile 遷移を解決してから Node を構築し、失敗時は構築済み Node を閉じます。成功時の ownership は Geometry、Build 後は Pipeline に移ります。

## Plugin 開発チェックリスト

- [ ] role ごとに一意な named config 型を宣言した
- [ ] config に execution resource を混ぜていない
- [ ] manifest の Name、Factory、Capability、Probe / Supports を満たした
- [ ] resource 配分が変わっても出力が同一である
- [ ] parallelism 1 で不要な goroutine/channel を作らない
- [ ] worker pool を instance-owned・lazy にした
- [ ] `Flush` と idempotent な `Close` を実装した
- [ ] context cancellation で Node が終了できる
- [ ] sequential / parallel の決定性テストを追加した
- [ ] lifecycle、race、統合テストを追加した
