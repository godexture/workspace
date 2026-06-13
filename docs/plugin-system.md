# プラグインシステム

このドキュメントでは godec のプラグインアーキテクチャの仕組みと、  
新しいプラグインの実装方法を説明します。

---

## 概要

godec のプラグインシステムは以下の原則に基づいています:

1. **型をキーとした登録** — `reflect.Type` をキーとして Registry に登録する
2. **init() による自動登録** — ブランクインポートで自動登録が完了する
3. **Capability による能力宣言** — プラグインが対応する入力を宣言し、Resolver が選択する
4. **Factory Pattern** — プラグインはファクトリ関数として登録する

---

## プラグインの種類

| 種別 | 対応インターフェース | 登録先 |
|------|------------------|--------|
| Demuxer | `node.Demuxer` | `registry.Bundle.Demuxers` |
| Muxer | `node.Muxer` | `registry.Bundle.Muxers` |
| Decoder | `node.Decoder` | `registry.Bundle.Decoders` |
| Encoder | `node.Encoder` | `registry.Bundle.Encoders` |
| Filter | `node.Filter` | `registry.Bundle.Filters` |

---

## 登録メカニズム

### 1. Config 型を定義する

Config 型が Registry の **キー** (ID) になります。

```go
type MyDemuxerConfig struct {
    // 設定フィールドを追加
    BufferSize int
}

// registry.Configuration インターフェースを実装
func (MyDemuxerConfig) NodeConfigaration() {}
```

### 2. init() でグローバルレジストリに登録する

```go
func init() {
    err := godec.DefaultRegistry.Demuxers.Register(MyDemuxerConfig{}, registry.DemuxerManifest{
        BaseManifest: registry.BaseManifest{
            Name:        "my-demuxer",
            Description: "My custom demuxer",
        },
        Probe: func(r io.Reader) manifest.ProbeScore {
            // フォーマット検出ロジック
            return manifest.ProbeExactSignature
        },
        Factory: func(r io.Reader, cfg registry.Configuration) (node.Demuxer, error) {
            // インスタンスを生成して返す
            engine := mypackage.NewDemuxer(r)
            return sdk_engine.WrapDemuxer(engine), nil
        },
    })
    if err != nil {
        panic(err)
    }
}
```

### 3. ブランクインポートで有効化する

```go
import (
    _ "github.com/myorg/my-plugin"  // このインポートで init() が実行される
)
```

---

## プラグイン実装ガイド

### Demuxer プラグインの実装

```go
// 1. Engine インターフェースを実装
type MyDemuxer struct {
    r io.ReadSeeker
}

func (d *MyDemuxer) Analyze() ([]media.StreamInfo, metadata.Bundle, error) {
    // ヘッダーを解析してストリーム情報を返す
    return []media.StreamInfo{
        {
            Index:     0,
            Type:      media.MediaAudio,
            IsDefault: true,
            MediaAttributes: media.MediaAttributes{
                Codec: media.CodecLPCM,
                Audio: media.AudioAttributes{
                    CodecID:       media.CodecLPCM,
                    SampleRate:    44100,
                    Format:        media.SampleFormatS16,
                    ChannelLayout: media.LayoutStereo2_0,
                },
            },
        },
    }, *metadata.NewBundle(), nil
}

func (d *MyDemuxer) ReadPacket() (*media.Packet, int, error) {
    // パケットを読み込んで返す
    data := make([]byte, 4096)
    n, err := d.r.Read(data)
    if err == io.EOF {
        return nil, 0, io.EOF
    }

    pkt := media.NewPacket(n)
    copy(pkt.Data(), data[:n])
    pkt.MediaType = media.MediaAudio
    pkt.StreamIndex = 0
    return pkt, 0, nil
}

// 2. Probe 関数を実装
func Probe(r io.Reader) manifest.ProbeScore {
    buf := make([]byte, 4)
    if _, err := io.ReadFull(r, buf); err != nil {
        return manifest.ProbeMismatch
    }
    if string(buf) == "MYFT" {  // マジックバイト
        return manifest.ProbeExactSignature
    }
    return manifest.ProbeMismatch
}

// 3. 登録
func init() {
    godec.DefaultRegistry.Demuxers.Register(Config{}, registry.DemuxerManifest{
        BaseManifest: registry.BaseManifest{Name: "my-format"},
        Probe: Probe,
        Factory: func(r io.Reader, _ registry.Configuration) (node.Demuxer, error) {
            rs, ok := r.(io.ReadSeeker)
            if !ok {
                return nil, fmt.Errorf("requires io.ReadSeeker")
            }
            return engine.WrapDemuxer(&MyDemuxer{r: rs}), nil
        },
    })
}
```

