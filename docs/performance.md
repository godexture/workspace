# Performance log

性能変更は、原則として同じバイナリ内の paired benchmark、または変更前後の
テストバイナリを交互に実行して評価する。軽量カーネルは 100 標本以上、重い
ワークロードは各標本内で十分な回数を反復し、中央値と平均値の両方を記録する。

## 測定環境

- Windows/amd64、Go 1.26.4
- Intel Core i7-13620H、AC 接続
- Windows 電源モード: 最適なパフォーマンス
- SIMD 対象: `GOEXPERIMENT=simd`

## 32-bit 以下の bit reader fast path

CPU profile では、FLAC フレーム復号の 61.17% が residual 復号に含まれ、
`bits.Reader` の `Bits64`、`Unary64`、`Bits32` が主要なホットスポットだった。
FLAC のフィールドの大半は 32 bit 以下なので、`Bits64` と signed read を既存の
`Bits32` 経路へ分岐させ、明示的に 32-bit 幅と分かる箇所では `ReadBits32` を使う。

`BenchmarkBits64Paired` は 9-bit 読取を 8 KiB ごとに新旧交互実行した。

| metric | current | generic reference | improvement |
| --- | ---: | ---: | ---: |
| median, 100 samples | 23,500 ns | 40,540.5 ns | 42.67% |
| mean, 100 samples | 23,519.2 ns | 40,418 ns | 39.61% |

`BenchmarkDecodeFrameDefaultConfig` は基準コミットと変更後のテストバイナリを
50 ペア、ペアごとに順序を反転して各 200 ms 実行した。全 50 ペアで変更後が速い。

| metric | baseline | current | improvement |
| --- | ---: | ---: | ---: |
| median | 147,384 ns/op | 128,273 ns/op | 13.05% |
| mean | 148,354.9 ns/op | 129,158.8 ns/op | 12.91% |

通常ビルドと SIMD ビルドの双方で、`pkg/...`、FLAC decoder/encoder、
FLAC frame の対象テストを通過した。

MP3 の `BenchmarkDecodeFile` も 30 ペアで測定したが、中央値 -0.09%、平均
+0.66%、改善 14/30 ペアで効果を確認できなかった。MP3 固有の
`ReadBits32` 置換は採用せず、MP3 は別途 profile 上の大きなホットスポットを狙う。

## Unaligned unary prefix scan

32-bit fast path 導入後も FLAC residual 復号は CPU profile の 57.02% を占め、
`Unary64` は累積 18.67%、その bit-at-a-time prefix は 7.20% を占めていた。
Rice 符号は通常短く、開始位置も非 byte-aligned なので、先頭 byte の残りを
1 bit ずつ読む代わりに、mask と `LeadingZeros8` で一括検査する。

`BenchmarkDecodeFrameDefaultConfig` の変更前後バイナリを 50 ペア、各 200 ms、
ペアごとに順序を反転して実行した。49/50 ペアで変更後が速い。

| metric | baseline | current | improvement |
| --- | ---: | ---: | ---: |
| median | 126,724.5 ns/op | 112,105 ns/op | 11.25% |
| mean | 127,931.2 ns/op | 113,208.6 ns/op | 11.37% |

## Branchless Rice sign decode

Unary prefix 改善後の profile では `decodeRiceSigned` が累積 34.29%、flat 7.36%
を占めていた。範囲検証は維持したまま、偶数/奇数の分岐による Rice 符号の
正負展開を ZigZag の XOR 式へ置き換えた。

`BenchmarkDecodeFrameDefaultConfig` の変更前後バイナリを 50 ペア、各 200 ms、
ペアごとに順序を反転して実行した。49/50 ペアで変更後が速い。

| metric | baseline | current | improvement |
| --- | ---: | ---: | ---: |
| median | 115,301.5 ns/op | 104,862 ns/op | 8.64% |
| mean | 117,667.4 ns/op | 106,427.8 ns/op | 9.19% |

## Fused Rice quotient and remainder read

Branchless sign decode 後も residual 復号は profile の 52.27% を占め、`Rice64`
は累積 26.50%だった。従来は `Unary64` で quotient を読み、その直後に
`Bits32` で同じ位置から remainder を読み直していた。典型的な短い符号が
8-byte window 内に収まる場合は、1回のloadと `LeadingZeros64` で両方を読む。
長い符号、buffer末尾、切詰め入力は従来の分割経路へフォールバックする。

