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

## Removed slower PCM left-justify SIMD

PCM encoder は有効 bit 数が container 幅より小さい入力を left-justify する。
4096 sample の現 SIMD 実装は SIMD API の load/shift/store 固定費が大きく、
単純な scalar byte loop より大幅に遅かった。同一バイナリの直接 kernel 比較を
S16/S32 それぞれ 100 ペア、各 50 ms、順序を反転して実行した。すべてのペアで
scalar が速い。

| format | scalar median | SIMD median | median improvement | mean improvement |
| --- | ---: | ---: | ---: | ---: |
| S16 | 3,550.5 ns/op | 45,142.5 ns/op | 92.13% | 91.85% |
| S32 | 1,768.5 ns/op | 45,417.5 ns/op | 96.11% | 96.04% |

build-tag dispatch、SIMD kernel、それ専用の比較テストを削除し、scalar 実装を
全 build 共通の `leftJustifyS16` / `leftJustifyS32` にした。有効な MS ADPCM
predictor SIMD は変更していない。

割当と copy を含む `BenchmarkLeftJustifyPCM` も変更前後バイナリで各 format
100 ペア、各 50 ms、順序を反転して実行した。すべてのペアで変更後が速い。

| format | baseline median | current median | median improvement | mean improvement |
| --- | ---: | ---: | ---: | ---: |
| S16 | 24,777.5 ns/op | 3,875 ns/op | 84.36% | 84.19% |
| S32 | 48,809 ns/op | 4,537.5 ns/op | 90.70% | 90.46% |

codec-pcm 全 package の通常/SIMD テストと integration test、race detector の
internal テストを通過した。

## Ownership-aware media pipeline

`BenchmarkAudioPipeline/CodecRoundtrip/64MiB` の初期 profile は 619 MB/op、
約38万 allocs/op で、CPU 時間の多くを GC と scheduler に費やしていた。
`media.Packet` / `media.Frame` の ownership 契約に反し、engine adapter と
testutil node が消費済み resource を `Release` していなかったため、backing
buffer pool が通常完了時にも再利用されていなかった。

engine 境界では入力を処理し終えた時点で解放し、downstream への `Push` 失敗時は
未移譲の出力を解放する。非同期 loop は pull goroutine の終了と未処理入力の解放を
一つの cleanup に集約した。testutil の chunk/compare/discard は消費した frame を
解放し、tee は二つの downstream ownership ごとに `Retain` する。

旧版と変更後 binary を交互に実行した。1 MiB は10標本内で10回ずつ、計100回、
64 MiB は30標本内で1回ずつ実行した。

| workload | size | baseline median | current median | median improvement | mean improvement |
| --- | ---: | ---: | ---: | ---: | ---: |
| Decode | 1 MiB | 1,405,835 ns/op | 998,755 ns/op | 28.96% | 26.32% |
| Codec roundtrip | 1 MiB | 4,349,425 ns/op | 3,814,780 ns/op | 12.29% | 14.60% |
| Decode | 64 MiB | 74,036,550 ns/op | 54,241,950 ns/op | 26.74% | 25.94% |
| Codec roundtrip | 64 MiB | 255,894,000 ns/op | 190,163,650 ns/op | 25.69% | 25.29% |

64 MiB の allocation は Decode が 142,081,880 から 7,809,464 B/op へ
94.50%減、Codec roundtrip が 618,885,224 から 285,828,924 B/op へ
53.82%減った。ownership を数える専用テスト、通常テスト、race detector を
通過した。

## Reused PCM comparison buffers

ownership 修正後の allocation profile では、比較用 `float32` 変換が
263 MB/op を占めた。比較 stream ごとに直前の slice を保持し、次 frame の
変換先として再利用する。公開 helper も destination を受け取る単一 API に
集約し、互換 wrapper は残していない。

直前版と変更後 binary を交互に、1 MiB は計100回、64 MiB は30回実行した。
全10/10および30/30標本で変更後が速い。

| size | baseline median | current median | median improvement | mean improvement | bytes/op reduction |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 1 MiB | 3,012,705 ns/op | 2,254,065 ns/op | 25.18% | 26.21% | 93.43% |
| 64 MiB | 191,714,500 ns/op | 144,253,100 ns/op | 24.76% | 25.20% | 94.97% |

## Pooled MP3 frame packets

`l3-sin1k0db.mp3` の demux profile では、frame scanner が frame ごとに
`make` した slice を `NewPacketFromData` で包み、214,656 B/op、
1,279 allocs/op を発生させていた。scanner の検証処理は共通のまま、
通常の packetization では `media.NewPacket` の pooled backing buffer へ
直接 `ReadFull` する。読取失敗時の `Release` も allocator と同じ helper に
集約した。

実ファイル全 packet の demux を1標本10回、20標本、計200回実行し、旧版と
変更後の順序を交互にした。全20標本で変更後が速い。

| metric | baseline | pooled frame buffer | improvement |
| --- | ---: | ---: | ---: |
| median | 104,875 ns/op | 60,650 ns/op | 42.17% |
| mean | 107,351 ns/op | 61,474 ns/op | 42.74% |
| bytes/op | 214,656 | 65,317 | 69.57% |
| allocations/op | 1,279 | 645 | 49.57% |

## Pooled media Packet objects

backing buffer pool 導入後の MP3 demux allocation profile では、残る object
allocation の98.07%が `media.newPacket` だった。`Packet` 本体と解放 callback
を `sync.Pool` で再利用し、取得時に全 scalar、codec parameters、timebase を
初期化する。backing buffer の pool は従来どおりで、`NewPacketFromData` の
ownership-transfer 契約も変えない。

