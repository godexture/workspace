# M0/M1 checkpoint

この文書は M0/M1 を完了へ移すための実装 backlog と検証結果を整理する。進捗状態の正本は [refactor.md](../refactor.md)、品質契約は [quality.md](quality.md)、repository/package 方針は [architecture.md](architecture.md) とする。

## 現在の判定

- M0: 完了。一次再監査（differential harness、filter chain benchmark、metadata baseline、lifecycle failure injection、baseline artifact）、二次再監査（任意 differential tool の false positive、撤回した保証の文書反映漏れ、filter chain benchmark の lifecycle 境界、baseline manifest の再現性・構造、convolver end-to-end test の resource/observation 不備）、三次再監査（baseline commit の古さ、filter chain benchmark の teardown 分離不足、checkpoint.md 自身の矛盾記述、convolver test の decode failure 経路の leak）、四次再監査（三次の `processing-ns/op` 計測方法自体の不正確さ、direct-call benchmark の frame leak、pipeline/direct-call 両 benchmark の frame数・block size・Encode-Decode 境界の不一致）のいずれも解消済み。詳細は [baseline.md](baseline.md) を正本とする。
- M1: 完了。monorepo 化、最終 `plugin/<family>` path、tracked workspace、設計期の module relation、path/command 同期、generator bootstrap の cross-platform 対応を完了している。
- `example/assets`、`example/web/assets`、`plugin/flac/test/testdata/conformance` の3 gitlinkは、code と独立した任意取得の共有データとして意図的に維持し、M0/M1 の blocker にしない。
- repository 全体の scalar/SIMD/forced-scalar semantic differential は、検証コストを理由に M0 の保証と必須 gate から外している。`tools/cmd/differential` は任意の診断 tool として残り、`main_test.go` の unit/integration test がその正しさ（子 process の failure を必ず検出すること等）を検証する。scalar/SIMD/worker の semantic diff 自体は、対象 package ごとの differential test（`sdk/dsp`、FLAC parallelism、convolver worker 1/4/16 等）で満たす。

## 四次再監査で解消した項目

- **processing-ns/op の計測方法自体の不正確さ**: 三次再監査で追加した `core/pipeline.NewObserved`/`ObservationMetrics` 経由の計測は、標準の `pipeline.New` のままで既に使えた `Snapshot().Elapsed` を再発明していた。`Pipeline.Run` は node 処理開始直前に `startedAt`、teardown 開始前に `finishedAt` を無条件に記録するため、`Snapshot().Elapsed` は ObservationMode を問わず steady-state-only の値になる。`NewObserved` と、それに依存していた `processingElapsed`（node 単位の timestamp を走査する独自実装）およびそのテストを削除し、`Snapshot().Elapsed` を直接使う形にした。artificially slow `Close` を仕込んで `Snapshot().Elapsed` が teardown を含まないことを確認した上で revert して検証した。
- **direct-call benchmark の frame leak**: `chain_test.go` の `runChain` が、各 stage へ送信した frame を次段の出力で上書きするだけで `Release` していなかったため、多段 chain では最終出力以外の中間 frame がすべて漏れていた。`SendFrame` 直後に `Release` するよう修正し、mutant 検証（修正を外すと 16 段で allocs/op が 440→1080 に増加）で効果を確認した。
- **pipeline/direct-call 両 benchmark の frame数・block size・Encode-Decode 境界の不一致**: `BenchmarkGainChainPipeline` は 8 frame/op・source Encode・sink Decode を含むのに対し、`BenchmarkGainChainDepths` は 1 frame/op で Encode を計測対象外にしており、比較不能だった。`BenchmarkGainChainDepths` を同じ chainDepths×chainBlockSizes、同じ chainFrameCount frame/op、frame ごとの Encode/Decode を含む形に揃えた。この過程で、`pipeline.Link` の既定 buffer（100）により pipeline 側は複数 frame を並行処理でき、direct-call（単一 goroutine で逐次実行）には原理的に再現できない優位性を持つことが判明した（16段/Large で 13 倍）。「差は pipeline overhead のみ」という doc comment の主張を、「overhead と並行実行利得の純計」に訂正した。

## 三次再監査で解消した項目