`BenchmarkRice64Paired` は4096個のRice値ごとに統合経路と分割経路を交互実行した。

| metric | fused | split reference | improvement |
| --- | ---: | ---: | ---: |
| median, 100 samples | 21,030.5 ns | 41,229.5 ns | 49.19% |
| mean, 100 samples | 21,292.9 ns | 41,875.6 ns | 47.72% |

`BenchmarkDecodeFrameDefaultConfig` の変更前後バイナリを 50 ペア、各 200 ms、
ペアごとに順序を反転して実行した。全 50 ペアで変更後が速い。

| metric | baseline | current | improvement |
| --- | ---: | ---: | ---: |
| median | 103,167.5 ns/op | 85,320.5 ns/op | 17.15% |
| mean | 104,517 ns/op | 85,981.1 ns/op | 17.62% |

## Inline Rice range validation

`decodeRiceSigned` と `Rice64` はともにインライン不可で、サンプルごとに二段の
呼び出しになっていた。符号なしRice値の有効範囲が `0..0xfffffffe` と等価で
あることを利用し、range check とZigZag展開を残差ループへ集約して中間関数と
sentinelを削除した。

`BenchmarkDecodeFrameDefaultConfig` の変更前後バイナリを 50 ペア、各 200 ms、
順序を反転して実行した。実装コードは減少し、36/50 ペアで変更後が速い。

| metric | baseline | current | improvement |
| --- | ---: | ---: | ---: |
| median | 84,772.5 ns/op | 84,035 ns/op | 1.04% |
| mean | 85,386.5 ns/op | 84,536.7 ns/op | 0.96% |

## FLAC integration checkpoint

bit/Rice decode 一連の変更を、47,782 byte の `60 - mono audio.flac` を使う
`BenchmarkDecodeConformance/SmallMono` で基準コミットと比較した。デマルチプレクサ、
decoder wrapper、audio frame生成を含む完全な復号パイプラインを、30 ペア、
各 200 ms、ペアごとに順序を反転して実行した。29/30 ペアで現行が速い。

| metric | baseline | current | improvement |
| --- | ---: | ---: | ---: |
| median | 1,299,904 ns/op | 1,100,537 ns/op | 15.72% |
| mean | 1,326,232 ns/op | 1,144,895.6 ns/op | 13.73% |

## Reused LPC analysis workspace

FLAC frame encode の allocation profile では `lpcCoefficientSets` が割当量の
63.23%、割当回数の 50.23% を占めていた。LPC 解析用のベクトルと次数別係数を
channel candidate ごとに作り直さず、各 encoder worker が所有する `windowSet`
内で再利用する。worker 間では共有しないため同期は不要である。

`BenchmarkEncodeFrameDefaultConfig` の変更前後バイナリを 50 ペア、各 200 ms、
ペアごとに順序を反転して実行した。47/50 ペアで変更後が速い。

| metric | baseline | current | improvement |
| --- | ---: | ---: | ---: |
| median | 263,487 ns/op | 250,176.5 ns/op | 5.14% |
| mean | 266,417.9 ns/op | 252,458.5 ns/op | 5.12% |
| median bytes/op | 210,392.5 | 107,251 | 49.02% |
| allocations/op | 102 | 57 | 44.12% |

worker と作業領域を複数フレームで再利用する `BenchmarkEncoderDefaultConfig` も
30 ペア、各 200 ms、順序を反転して実行した。25/30 ペアで変更後が速い。

| metric | baseline | current | improvement |
| --- | ---: | ---: | ---: |
| median | 919,378 ns/op | 865,378.5 ns/op | 4.97% |
| mean | 928,980.7 ns/op | 882,718.5 ns/op | 4.91% |
| median bytes/op | 1,748,614.5 | 1,320,161.5 | 24.50% |
| allocations/op | 534 | 364 | 31.84% |

変更後の allocation profile では `lpcCoefficientSets` 固有の割当は消え、
フレーム単体の総量は約 107.6 KiB/op、57 allocs/op になった。通常、SIMD、
race detector の encoder 対象テストを通過した。

