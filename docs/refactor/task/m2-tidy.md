# M2 仕上げ指示書: test 分割と C17 の実 config 検証

M3 へ進む前に片付ける 2 件の作業指示である。どちらも難易度は高くないが分量がある。前提とスコープ境界は [task/m2.md](m2.md) と同じで、`core`・`sdk`・`plugin/<family>` の production code と surface には触れない。

review で見つかった欠陥の是正は [task/m2-fix.md](m2-fix.md) で完了している。この文書は残った整理作業だけを扱う。

## 1. `config` の test を source の分割に合わせる

`config` の source は責務別に 10 file へ分割済みだが、test は `config/schema_test.go` 一本に 680 行が残っている。M3 は `media`/`access` が対象で `config` を触らないため、今分けるほうが独立して安全である。

- source と同じ境界で分ける。目安は `field_test.go`、`codec_test.go`、`scalar_test.go`、`unit_test.go`、`sum_test.go`、`collection_test.go`、`secret_test.go`、`patch_test.go`、`resolved_test.go`、`describe_test.go`、`wire_test.go`、`validate_test.go`、`schema_test.go`。実際の test 内容に対応する file がなければ無理に作らない。
- 共有 fixture（`testConfig`、`defaultTestConfig`、`testSchema`、`testNestedSchema`、`diagnosticItems`）は 1 file へ集約する。`export_test.go` のような曖昧な名前にせず、`fixture_test.go` のように役割が読める名前にする。
- `example_test.go` と `schema_bench_test.go` はそのまま残す。
- **移動だけとし、同じ commit で test の内容を変えない。** 分割前後で `go test ./config/ -v` の実行 test 名と件数が一致することを確認する。
- 分割後に不要になった import と重複 helper を消す。

## 2. C17 を実 config で検証する

[C17](../decisions.md#c17-config-snapshot-は-codec-clone-だけで構成する) と [config.md](../config.md#m2-完了条件) は「`C` の field が schema に未登録なら schema 登録を失敗させる」と定めた。この規則は M2 の test fixture でしか検証しておらず、M8 で移行する実 config に耐えるかが未確認である。破綻するなら foundation を積み上げた後ではなく今直す。

**prototype であり移行ではない。** 対象 plugin の production code は一切変更しない。

- 対象は次の 2 つ。難しいほうを優先する。
  - `plugin/audio/internal/config` の equalizer（可変長 band）と mixer（port 数）
  - `plugin/flac/internal/codec/config` の encoder（apodization、partition order 等の field 数が多い config）
- 現行の config struct を、M2 の `config.Schema` で表現し直す prototype を test fixture として書く。置き場所は `config` package の test 内でよく、対象 plugin を import しない（構造だけを写す）。
- 次を確認し、結果を報告する。
  - 全 field を登録できるか。できない field があれば、その型と理由
  - 可変長 band を `Slice(Nested(...))` 等で表現でき、canonical fingerprint が安定するか
  - mixer の port 数のように「config が topology を決める」値が field として自然に置けるか
  - 登録に必要な行数。現行の config struct 定義と比べてどれだけ増えるか
- **規則が破綻する場合は独断で C17 を変えず、実装を止めて報告する。** [decisions.md](../decisions.md) の更新規則に従い、product 判断として確認を取る。
- prototype は判断が終わったら残すか消すかを決める。恒久的な test として価値がなければ消す。

## 検証

- 1 の後: `go test ./config/... -race` と、分割前後の test 名一覧の一致。
- 2 の後: `go test ./config/... -race`。prototype が対象 plugin を import していないこと。
- 両方の後: `go build ./...` と `go run ./tools/cmd/test-runner --simd`。runner の exit status を直接確認し、`tail` 越しの exit code で判定しない。

## 中断して確認する条件

[task/m2.md](m2.md#中断して確認する条件) と同じ。特に 2 で C17 の規則が実 config に合わないと判明した場合は、必ず確認を取る。

## 完了時の記録

1. [checkpoint.md](../checkpoint.md) の注記から、片付いた項目（`config` の test 分割、C17 の実 config 検証）を削除する。
2. 2 の結論を [checkpoint.md](../checkpoint.md) の注記か [decisions.md](../decisions.md) へ記録する。規則を変えるなら ledger を更新する。
