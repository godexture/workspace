# MP3デコーダ Goコード品質改善計画

## 概要

`plugins/codec-mp3/internal/mp3` パッケージは、`minimp3.h` を参考にしたCのコードを Go へ愚直に移植した結果として、いくつかの品質上の問題を抱えている。本ドキュメントでは、それらの問題点を分析し、**動作を保ちながら** Go 言語としての品質を高めるための改善方針を整理する。

> **前提**  
> 各改善作業は、`mp3-decoder-implementation.md` で定義されたスナップショットテストによってリグレッションがないことを確認しながら進める。

---

## 問題点一覧と改善方針

### 1. `unsafe` パッケージの排除

現在、以下の4箇所で `unsafe` が使われており、それぞれ異なる目的がある。

#### 1-1. `bs_t` が `*byte` でバッファを保持している（`huffman.go`, `dequant.go`）

**現状の問題:**

```go
// huffman.go
type bs_t struct {
    buf   *byte   // ← unsafe の根本原因
    pos   int32
    limit int32
}

// dequant.go
func getBits(bs *bs_t, n int) uint32 {
    // ...
    bufSlice := unsafe.Slice(bs.buf, int(pIdx)+n+2) // unsafe.Slice で *byte をスライスに変換
    // ...
}
```

`bs_t.buf` が `*byte`（Cポインタ相当）であるため、バイト列の読み取りに `unsafe.Slice` が必要になっている。

**改善方針:**

`bs_t` を廃止し、`[]byte` スライスでバッファを保持する `BitStream` 型に置き換える。

```go
// 改善後のイメージ
type BitStream struct {
    buf   []byte  // バッファはスライスで保持
    pos   int     // ビット位置
    limit int     // ビット上限
}
```

`buf` をスライスで持つことで、`getBits` 内の `unsafe.Slice` が不要になる。また `pos`/`limit` の型も `int` に統一できる（後述の型整理と連携）。

`header.go` には同等の `BitReader` 型がすでに `[]byte` ベースで存在しているが、`bs_t` と共存して混在状態になっている。この機会に `bs_t` を廃止し、`BitReader` へ統一するか、もしくは内部用に `bitStream` という名前の統一型を新設する。

#### 1-2. `minimp3.go` での 2次元配列の平坦化

**現状の問題:**

```go
// minimp3.go
grbufFlat := unsafe.Slice(&scratch.grbuf[0][0], 1152)
synFlat   := unsafe.Slice(&scratch.syn[0][0], 2112)
```

`[2][576]float32` や `[18+15][2*32]float32` という 2次元配列を、`unsafe.Slice` を使って 1次元スライスとして参照している。

**改善方針:**

`mp3dec_scratch_t` の構造体定義を変更し、2次元配列を 1次元スライス（または 1次元配列）として持つよう再定義する。

```go
// 改善後のイメージ
type mp3decScratch struct {
    bs       BitStream
    maindata [511 + 2304]byte
    grInfo   [4]L3GrInfo
    grbuf    [2 * 576]float32   // [2][576] → [1152]
    scf      [40]float32
    syn      [(18 + 15) * 2 * 32]float32  // [33][64] → [2112]
    istPos   [2][39]byte
}
```

これにより、`scratch.grbuf[ch*576:]` のような通常のスライス操作でアクセスでき、`unsafe.Slice` が不要になる。

#### 1-3. `L3HuffmanDecode` が `unsafe.Pointer` を引数に取っている（`huffman.go`, `dequant.go`）

**現状の問題:**

```go
// huffman.go
func L3HuffmanDecode(dst []float32, bsPtr unsafe.Pointer, grInfoPtr unsafe.Pointer, scf []float32, regionLimit int) {
    bs     := (*bs_t)(bsPtr)
    grInfo := (*L3GrInfo)(grInfoPtr)
    // ...
    bsBuf     := uintptr(unsafe.Pointer(bs.buf))
    bsNextPtr := bsBuf + uintptr(bs.pos/8)
    // ... unsafe.Pointer を使ったポインタ演算 ...
}
```

引数に `unsafe.Pointer` を取り、内部でポインタ演算を行っている。これはCスタイルのビット読み込みをそのまま移植した結果である。

**改善方針:**

1-1 の `bs_t` → `BitStream` 置き換えに伴い、`L3HuffmanDecode` の引数を型付きポインタに変更する。