- **baseline commit の古さ**: `baseline.manifest.json`/`baseline.md` が固定していた commit は、benchmark 修正・固定 seed 化・convolver 修正より **前** で、記載条件をその commit から再現できなかった。全修正を含む commit へ再固定し、benchmark 数値を含む実測値をすべてこの commit 上で再取得した（この後の四次再監査で、同じ理由によりさらに再固定している）。
- **filter chain benchmark の teardown 分離不足**: `Pipeline.Run` が必ず内部で teardown まで行う構造上、外部から steady-state だけを timer で分離する手段がなかった。当時は `core/pipeline` に `NewObserved` という新規公開 API を追加して対応したが、この API は四次再監査で不要と判明し削除した（後述）。
- **checkpoint.md 自身の矛盾記述**: 一次再監査の解消記録に残っていた「differential harness が semantic result を比較するようにした」という記述が、二次再監査自身の指摘（package の PASS/FAIL/MISSING 集約のみで semantic output は比較しない）と矛盾していたため、実態に合わせて修正した。
- **convolver end-to-end test の decode failure 経路の leak**: `runConvolverEndToEnd` が `audio.Decode` 失敗時に `frame.Release()` へ到達せず、正常経路のみ所有権処理が完全になっていた。失敗経路にも `Release()` を追加した。

## 二次再監査で解消した項目

- **differential tool の false positive**: `runSuite` が `*exec.ExitError` を「packages に反映された失敗」と無条件に扱っていたため、`go test` が1件も test event を出す前に失敗する場合（例: 不正な `GOFLAGS`）、`0 package(s)` の clean success として報告されていた。`anyPackageFailed` チェックを追加し、レビュー記載の repro（`GOFLAGS=-definitely-invalid`）を regression test 化して検証した。tool の doc comment も、package status の集約のみを行い semantic output は比較しない、という実際の責務に合わせて書き直した。
- **撤回した differential 保証の文書反映**: quality.md の M0 完了条件、baseline.md の完了条件対応が repository 全体の differential 実行に依存しているように読めていた箇所を、対象 package ごとの differential test で満たす旨に修正した。baseline.manifest.json の forced-scalar variant/command には `requiredForBaseline: false` を明示した。
- **filter chain benchmark の lifecycle 境界**: `Pipeline.Run` は内部で必ず `Prepare` と全 node の teardown（Close）を行うため、construction だけを timer 外へ出しても測定値は「Prepare + steady-state + teardown」のままだった。`Prepare` を timer 外で明示的に呼び、`Run` 内部の再呼び出しを no-op にした（実際に allocs/op が減ることを確認）。teardown は `Pipeline` に分離手段がないため測定範囲に残るが、doc comment で明示した。`BenchmarkGainChainPipelineOpen` も、`Prepare` を呼ばず input 生成を timer 内に含めていた不備を修正した。
- **baseline manifest の再現性・構造**: benchmark 入力 `stereoBlock` が `math/rand/v2` の runtime-seeded package-level generator を使っており実行ごとに変わっていたため、固定 seed のローカル generator に変更した（決定性を確認済み）。manifest の command を shell string から `argv`/`env` の構造化形式に変更し、CPU feature（avx2/avx2fma）と各 benchmark の input generator 仕様（algorithm、seed、size tier）を追加した。
- **convolver end-to-end test の resource/observation**: 入力 frame の Release 漏れ、Engine の Close 漏れを修正し、`ReceiveFrame` のエラーを `engine.ErrEOF` かどうかで判定するようにした（他のエラーは即 test failure）。出力も frame 数・PTS・sample data を保持する形へ変更した。ErrEOF 判定の効果は、`Engine.Flush` が queue を flush しないよう一時的に壊して検証した。

## 一次再監査で解消した項目

- differential harness を fail-safe にした（shared failure を exit 0 として見逃さない、build-fail を見落とさない、scalar variant が呼び出し元の GOEXPERIMENT を継承しない）。実装は package 単位の PASS/FAIL/MISSING 集約であり、semantic output の比較はしない（`tools/cmd/differential`）。SIMD build 内で forced-scalar path を検査する機構を追加した（`sdk/dsp/cpu_simd.go`）。
- filter chain benchmark の construction を steady-state から分離し、output の正しさを検査する test を追加した（`plugin/audio/chain_pipeline_test.go`）。
- stream/metadata baseline を単一 known key から拡張し、multi-value の順序保持、duplicate の上書き、unknown tag の loss、raw chunk の保持を固定した（`sdk/conversion/passthrough_test.go`）。
- lifecycle failure injection の未検査 phase（decoder/encoder の Flush、muxer の SetMetadata/AddStream）を埋めた（`sdk/engine/wrapper_test.go`、`core/routing/negotiator_lifecycle_test.go`）。
- baseline artifact を go.work tracked 済みの commit へ再固定した。
- generator bootstrap を `runtime.GOOS` 分岐で cross-platform 化した（`tools/cmd/generate/build.go`）。
- module relation の完了条件を設計期 repository 内 composition に限定し、architecture.md に明記した。
- `.gitmodules`、fixtures.md、quality.md、performance.md、一部 test comment に残っていた M1 以前の path/command 表記を同期した。

各項目の詳細な作業内容と検証記録は Git 履歴（コミットメッセージ）を正本とし、本文書では繰り返さない。
