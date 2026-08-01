# M0/M1 checkpoint

この文書は M0/M1 を完了へ移すための実装 backlog と検証結果を整理する。進捗状態の正本は [refactor.md](../refactor.md)、品質契約は [quality.md](quality.md)、repository/package 方針は [architecture.md](architecture.md) とする。

## 現在の判定

- M0: 完了。一次再監査（differential harness、filter chain benchmark、metadata baseline、lifecycle failure injection、baseline artifact）と、それに続く二次再監査（任意 differential tool の false positive、撤回した保証の文書反映漏れ、filter chain benchmark の lifecycle 境界、baseline manifest の再現性・構造、convolver end-to-end test の resource/observation 不備）のいずれも解消済み。詳細は [baseline.md](baseline.md) を正本とする。
- M1: 完了。monorepo 化、最終 `plugin/<family>` path、tracked workspace、設計期の module relation、path/command 同期、generator bootstrap の cross-platform 対応を完了している。
- `example/assets`、`example/web/assets`、`plugin/flac/test/testdata/conformance` の3 gitlinkは、code と独立した任意取得の共有データとして意図的に維持し、M0/M1 の blocker にしない。
- repository 全体の scalar/SIMD/forced-scalar semantic differential は、検証コストを理由に M0 の保証と必須 gate から外している。`tools/cmd/differential` は任意の診断 tool として残り、`main_test.go` の unit/integration test がその正しさ（子 process の failure を必ず検出すること等）を検証する。scalar/SIMD/worker の semantic diff 自体は、対象 package ごとの differential test（`sdk/dsp`、FLAC parallelism、convolver worker 1/4/16 等）で満たす。

## 二次再監査で解消した項目

- **differential tool の false positive**: `runSuite` が `*exec.ExitError` を「packages に反映された失敗」と無条件に扱っていたため、`go test` が1件も test event を出す前に失敗する場合（例: 不正な `GOFLAGS`）、`0 package(s)` の clean success として報告されていた。`anyPackageFailed` チェックを追加し、レビュー記載の repro（`GOFLAGS=-definitely-invalid`）を regression test 化して検証した。tool の doc comment も、package status の集約のみを行い semantic output は比較しない、という実際の責務に合わせて書き直した。
- **撤回した differential 保証の文書反映**: quality.md の M0 完了条件、baseline.md の完了条件対応が repository 全体の differential 実行に依存しているように読めていた箇所を、対象 package ごとの differential test で満たす旨に修正した。baseline.manifest.json の forced-scalar variant/command には `requiredForBaseline: false` を明示した。
- **filter chain benchmark の lifecycle 境界**: `Pipeline.Run` は内部で必ず `Prepare` と全 node の teardown（Close）を行うため、construction だけを timer 外へ出しても測定値は「Prepare + steady-state + teardown」のままだった。`Prepare` を timer 外で明示的に呼び、`Run` 内部の再呼び出しを no-op にした（実際に allocs/op が減ることを確認）。teardown は `Pipeline` に分離手段がないため測定範囲に残るが、doc comment で明示した。`BenchmarkGainChainPipelineOpen` も、`Prepare` を呼ばず input 生成を timer 内に含めていた不備を修正した。
- **baseline manifest の再現性・構造**: benchmark 入力 `stereoBlock` が `math/rand/v2` の runtime-seeded package-level generator を使っており実行ごとに変わっていたため、固定 seed のローカル generator に変更した（決定性を確認済み）。manifest の command を shell string から `argv`/`env` の構造化形式に変更し、CPU feature（avx2/avx2fma）と各 benchmark の input generator 仕様（algorithm、seed、size tier）を追加した。
- **convolver end-to-end test の resource/observation**: 入力 frame の Release 漏れ、Engine の Close 漏れを修正し、`ReceiveFrame` のエラーを `engine.ErrEOF` かどうかで判定するようにした（他のエラーは即 test failure）。出力も frame 数・PTS・sample data を保持する形へ変更した。ErrEOF 判定の効果は、`Engine.Flush` が queue を flush しないよう一時的に壊して検証した。

## 一次再監査で解消した項目

- differential harness を fail-safe にし、semantic result を比較するようにした（`tools/cmd/differential`、`sdk/dsp/cpu_simd.go` の forced-scalar 機構）。
- filter chain benchmark の construction を steady-state から分離し、output の正しさを検査する test を追加した（`plugin/audio/chain_pipeline_test.go`）。
- stream/metadata baseline を単一 known key から拡張し、multi-value の順序保持、duplicate の上書き、unknown tag の loss、raw chunk の保持を固定した（`sdk/conversion/passthrough_test.go`）。
- lifecycle failure injection の未検査 phase（decoder/encoder の Flush、muxer の SetMetadata/AddStream）を埋めた（`sdk/engine/wrapper_test.go`、`core/routing/negotiator_lifecycle_test.go`）。
- baseline artifact を go.work tracked 済みの commit へ再固定した。
- generator bootstrap を `runtime.GOOS` 分岐で cross-platform 化した（`tools/cmd/generate/build.go`）。
- module relation の完了条件を設計期 repository 内 composition に限定し、architecture.md に明記した。
- `.gitmodules`、fixtures.md、quality.md、performance.md、一部 test comment に残っていた M1 以前の path/command 表記を同期した。

各項目の詳細な作業内容と検証記録は Git 履歴（コミットメッセージ）を正本とし、本文書では繰り返さない。