```go
func L3HuffmanDecode(dst []float32, bs *BitStream, grInfo *L3GrInfo, scf []float32, regionLimit int) {
```

さらに、関数内部の手動ポインタ演算によるビットキャッシュ機構を、スライスインデックスを用いた安全なコードに書き直す。この部分は最も複雑な `unsafe` 使用箇所であり、1-1の対応と合わせて慎重に行う。

#### 1-4. `L3GrInfo.sfbtab` が `*byte` ポインタである（`huffman.go`, `dequant.go`）

**現状の問題:**

```go
type L3GrInfo struct {
    sfbtab *byte  // スケールファクタバンドテーブルへの生ポインタ
    // ...
}

func getSfbVal(sfbtab *byte, idx int) byte {
    return *(*byte)(unsafe.Pointer(uintptr(unsafe.Pointer(sfbtab)) + uintptr(idx)))
    // ↑ ポインタ演算によるバイト取得
}
```

`sfbtab` が配列の先頭バイトへの生ポインタで、オフセットアクセスに `unsafe` が必要になっている。

**改善方針:**

`sfbtab` を `[]byte` スライスに変更し、`getSfbVal` を廃止する。

```go
type L3GrInfo struct {
    sfbtab []byte  // スライスに変更
    // ...
}
```

アクセスは `grInfo.sfbtab[idx]` と書ける。`sfbtab` にセットしている箇所（`l3ReadSideInfo` 内）も、`gScfLong[srIdx][:]` 等をスライスとして渡すよう変更する。

---

### 2. 型・命名の Go 化

#### 2-1. C スタイルの型・命名を Go の慣習に統一する

**現状の問題:**

- `bs_t`：C の typedef スタイル（`_t` サフィックス）
- `mp3dec_scratch_t`：スネークケース、`_t` サフィックス
- `L3GrInfo`、`L12ScaleInfo`：一部はGoスタイルに近いが不統一
- `getBits`、`getSfbVal`：C 由来のキャメルケースだが `get` プレフィックスが Go では珍しい
- `L3HuffmanDecode`、`Mp3dSynthGranuleFloat`：大文字始まりで公開されているが、パッケージ外から呼ぶ必要がない

**改善方針:**

| 現在の名前 | 改善後の名前 | 理由 |
|---|---|---|
| `bs_t` | `bitStream`（非公開）| Go の命名規則に従いスネークケース/`_t` を廃止 |
| `mp3dec_scratch_t` | `decScratch`（非公開）| 同上 |
| `L3GrInfo` | `grInfo`（非公開）| パッケージ外から参照されないなら非公開 |
| `L12ScaleInfo` | `l12ScaleInfo`（非公開）| 同上 |
| `L3HuffmanDecode` | `l3HuffmanDecode`（非公開）| パッケージ内関数は非公開に |
| `Mp3dSynthGranuleFloat` | `synthGranule`（非公開）| 同上 |
| `getSfbVal` | 廃止（`sfbtab[idx]` に置換）| 関数自体不要になる |

> **注意:** `Mp3Dec` と `Mp3DecFrameInfo` は `decoder.go` から使用されるパッケージ公開型のため、その公開方針は変えない（もしくは、`internal` パッケージ全体が外部非公開であることを考慮して適切に整理する）。

#### 2-2. 重複している型定義の統合

`header.go` に `BitReader` があり、`huffman.go` に `bs_t` がある。両者は同じ「ビットストリームリーダー」を表す型として重複している。1-1 の改善と合わせて、どちらか一方に統一する。

#### 2-3. Go 1.21 以降の組み込み `min` / `max` を使用する

**現状の問題:**

```go
// huffman.go
func min(a, b int) int { ... }

// dequant.go
func max(a, b int) int { ... }
```

Go 1.21 で `min` / `max` が組み込み関数として追加された。カスタム定義は不要になった。

**改善方針:**

`go.mod` の Go バージョンが 1.21 以上であることを確認した上で、カスタム `min` / `max` を削除する。

---

### 3. オフセット引数による間接的な C ポインタ演算パターンの解消

#### 3-1. `*Offset` 引数パターン

**現状の問題:**

多くの関数が C の「ポインタ + オフセット」パターンを `(slice []T, offset int)` の2引数ペアとして移植している。

