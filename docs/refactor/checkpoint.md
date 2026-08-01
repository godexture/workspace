# M0/M1 checkpoint

この文書は M0/M1 を完了へ移すための実装 backlog と検証結果を整理する。進捗状態の正本は [refactor.md](../refactor.md)、品質契約は [quality.md](quality.md)、repository/package 方針は [architecture.md](architecture.md) とする。

## 現在の判定

- M0: 完了。再監査で判明した5項目（differential harness の false positive、filter chain benchmark の計測範囲、metadata baseline の単一 key 限定、lifecycle failure injection の未検査 phase、baseline artifact の再現不能性）すべてに対応する test/tool/document を追加・修正し、`docs/refactor.md` を更新済み。詳細は [baseline.md](baseline.md) を正本とする。
- M1: 完了。再監査で判明した3項目（generator bootstrap の cross-platform 対応、module relation の完了条件、M1 後の path/command 同期）すべてに対応した。
- `example/assets`、`example/web/assets`、`plugin/flac/test/testdata/conformance` の3 gitlinkは、code と独立した任意取得の共有データとして意図的に維持し、M0/M1 の blocker にしない。
- repository 全体を対象にした `tools/cmd/differential` の横断実行（scalar/SIMD/forced-scalar の3 variant × 全 package）は、実行時間が長すぎて baseline/CI の実用的な gate にならないため、必須証跡から外した。tool 自体と、その正しさを検証する `main_test.go` の unit/integration test は維持する。日常の scalar/SIMD 検証は `go run ./tools/cmd/test-runner`（`--simd` で SIMD variant）を使う。

## 再監査で解消した項目

- differential harness を fail-safe にし、semantic result を比較するようにした（`tools/cmd/differential`、`sdk/dsp/cpu_simd.go` の forced-scalar 機構）。
- filter chain benchmark の construction と steady-state を `b.StopTimer`/`b.StartTimer` で分離し、output の正しさを検査する test を追加した（`plugin/audio/chain_pipeline_test.go`）。
- stream/metadata baseline を単一 known key から拡張し、multi-value の順序保持、duplicate の上書き、unknown tag の loss、raw chunk の保持を固定した（`sdk/conversion/passthrough_test.go`）。
- lifecycle failure injection の未検査 phase（decoder/encoder の Flush、muxer の SetMetadata/AddStream）を埋めた（`sdk/engine/wrapper_test.go`、`core/routing/negotiator_lifecycle_test.go`）。
- baseline artifact を go.work tracked 済みの commit へ再固定し、machine-readable な [baseline.manifest.json](baseline.manifest.json) を追加した。
- generator bootstrap を `runtime.GOOS` 分岐で cross-platform 化した（`tools/cmd/generate/build.go`）。
- module relation の完了条件を設計期 repository 内 composition に限定し、architecture.md に明記した。
- `.gitmodules`、fixtures.md、quality.md、performance.md、一部 test comment に残っていた M1 以前の path/command 表記を同期した。

各項目の詳細な作業内容と検証記録は Git 履歴（コミットメッセージ）を正本とし、本文書では繰り返さない。