### Decoder プラグインの実装

```go
type MyDecoder struct {
    pending *media.Packet
    flushed bool
    config  Config
}

func (d *MyDecoder) SendPacket(pkt *media.Packet) error {
    if d.pending != nil {
        return errors.New("unconsumed packet")
    }
    d.pending = pkt
    return nil
}

func (d *MyDecoder) ReceiveFrame() (*media.Frame, error) {
    if d.pending == nil {
        if d.flushed {
            return nil, engine.ErrEOF
        }
        return nil, engine.ErrEAGAIN
    }

    // デコード処理
    pkt := d.pending
    d.pending = nil

    frame := media.NewAudioFrame(
        d.config.Format,
        d.config.Layout,
        d.config.SampleRate,
        len(pkt.Data()) / d.config.Format.BytesPerSample() / d.config.Layout.ChannelCount(),
        media.WithAudioPts(pkt.PTS),
    )
    // デコードしたデータをコピー
    copy(frame.Planes()[0], pkt.Data())

    var f media.Frame = frame
    return &f, nil
}

func (d *MyDecoder) Flush() error {
    d.flushed = true
    return nil
}

// Capability 宣言
type myCapability struct{}
func (myCapability) MediaType() media.MediaType { return media.MediaAudio }
func (myCapability) Match(s media.StreamInfo) bool {
    return s.Type == media.MediaAudio && s.Audio.CodecID == media.CodecLPCM
}
func (myCapability) Diagnose(s media.StreamInfo) bool { return s.Type == media.MediaAudio }

// 登録
func init() {
    godec.DefaultRegistry.Decoders.Register(Config{}, registry.DecoderManifest{
        TransformManifest: registry.TransformManifest{
            BaseManifest: registry.BaseManifest{Name: "my-decoder"},
            Capabilities: []manifest.Capability{myCapability{}},
            TransformFunc: func(s media.StreamInfo) media.Profile {
                return media.Profile{Type: s.Type, MediaAttributes: s.MediaAttributes}
            },
        },
        Factory: func(cfg registry.Configuration) (node.Decoder, error) {
            c := cfg.(Config)
            return engine.WrapDecoder(&MyDecoder{config: c}), nil
        },
    })
}
```

### Muxer プラグインの実装

```go
type MyMuxer struct {
    w      io.Writer
    stream media.StreamInfo
}

func (m *MyMuxer) AddStream(info media.StreamInfo) (int, error) {
    m.stream = info
    return 0, nil
}
func (m *MyMuxer) SetMetadata(meta metadata.Bundle) error { return nil }
func (m *MyMuxer) WriteHeader() error { /* ヘッダー書き込み */ return nil }
func (m *MyMuxer) WritePacket(idx int, pkt *media.Packet) error {
    _, err := m.w.Write(pkt.Data())
    return err
}
func (m *MyMuxer) WriteTrailer() error { /* フッター書き込み */ return nil }

// 登録
func init() {
    godec.DefaultRegistry.Muxers.Register(Config{}, registry.MuxerManifest{
        BaseManifest: registry.BaseManifest{Name: "my-muxer"},
        Factory: func(w io.Writer, _ registry.Configuration) (node.Muxer, error) {
            return engine.WrapMuxer(&MyMuxer{w: w}), nil
        },
    })
}
```

### Filter プラグインの実装

Filter は現在 `filter-audio` パッケージが stub として用意されています。