```go
// 例: dequant.go
func l3Antialias(grbuf []float32, grbufOffset int, nbands int) { ... }
func l3Reorder(grbuf []float32, scratch []float32, sfb *byte, sfbIdx int) { ... }
func l3StereoTopBand(right []float32, rightOffset int, sfb *byte, sfbIdx int, nbands int, maxBand []int) { ... }
func l12ApplyScf384(sci *L12ScaleInfo, scf []float32, scfOffset int, dst []float32, dstOffset int) { ... }
```

**改善方針:**

Goでは、スライスを部分取りすることで `slice[offset:]` を「先頭オフセット付きポインタ」として渡せる。`(slice, offset)` の2引数ペアを、`slice[offset:]` を直接渡す形に統一することでコードが読みやすくなる。

```go
// 改善後のイメージ
func l3Antialias(grbuf []float32, nbands int) { ... }
// 呼び出し側: l3Antialias(grbuf[ch*576:], aaBands+1)
```

ただし、スライスを渡した後に **元のスライスの別の範囲も参照する**ケースでは、オフセットを引数として保持した方が可読性が高い場合もある。関数ごとに慎重に判断する。

---

### 4. デッドコード・未使用コード

#### 4-1. `L3Dequantize` が空関数のまま残っている（`dequant.go`）

**現状の問題:**

```go
// dequant.go
// L3Dequantize is kept for compatibility.
func L3Dequantize(xr []float32, grInfo interface{}, scf []float32) {}
```

本体が空で、どこからも呼ばれていない。コメントに「for compatibility」とあるが、参照箇所は存在しない。

**改善方針:**

この関数を削除する。

#### 4-2. `header.go` の `Header` 型・`BitReader` 型がどこからも使われていない

**現状の問題:**

`header.go` には `Header` 型（`Mp3DecFrameInfo` と重複するフィールドセット）と `BitReader`/`BitReader.GetBits` が定義されているが、`decoder.go` も `mp3` パッケージ内の他のファイルも、これらを一切参照していない。

```go
// header.go — 使われていない型
type Header struct { ... }
type BitReader struct { ... }
func (br *BitReader) Init(buf []byte) { ... }
func (br *BitReader) GetBits(n int) uint32 { ... }
```

**改善方針:**

- `Header` 型を削除する（`Mp3DecFrameInfo` で代替）
- `BitReader` は 1-1・2-2 の `bs_t` 統合の文脈で採用するか否かを判断し、採用しない場合は削除する

#### 4-3. `snapshot_test.go` の `requireBitExact` 定数と `compareExact` 関数

**現状の問題:**

`snapshot_test.go` には、浮動小数点数演算の完全一致を検証するための `requireBitExact` 定数と `compareExact` 関数が残っている。しかし、CからGoへの移植はすでに完了しており、コンパイラや最適化による微細な浮動小数点数演算の誤差が生じるため、テストは常に `requireBitExact = false`（Epsilon許容比較）で実行する。そのため、これらは不要なデッドコードとなっている。

**改善方針:**

`snapshot_test.go` から `requireBitExact` 定数を削除し、常に `comparePCM` でテストを実行するようにする。また、不要となった `compareExact` 関数も削除する。

---

### 5. `huffman.go` における `uintptr` の GC 安全性問題

**現状の問題:**

```go
// huffman.go
bsBuf     := uintptr(unsafe.Pointer(bs.buf))  // ← uintptr に変換した時点で GC の追跡対象外
bsNextPtr := bsBuf + uintptr(bs.pos/8)
// ... bsNextPtr を使った読み出しを複数行にわたって実施 ...
```

Go の GC は `uintptr` 型に変換されたポインタを追跡しない。`uintptr` 変数に値を保存して複数行に渡って使用すると、GC が当該メモリを回収する可能性がある（`unsafe.Pointer` → `uintptr` 変換は**単一式内**でのみ安全）。

これは `unsafe` の排除（問題点 1-3）によって根本的に解消されるが、それ以前の問題として「現状のコードが GC に対して安全でない」ことを明記しておく。

**改善方針:**

1-3 の対応（`L3HuffmanDecode` の `unsafe.Pointer` 引数をスライスベースに書き換え）が最優先の修正。暫定的には `uintptr` を使わず `unsafe.Add` を用いる書き換えも検討できるが、根本解決ではない。