## Four-way scalar LPC residual

作業領域再利用後の frame encode CPU profile では、既定設定が使う次数 8 以下の
`lpcResidualScalar` が flat 12.69% で最大の演算カーネルだった。4 出力の積和を
同時に進め、係数の読取とループ制御を共有する。各出力内の加算順序は変えない。

旧 serial kernel と新 kernel を同じバイナリ内で交互に実行し、各次数を 100 標本、
標本内 100 ms 以上反復した。

| order | current median | serial median | median improvement | mean improvement | wins |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 4 | 5,414.5 ns/op | 9,022.5 ns/op | 39.99% | 42.78% | 92/100 |
| 8 | 8,570.5 ns/op | 13,688 ns/op | 37.39% | 34.50% | 89/100 |
| 12 | 13,138 ns/op | 18,522 ns/op | 29.07% | 29.22% | 86/100 |
| 32 | 29,829 ns/op | 41,165 ns/op | 27.54% | 25.37% | 78/100 |

`BenchmarkEncodeFrameDefaultConfig` は直前コミットと 50 ペア、各 200 ms、
順序を反転して実行した。48/50 ペアで変更後が速い。

| metric | baseline | current | improvement |
| --- | ---: | ---: | ---: |
| median | 249,293 ns/op | 232,050 ns/op | 6.92% |
| mean | 250,295.7 ns/op | 233,184 ns/op | 6.84% |

複数フレームを処理する `BenchmarkEncoderDefaultConfig` も 30 ペアで中央値
6.12%、平均 4.22% 改善した。変更後 profile の `lpcResidualScalar` は flat
8.66% まで低下した。端数長を含む全次数 1..32 と複数 shift を旧 kernel と
照合し、通常、SIMD、race detector の encoder 対象テストを通過した。

## Pre-sized frame bit writer

allocation profile では `bits.Writer.Bits64` 内の段階的な `append` growth が
フレーム割当量の 31.81% を占めていた。解析済みフレームの `costBits` から
出力容量を事前確保し、最大ヘッダ差分用に 16 byte の余裕を加える。共通
`bits.Writer.Grow` は `slices.Grow` と同じ「追加 n byte」の契約にした。

`BenchmarkEncodeFrameDefaultConfig` を直前コミットと 50 ペア、各 200 ms、
ペアごとに順序を反転し、`benchmem` 付きで実行した。49/50 ペアで変更後が速い。

| metric | baseline | current | improvement |
| --- | ---: | ---: | ---: |
| median | 231,175.5 ns/op | 223,480.5 ns/op | 3.33% |
| mean | 234,302.9 ns/op | 223,713 ns/op | 4.52% |
| median bytes/op | 107,059.5 | 80,671.5 | 24.65% |
| allocations/op | 57 | 43 | 24.56% |

`BenchmarkEncoderDefaultConfig` は 30 ペア中 29 ペアで変更後が速く、連続する
4 frame それぞれの grow を削減するため、単体より効果が大きい。

| metric | baseline | current | improvement |
| --- | ---: | ---: | ---: |
| median | 845,056 ns/op | 753,952 ns/op | 10.78% |
| mean | 860,906.4 ns/op | 767,385.2 ns/op | 10.86% |
| median bytes/op | 1,322,030.5 | 1,141,993 | 13.62% |
| allocations/op | 364 | 295 | 18.96% |

変更後 allocation profile では writer 出力は単一の `Grow` になり、割当量の
11.66% まで低下した。共通 bits、通常/SIMD FLAC encoder、race detector の
対象テストを通過した。

## Small-field bit writer path

容量確保後も `bits.Writer.Bits64` は frame encode CPU profile の flat 11.33%
を占めた。Rice residual とフレームヘッダで頻出する 1..16 bit は、汎用の
非整列 merge、whole-byte loop、末尾処理を通さず、最大 3 byte の packed value
を直接配置する。`UnaryBits64` も小さい Rice 符号を同じ helper へ直結する。

1..16 bit を混在させ、4096 field ごとに writer を再利用する
`BenchmarkWriterBits64Small` を変更前後バイナリで 100 ペア、各 50 ms、
ペアごとに順序を反転して実行した。92/100 ペアで変更後が速い。