```go
type MyFilter struct {
    in  *node.InPort[media.Frame]
    out *node.OutPort[media.Frame]
}

func (f *MyFilter) Start(ctx context.Context) error {
    return f.Process(ctx)
}

func (f *MyFilter) Process(ctx context.Context) error {
    defer f.out.Edge().Close()
    for {
        frame, err := f.in.Pull(ctx)
        if err == io.EOF { return nil }
        if err != nil { return err }

        // フレーム変換処理
        processed := transform(frame)

        if err := f.out.Push(ctx, processed); err != nil {
            return err
        }
    }
}

func (f *MyFilter) InputPorts() map[string]*node.InPort[media.Frame] {
    return map[string]*node.InPort[media.Frame]{"in": f.in}
}
func (f *MyFilter) OutputPorts() map[string]*node.OutPort[media.Frame] {
    return map[string]*node.OutPort[media.Frame]{"out": f.out}
}
```

---

## Resolver によるプラグイン自動選択

### Demuxer の自動選択

`DefaultDemuxerResolver` は全 Demuxer の `Probe()` を呼び出し、  
スコアが最高のものを選びます:

```go
resolver := resolver.NewDefaultDemuxerResolver(registry.Demuxers)
manifest, err := resolver.ResolveDemuxer(reader)
if err != nil {
    return fmt.Errorf("unsupported format: %w", err)
}

dmx, err := manifest.Factory(reader, manifest.ID())
```

### Decoder の自動選択

`DefaultDecoderResolver` は `Capability.Accept(stream)` が true を返す  
Decoder の中から最優先のものを選びます:

```go
resolver := resolver.NewDefaultDecoderResolver(registry.Decoders)
manifest, err := resolver.ResolveDecoder(streams[0])

dec, err := manifest.Factory(nil)  // config は省略可能
```

#### 優先度の上書き

```go
resolver := resolver.NewDefaultDecoderResolver(
    registry.Decoders,
    resolver.WithPriority(mypackage.Config{}, 100),  // 高優先度
)
```

### Encoder の自動選択

```go
resolver := resolver.NewDefaultEncoderResolver(registry.Encoders)
manifest, err := resolver.ResolveEncoder(media.CodecLPCM)
enc, err := manifest.Factory(nil)
```

---

## routing.Negotiator による変換パス探索

変換パスを BFS で自動探索します。  
例: `media.SampleFormatF32` → `media.SampleFormatS16` が直接受け入れられない場合に  
間に変換フィルタを挟む候補を探します。

```go
// 全フィルタを Candidate として収集
candidates := routing.AsCandidates(registry.Filters.Enumerate())
negotiator := routing.NewNegotiator(candidates)

// src から target に到達するフィルタ列を探索
path, err := negotiator.FindPath(srcProfile, targetCandidate)
if errors.Is(err, routing.ErrNoPathFound) {
    return fmt.Errorf("no conversion path")
}

// path に含まれるフィルタを順番にパイプラインに挿入する
```

---

## go.mod の設定例

プラグインの `go.mod` は以下のような構成になります:

```
module github.com/myorg/my-plugin

go 1.26.1

require (
    github.com/godexture/core v0.x.x
    github.com/godexture/sdk  v0.x.x
)
```

> Go ワークスペース (`go.work`) 環境では `use ./path/to/plugin` を追加します。

---

## プラグイン開発チェックリスト

- [ ] Config 型に `NodeConfigaration()` メソッドを実装した
- [ ] Engine インターフェース (`DemuxerEngine` / `DecoderEngine` 等) を実装した
- [ ] `init()` でグローバルレジストリに登録した
- [ ] Demuxer の場合は `Probe()` 関数を実装し、適切な `ProbeScore` を返している
- [ ] Decoder/Encoder/Filter の場合は `Capability` を宣言した
- [ ] `ErrEAGAIN` と `ErrEOF` を適切に返している
- [ ] `Flush()` を正しく実装している (フラッシュ後も残りデータを `Receive*()` で取れる)
- [ ] 単体テストを作成した
- [ ] WAV/PCM roundtrip などの統合テストで動作確認した