---

### 6. スナップショットの可読性

#### 6-1. スナップショットの更新フローがバイナリ直書きで読みにくい

**現状の問題:**

スナップショットは `float32` を Little Endian バイナリとして保存している。これはディスク効率は良いが、`.snapshot` ファイルの差分がバイナリ diff になるため、**誰かが誤ってスナップショットを更新した場合に git の diff で内容を確認できない**。

**改善方針:**

- スナップショットをテキスト形式（1行1サンプル、16進数または float 文字列）で保存すると diff が読みやすくなる

---

### 7. `decoder.go` の構造的な問題

#### 7-1. `decodeLoop` の責務過多

**現状の問題:**

`decodeLoop` が1つの Goroutine 内でバッファ管理・ID3スキップ・フレームデコード・PCM変換・フレーム生成・チャンネル送信をすべて行っており、関数が長く、テストしにくい。

**改善方針:**

- バッファリング（`buf` の管理）とフレームデコードを分離する
- `decodeFrame` のような純粋関数を抽出し、単体でテスト可能にする

#### 7-2. `ReceiveFrame` のチャンネル選択ロジックの問題

**現状の問題:**

```go
func (d *Decoder) ReceiveFrame() (*media.Frame, error) {
    select {
    case f, ok := <-d.outQueue:
        // ...
    default:
        if d.flushed && len(d.inQueue) == 0 {
            select {
            case <-d.outQueue: // 閉じているか確認 (上で select しているので不完全だが)
            default:
                // no-op
            }
        }
        return nil, engine.ErrEAGAIN
    }
}
```

コメント自身が「不完全」と認めているとおり、`flushed` 後に `decodeLoop` がまだ動作中であるかどうかを正確に判定できていない。`outQueue` が閉じられた後でも `ErrEAGAIN` を返し続けるリスクがある。

**改善方針:**

`decodeLoop` の終了を `sync.WaitGroup` または `done` チャンネルで明示的に追跡し、`ReceiveFrame` がそれを利用して EOF を確実に返せる構造にする。

#### 7-3. デコードループ中にサンプルレートとチャンネル数を毎フレーム上書きしている

**現状の問題:**

```go
// decoder.go
var sampleRate int
var channels int
// ...
for {
    samples, info := dec.DecodeFrame(...)
    if samples > 0 {
        sampleRate = info.Hz       // フレームごとに上書き
        channels = info.Channels   // フレームごとに上書き
        // ...
    }
}
```

MP3の仕様上、1ファイル内でサンプルレートやチャンネル数が変化することは基本的にない。初回フレームで確定したら以後は変わらない前提で扱うべきであるが、現状は変化した場合に検知せず黙って更新し続ける。また `sampleRate`/`channels` が上書きされても、すでに送信済みのフレームとの不整合が検知されない。

**改善方針:**

- 初回フレームでサンプルレート・チャンネル数を確定させる
- 2フレーム目以降で変化を検知した場合はエラーを返すか、警告ログを出す

---

### 8. `encoder.go` / `register.go` の問題

#### 8-1. Config marker method

**解決済み:**

```go
type Configuration interface{}
```

marker method は廃止された。Registry が named config 型の reflection から強制的に `PluginKey` を生成するため、Config 側で ID 用メソッドを実装する必要はない。

#### 8-2. `register.go` の出力チャンネルレイアウト

**解決済み:** `TransformFunc` は廃止され、Decoder Factory が唯一の output `StreamInfo` を返す。MP3 Factory は入力の channel count が 1 のとき `LayoutMono1`、それ以外では `LayoutStereo2_0` を返すため、交渉時の profile と実装が分離しない。

---

## 改善の優先順位と作業順序

各改善はすべて**スナップショットテスト（Epsilon 許容モード）が通ることを確認しながら**実施する。

