# 開発ガイド

このドキュメントでは godec の開発環境セットアップ、コーディング規約、テスト方針について説明します。

---

## 開発環境セットアップ

### 必要なもの

| ツール | バージョン | 備考 |
|-------|----------|------|
| Go | 1.26.1 以上 | go.work / go.mod で指定 |
| Git | 任意 | バージョン管理 |

### リポジトリのクローン

```bash
git clone https://github.com/godexture/core.git
cd core/packages
```

### Go ワークスペースについて

`packages/go.work` により以下のモジュールがワークスペース内で参照されます:

```
go.work
├── ./cli           → github.com/godexture/cli
├── ./core          → github.com/godexture/core
├── ./example       → (main パッケージ)
├── ./pkg           → github.com/godexture/sdk
├── ./plugins/codec-pcm   → github.com/godexture/codec-pcm
├── ./plugins/filter-audio → github.com/godexture/filter-audio
└── ./plugins/format-wav  → github.com/godexture/format-wav
```

ワークスペース内では `go get` でのバージョン解決が不要です。  
`go build ./...` や `go test ./...` はワークスペースルートから実行できます。

---

## テスト

### テストの実行

```powershell
# ワークスペース全体のテスト
cd packages
go test ./...

# 特定モジュールのテスト
cd core
go test ./...

# 特定パッケージのテスト
go test ./core/test/...

# 詳細出力
go test -v ./...

# 特定テスト関数のみ
go test -run TestWaveFilesInDataRoundtrip ./core/test/...
```

### テストアセット

統合テストは `core/test/assets/` 以下の WAV ファイルを使用します。  
テストを実行する前に、WAV ファイルを用意してください。

---

## テスト方針

### テストの分類

| テストの種類 | 場所 | 内容 |
|-----------|------|------|
| 単体テスト | 各パッケージ内 `*_test.go` | 個別の関数・型のテスト |
| 統合テスト | `core/test/` | 複数パッケージを組み合わせたフロー |
| システムテスト | `core/test/` | roundtrip (入力=出力) の確認 |
| プラグインテスト | `plugins/*/internal/*_test.go` | プラグイン固有の動作確認 |

### roundtrip テスト

godec の品質保証の中心は **roundtrip テスト** です:

```
入力ファイル → Demux → Decode → Encode → Mux → 出力ファイル
出力バイト列 == 入力バイト列 であることを確認
```

現在の統合テスト:

| テスト関数 | ファイル | 内容 |
|-----------|---------|------|
| `TestWaveFilesInDataRoundtrip` | `wav_data_roundtrip_test.go` | WAV → Demux → Mux → WAV roundtrip |
| `TestWAVDemuxerMuxerRoundtrip` | `wav_system_test.go` | インメモリ WAV roundtrip |
| `TestWaveFilesDemuxDecodeEncodeMuxRoundtrip` | `wav_pcm_integration_test.go` | WAV + PCM デコード・エンコード roundtrip |
| `TestRunnerPipeline_WavPcmRoundtrip` | `runner_integration_test.go` | pipeline.Runner を使った roundtrip |

### テストの書き方

```go
package test

import (
    "bytes"
    "testing"

    "github.com/godexture/core/domain/media"
    wav "github.com/godexture/format-wav"
)

func TestMyFeature(t *testing.T) {
    // 1. テストデータ準備
    var buf bytes.Buffer
    muxer := wav.NewMuxerEngine(&buf)
    // ...

    // 2. 実行
    if err := doSomething(); err != nil {
        t.Fatalf("doSomething() error = %v", err)
    }

    // 3. 検証
    if got != want {
        t.Errorf("got %v, want %v", got, want)
    }
}
```

---

## コーディング規約

### 全般

- Go 標準の `gofmt` でフォーマット
- エラーは必ずハンドリングするか `_` で明示的に無視する
- パニックは `init()` 内の登録失敗など、プログラミングエラーにのみ使用
- 公開 API にはドキュメントコメントを書く

### 命名規則

