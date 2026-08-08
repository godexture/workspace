# AGENTS.md

## Instructions

- ファイル・ディレクトリは責務によって構造的に分割する
    - 一つのファイル・ディレクトリが大きくなりすぎないように
- 既存の実装・慣習に固執しない
    - 既存の実装がよい実装であるとは限らない
- スコープに固執しない
    - 仮にスコープ外の変更であっても、全体としてよい方向に向かうのであれば臆せず行う
- 差分を最小化することに固執せず、将来の変更を見据えた、機能追加・修正に強い設計・実装にする
    - より質の良いコードになるのであれば、上位のパッケージやモジュールの修正することも積極的に検討する
- それまでの議論やプランに不足や誤りがあり，実装の途中で問題が生じたり，よりよい解決策を発見したりした際，または新たに意思決定が必要になった際は，独断で判断を行わず，実装を一時中断してユーザーに指示を仰ぐ
- 同じ処理を何度も重複して書かない。
    - 適切に共通化したり、ヘルパーを用意して簡潔に書く
    - 既存の処理を流用したり、既存の処理の一部を共通モジュールあるいは SDK に切り出して利用できないかも検討する
- ディレクトリやファイル、パッケージ単位での集積度を高める
    - 似ている処理が散らばらないようにする
    - 似ていない処理は分離する
    - たとえば、リファクタリングの過程で Frame の書き込み処理を別な package に切り出したら、Frame の読み込み処理も同じ package に集約する
- (後方)互換性のためのコードを残さない
    - 利用する側も同時に修正し、古いコードを残す必要がないようにする
- 途中で必要なくなったコードは削除する
    - 同様に、途中で export する必要がなくなったコードも camelCase に rename し、export しないようにする
- コメントは必要最低限にとどめる
    - コードの意図が明確であれば、コメントは不要
    - コードだけで説明しきれない背景知識等を補足する場合にコメントを利用する
    - コミットメッセージも簡潔に
- 検証範囲は変更内容とリスクに合わせる
    - 実装変更では、まず変更した package と直接影響する経路の test を実行する
    - 全 module test は、広範な変更、milestone の完了確認、release 前に実行する。日常の局所変更では必須にしない
    - benchmark/profile は hot path、allocation、並行処理、runtime 構造に影響し得る変更、または性能回帰の調査時だけ実行する
    - 日常の性能確認は代表 case で、同一条件における処理時間または無視できない allocation が概ね 2 倍以上に悪化する明白な回帰の検出を目的とする。小さな差は gate にしない
    - 小さな性能差を採用判断に使う場合だけ、同一 process の paired benchmark や profile で追加確認する。異なる machine や電源状態の raw timing は比較しない

## Rules
- package などの命名に複合語はなるべく用いない
    - なるべく一単語でより明確な意味を持つ語がないかを先に検討する
    - 例外的に、複合語にした方が明確だと判断した場合は、複合語を用いてよい
- `_legacy/` は移植参照専用である（リファクタリングの M5 で作られ、M8 で削除する）
    - `_` 始まりの directory は go tool が無視するため、build されず import できない。`go build ./...`、`go test ./...`、`gofmt -l` の対象にならない
    - 規格処理や数値 algorithm を新経路へ移植する時に**読む**対象であり、編集・修正・再利用のために復活させる対象ではない
    - 検索の既定対象から外す。旧 API 名で repository を検索した結果に `_legacy/` が現れた場合、それは現行実装ではない
    - 移植が終わった範囲はその場で `_legacy/` から削除する

## Tools
以下のコマンドは事前 build した local binary を前提にせず、tracked source から起動する。同じコマンドを AGENTS.md、開発手順、CI で使う。

- 全モジュールのテスト: `go run ./tools/cmd/test-runner --simd` (at the workspace root)
    - 広範な変更、milestone の完了確認、release 前に使う。通常は対象 package の test を優先する
    - scalar/SIMD 横断の診断が必要な場合は `go run ./tools/cmd/differential ./...` を使う。通常の必須 gate ではない
- 全 generator の実行: `go run ./tools/cmd/generate` (at the workspace root)。generator、入力、生成物に関係する変更、または milestone/release の確認時に使う
- docs の link/anchor 検査: `go run ./tools/cmd/docs-check` (at the workspace root)。設計文書を変更した時、または milestone の完了確認時に使う
- nested module `tools` が root module への暗黙の local source 解決に依存していないことを確認する場合は、`tools` 内で `GOWORK=off go build ./...` を実行する。

CLI、WASM、JS、demo web は M5 の capability hiatus で repository から外れている。M9 で新 surface を追加した時点で、その toolchain と real-browser lifecycle gate をこの節へ追加する。