| 優先度 | 項目 | 対象ファイル | 難易度 |
|---|---|---|---|
| 高 | 4-1: `L3Dequantize`（空関数）を削除 | `dequant.go` | 低 |
| 高 | 4-2: 未使用の `Header` 型を削除 | `header.go` | 低 |
| 高 | 4-3: `requireBitExact` と `compareExact` を削除 | `snapshot_test.go` | 低 |
| 高 | 1-4: `sfbtab` を `[]byte` に変更し `getSfbVal` を廃止 | `huffman.go`, `dequant.go` | 低 |
| 高 | 2-3: 組み込み `min`/`max` に置き換え | `huffman.go`, `dequant.go` | 低 |
| 高 | 1-2: 2次元配列を1次元配列に変更し `unsafe.Slice` を排除 | `minimp3.go`, `dequant.go` | 中 |
| 高 | 1-1: `bs_t` を `[]byte` ベースの型に置き換え（`getBits` の `unsafe.Slice` 排除） | `huffman.go`, `dequant.go` | 中 |
| 高 | 1-3 / 5: `L3HuffmanDecode` の `unsafe.Pointer`・`uintptr` をスライスに書き換え（GC安全性） | `huffman.go`, `dequant.go` | 高 |
| 中 | 2-2: `BitReader` と `bs_t` の統合（未使用なら `BitReader` を削除） | `header.go`, `huffman.go`, `dequant.go` | 中 |
| 中 | 2-1: 型・命名の Go 化 | 全体 | 中 |
| 中 | 7-3: サンプルレート・チャンネル変化の検知 | `decoder.go` | 低 |
| 完了 | 8-2: Factory のチャンネルレイアウト出力を入力 stream に合わせる | `register.go` | 低 |
| 低 | 3-1: `*Offset` 引数パターンの解消（優先度の高い箇所から順次）| `dequant.go` | 中 |
| 低 | 7-1: `decodeLoop` の責務分離 | `decoder.go` | 中 |
| 低 | 7-2: `ReceiveFrame` の EOF 検知修正 | `decoder.go` | 中 |
| 低 | 6-1: スナップショットのテキスト形式化（検討） | `snapshot_test.go` | 中 |

---

## 検証方針

- 各改善ステップの後、スナップショットテストを実行し **Epsilon 許容モードで失敗しないこと**を確認する（Go化が完了しているため、C との浮動小数点誤差が生じることがあり `requireBitExact = false` で固定する）
- `unsafe` の残留がないことを `grep` や `go vet` で確認する
- `go vet ./...` および `staticcheck` による静的解析を導入し、品質向上を継続的に検証できる環境を整える

---

## タスクリスト

### デッドコード削除
- [ ] `dequant.go` の空関数 `L3Dequantize` を削除する
- [ ] `header.go` の未使用型 `Header`・`BitReader`・`BitReader.Init`・`BitReader.GetBits` を削除する（または 2-2 で統合する）
- [ ] `snapshot_test.go` から `requireBitExact` 定数および `compareExact` 関数を削除し、常に `comparePCM` で検証するようにする

### 低コスト改善（名前・型）
- [ ] `go.mod` の Go バージョンを確認し、1.21 以上であれば `min`/`max` のカスタム定義を削除する
- [ ] 非公開にすべき型・関数のエクスポートを解除する（命名を Go 慣習に統一する）

### unsafe 排除
- [ ] `L3GrInfo.sfbtab` を `*byte` から `[]byte` に変更し `getSfbVal` を廃止する
- [ ] `mp3dec_scratch_t` の `grbuf`/`syn` を 1次元配列に変更し、`minimp3.go` と `dequant.go` の `unsafe.Slice` を排除する
- [ ] `bs_t.buf` を `*byte` から `[]byte` に変更し、`getBits` の `unsafe.Slice` を排除する
- [ ] `L3HuffmanDecode` の `unsafe.Pointer` 引数・内部 `uintptr` ポインタ演算をスライスベースに書き換える（GC安全性の問題を解消する）
- [ ] `bs_t`（または `BitReader`）を単一の型に統合する

### スライス API 整理
- [ ] `(slice, offset)` パターンを `slice[offset:]` 渡しに可能な範囲で変換する

### テスト改善
- [ ] スナップショットのテキスト形式化を検討する

### `decoder.go` 改善
- [ ] サンプルレート・チャンネル数の変化を検知してエラーを返す処理を追加する
- [x] `register.go` の Factory が返すチャンネルレイアウトを実際の出力に合わせる
- [ ] `decodeLoop` を責務ごとに分割する
- [ ] `ReceiveFrame` の EOF 検知を `done` チャンネル等で正確に実装する

### 継続的品質保証
- [ ] `go vet ./...` および `staticcheck` をCIに組み込む