| metric | generic | small-field path | improvement |
| --- | ---: | ---: | ---: |
| median | 34,274 ns/op | 23,674.5 ns/op | 30.93% |
| mean | 39,393.7 ns/op | 27,520.4 ns/op | 30.14% |

`BenchmarkEncodeFrameDefaultConfig` は直前コミットと 50 ペア、各 200 ms、
順序を反転して実行した。42/50 ペアで変更後が速い。

| metric | baseline | current | improvement |
| --- | ---: | ---: | ---: |
| median | 219,618.5 ns/op | 215,176.5 ns/op | 2.02% |
| mean | 227,014.8 ns/op | 217,605 ns/op | 4.15% |

`BenchmarkEncoderDefaultConfig` は 30 ペア中 25 ペアで変更後が速く、中央値
4.19%、平均 4.07% 改善した。全 offset/width を bit-by-bit oracle と照合する
既存テストに加え、共通 bits、通常/SIMD FLAC encoder、race detector の対象
テストを通過した。

## Removed slower mid/side SIMD

frame encode profile では `computeMidSide4` が flat 7.68% を占めていた。
現 SIMD 実装は slice load/store と shift 補正のコストが大きく、4096 sample
では単純な scalar loop より遅かった。同一バイナリの両 kernel を 100 ペア、
各 50 ms、順序を反転して比較すると、全 100 ペアで scalar が速い。

| metric | scalar | SIMD | scalar improvement |
| --- | ---: | ---: | ---: |
| median | 4,563.5 ns/op | 25,443 ns/op | 82.06% |
| mean | 4,618.1 ns/op | 25,501.4 ns/op | 81.89% |

閾値を変えるだけでなく、build-tag dispatch、SIMD kernel、それ専用のテストと
比較ベンチを削除し、全 build で共通の `computeMidSide` へ集約した。

`BenchmarkEncodeFrameDefaultConfig` は直前コミットと 50 ペア、各 200 ms、
順序を反転して実行した。47/50 ペアで変更後が速い。

| metric | baseline | current | improvement |
| --- | ---: | ---: | ---: |
| median | 214,565.5 ns/op | 192,545 ns/op | 10.26% |
| mean | 226,039.3 ns/op | 199,967.5 ns/op | 11.53% |

`BenchmarkEncoderDefaultConfig` は 30 ペア中 21 ペアで変更後が速く、中央値
0.89%、平均 2.69% 改善した。変更後 profile では `computeMidSide4` は上位から
消えた。通常、SIMD、race detector の encoder 対象テストを通過した。

## Removed slower MP3 synthesis-window SIMD

MP3 file decode profile では `synthWindowSIMD` が flat 83.38%、synthesis 全体が
95.64% を占めていた。処理単位が 4 lane と短いため、8 回の SIMD load、
broadcast、演算、store の固定費が scalar の unrolled loop を大幅に上回る。

同一バイナリの `BenchmarkSynthWindowCompare` を 100 ペア、各 50 ms、
ペアごとに順序を反転して実行した。全 100 ペアで scalar が速い。

| metric | scalar | SIMD | scalar improvement |
| --- | ---: | ---: | ---: |
| median | 32.665 ns/op | 385.05 ns/op | 91.52% |
| mean | 32.885 ns/op | 388.347 ns/op | 91.53% |

build-tag dispatch、SIMD kernel、それ専用の比較テストを削除し、既存の unrolled
scalar 実装を全 build 共通の `synthWindow` にした。

`l3-sin1k0db.mp3` 全体を復号する `BenchmarkDecodeFile` を変更前後バイナリで
20 ペア、各 300 ms、順序を反転して実行した。全 20 ペアで変更後が速い。

| metric | baseline | current | improvement |
| --- | ---: | ---: | ---: |
| median | 75,357,162.5 ns/op | 7,051,986.5 ns/op | 90.64% |
| mean | 75,292,963 ns/op | 7,207,012.8 ns/op | 90.43% |

変更後 profile では `synthWindow` は flat 29.69%、MP3 synthesis は累積 56.58%
まで低下した。通常/SIMD の internal 全 package、race detector、同じ入力の
snapshot integration テストを通過した。
