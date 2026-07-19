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