直前版と Packet object pool 版を上と同じ計200回で交互実行した。全20標本で
変更後が速い。

| metric | backing buffer pool | object pool | improvement |
| --- | ---: | ---: | ---: |
| median | 61,140 ns/op | 36,115 ns/op | 40.93% |
| mean | 62,482 ns/op | 36,814 ns/op | 41.08% |
| bytes/op | 65,317 | 9,776 | 85.03% |
| allocations/op | 645 | 11 | 98.29% |

専用の state reset、Retain/Release、16 goroutine 並行利用テストを追加した。
core、engine、testutil、全 format と codec internal の通常/SIMDテスト、
core の race detector を通過した。

## Aligned MP3 frame fast path

Packet object pool 後は frame scanner が MP3 demux CPU の累積55.07%を占めた。
一度同期した連続 stream でも、各 frame で次 frame の header まで `Peek` して
再検証していた。最初の scanner 成功後は現在位置の4-byte header と frame 全長を
検査して直接読み、header が不正なら従来 scanner へ戻す。Analyze と Seek 後は
必ず同期を再確立する。末尾の切詰め frame は消費前の `Peek` で検出する。

直前版と変更後 binary を1標本10回、20標本、計200回、順序を交互に実行した。
全20標本で変更後が速く、allocation は同じだった。

| metric | verified every frame | aligned fast path | improvement |
| --- | ---: | ---: | ---: |
| median | 36,290 ns/op | 27,770 ns/op | 23.48% |
| mean | 36,022 ns/op | 28,116 ns/op | 21.95% |
| bytes/op | 9,777 | 9,776 | 0.01% |
| allocations/op | 11 | 11 | 0% |

破損 byte 後の再同期と切詰め末尾を専用テストで固定し、通常/SIMD の format
全テスト、race detector、実 MP3 の codec snapshot を通過した。変更後 profile
は 5.27 GB/s で、残りは必須 `memmove` 12.70%、Packet pool/atomic、
複数の5〜7% header 演算に分散したため、この plugin の最適化を終了した。

## Pipeline observation fast paths

パイプライン観測は `Off`、`Progress`、`Metrics` の実行経路を分離した。`Off` は
通常の `ChanEdge` を直接接続し、edge の atomic 更新、node collector、runtime
sampler、表示 goroutine を生成しない。`Progress` は選択入力 edge の item 数と
最大メディア時刻だけを更新し、`Metrics` は全 node/edge を収集する。

同一テストバイナリの synthetic demux-decode-discard pipeline を使い、64 MiB を
各標本1回、30標本、AB/BA の順序を標本ごとに反転して測定した。各 timed run
の前に GC を実行して packet/frame pool と heap の開始条件を揃えた。この節の
数値はこの実行内の paired 比較だけに使い、過去の測定値とは比較していない。

| comparison | median overhead | criterion |
| --- | ---: | ---: |
| `ObservationOff` / direct `ChanEdge` | -4.985% | slowdown 1%以内 |
| `ObservationProgress` / `ObservationOff` | +1.098% | slowdown 3%以内 |
| `ObservationMetrics` / `ObservationOff` | +3.928% | 固定閾値なし |

packet/frame の edge 単体 microbenchmark は同一 object を再利用し、全モードで
`0 B/op, 0 allocs/op` だった。Progress/Off の差は atomic 更新の CPU コストだけで、
item 数に比例する追加 allocation はない。

| item | plain | progress | metrics |
| --- | ---: | ---: | ---: |
| packet | 103.0 ns/op | 148.3 ns/op | 206.8 ns/op |
| audio frame | 163.2 ns/op | 160.2 ns/op | 256.8 ns/op |

各モードの64 MiB CPU/heap profile は別々の専用 benchmark で採取した。Off-only
profile を観測 edge、`runtime.ReadMemStats`、formatter、progress reporter、ticker、
node metrics のシンボルへ絞った結果、CPU・heap とも sample はゼロだった。
Progress/Metrics の上位 hotspot は synthetic input の packet 生成・copy と runtime
scheduler で、観測処理による想定外の保持 object は見つからなかった。

測定環境はこの文書冒頭の Windows/amd64、Go 1.26.4、Core i7-13620H。実行時間は
電源供給・電源モードの影響を受けるため絶対値の判定には使わず、上記の同一実行内
paired 中央値だけで基準を評価した。

## Investigated and stopped candidates

10%以上の改善が難しい候補は実装を残さず、次の領域へ移った。

- FLAC LPC inner coefficient unroll は kernel 約5%に対して frame encode の
  中央値1.1%、平均0.77%に留まった。
- FLAC Rice fold/stat fusion は SIMD partition 版が約8.9%、scalar fusion が
  約5.9%遅く、双方を破棄した。
- MP3 decoder の unsafe bounds-check 除去は中央値3.51%、平均4.20%だった。
- common audio の完全一致 fast path は64 MiBで中央値 -1.23%、平均0.05%、
  native S16 view は中央値1.09%、平均3.12%だった。
- AudioFrame 本体と metadata map の pool 化は allocation を最大93.56%削減したが、
  64 MiB Decode は中央値6.33%、Codec roundtrip は0.56%で、実装を破棄した。
- format-flac demux は 4 MiB で 19.4 GB/s、CPU は `memmove` 44.3% と
  assembly `bytes.IndexByte` 31.3%だった。format-wav raw demux は
  14.1 GB/s、`memmove` 91.9%であり、双方とも必須 copy/既存 assembly を
  置き換えて10%を得る現実的な候補がなかった。

最終確認では `test-runner.exe --simd` を1回実行し、`codec-flac/test`
（88.6秒）を含む workspace 全 package が成功した。