| 対象 | 規則 | 例 |
|-----|------|---|
| インターフェース | `-er` 接尾辞 | `Demuxer`, `Encoder` |
| Config 型 | `Config` / `XxxConfig` | `Config`, `EncoderConfig` |
| ファクトリ関数 | `NewXxx()` | `NewDemuxer()`, `NewInPort()` |
| ラッパー関数 | `WrapXxx()` | `WrapDemuxer()`, `WrapEncoder()` |
| エラー変数 | `ErrXxx` | `ErrEAGAIN`, `ErrNoPathFound` |
| 定数 (ProbeScore等) | `ProbeXxx`, `ActionXxx` | `ProbeExactSignature`, `ActionStop` |

### パッケージ設計

- `internal/` パッケージはプラグイン固有の実装詳細を格納する
- ドメインロジックは `domain/` に配置し、外部依存を持たない
- プラグインは `core` と `sdk` のみに依存すること (他プラグインに依存しない)

### ジェネリクスの使用指針

- ポートとエッジは型パラメータで型安全にする: `InPort[T]`, `OutPort[T]`, `Edge[T]`
- 現実的に型が絞れる場合はジェネリクスを使う
- `any` への逃げは最小限にする

### メモリ管理

- フレーム・パケットは `pool` から取得し、必ず `Release()` で返却する
- 所有権の移転を行う場合は `Retain()` を呼び出す
- `Release()` 後にデータにアクセスしてはならない

```go
// ✅ 正しい例
pkt := media.NewPacket(1024)
defer pkt.Release()  // 関数終了時に解放

// 別の goroutine に渡す場合
pkt.Retain()
go func() {
    defer pkt.Release()
    // ... use pkt
}()
```

### エラーハンドリング

```go
// ✅ エラーにコンテキストを付ける
if err := someFunc(); err != nil {
    return fmt.Errorf("someFunc: %w", err)
}

// ✅ sentinel エラーの判定
if errors.Is(err, engine.ErrEAGAIN) {
    // ...
}

// ✅ カスタムエラー型の判定
var mediaErr *media.Error
if errors.As(err, &mediaErr) {
    // ...
}
```

---

## モジュール管理

### 新しいプラグインモジュールを追加する

1. `plugins/my-plugin/` ディレクトリを作成
2. `plugins/my-plugin/go.mod` を作成:
   ```
   module github.com/godexture/my-plugin
   
   go 1.26.1
   
   require (
       github.com/godexture/core v0.x.x
       github.com/godexture/sdk  v0.x.x
   )
   ```
3. `go.work` に `use ./plugins/my-plugin` を追加

### 依存関係の更新

```powershell
# 特定モジュールの依存を更新
cd core
go get golang.org/x/sync@latest

# go.sum を更新
go mod tidy
```

---

## 既知の問題・TODO

| 項目 | 詳細 |
|-----|------|
| `filter-audio` が未実装 | mixer と resampler の実装が必要 |
| `cli` が未実装 | コマンドライン実装が必要 |
| `VideoAttributes` が未実装 | ビデオ処理サポートは将来予定 |
| `domain/time/rescale.go` が空 | タイムスタンプのリスケールが未実装 |
| `pkg/bits/reader.go` が空 | ビットリーダーが未実装 |
| `resolver` の `ResolverBundle` フィールドが未使用 | 自動パイプライン構築機能が未実装 |
| Muxer `AddStream` の `time.Rational` 引数が未使用 | タイムベース設定が未実装 |
| `registry.Error` が使われていない | 将来の統一エラー型として予約 |
| `manifest.AudioConstraint.Diagnose` は bool を返すが設計と矛盾 | インターフェースと実装が不一致 |
| WAV Muxer がメモリにパケットを蓄積する | 大容量ファイルでメモリ問題が発生する可能性 |
| `codec-pcm` の Decoder Factory で `cfg` から設定を読み込んでいない | `register.go:102` の TODO — 現在はデフォルト値のみ使用 |

---

## ディレクトリ構造テンプレート (新しいプラグイン)

```
plugins/format-myformat/
├── go.mod
├── register.go          # init() によるグローバル登録
└── internal/
    ├── demuxer.go       # DemuxerEngine 実装
    ├── muxer.go         # MuxerEngine 実装
    ├── probe.go         # Probe 関数
    └── myformat_test.go # 単体テスト
```

```
plugins/codec-mycodec/
├── go.mod
├── register.go          # init() によるグローバル登録
└── internal/
    ├── decoder.go       # DecoderEngine 実装
    ├── encoder.go       # EncoderEngine 実装
    ├── codec.go         # コーデック固有のロジック
    └── codec_test.go    # 単体テスト
```
