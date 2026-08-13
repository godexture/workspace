# 現行機能の棚卸しと挙動変更の記録

この文書を、現行実装が持つ利用者から見える機能の正本とする。正式 release 前のため、新経路が現行と同じ挙動を再現することは要求しない。要求するのは、機能ごとに **維持 / 変更 / 廃止** を明示的に決め、決めた内容を担当 milestone で確認することである。

決めずに消えた機能は「改善」ではなく取りこぼしである。[inventory.md](inventory.md) が package の移行先、[findings.md](findings.md) が現行の問題を管理するのに対し、この文書は **能力そのもの**を管理する。

## 判断の意味

| 判断 | 意味 |
|---|---|
| 維持 | 新経路で同等の能力を提供する。表現や API は変わってよい |
| 変更 | 能力は残すが、意図的に挙動・境界・API を変える。理由を「挙動変更の記録」へ書く |
| 廃止 | 新経路では提供しない。理由と代替手段を「挙動変更の記録」へ書く |
| 未定 | 担当 milestone の着手時までに決める。未定のまま旧経路を削除しない |

## format と codec

| 機能 | 現状 | 判断 | 担当 | 確認方法 |
|---|---|---|---|---|
| WAVE (RIFF) format | demux/mux、chunk 解析、raw chunk 保持 | 維持 | M6 | 確認済み: `integration.TestOfficialPluginConformance` の RIFF/Extensible、malformed/truncated vector と `TestWAVEMetadataRoundTripUsesTagBoundParser` の未知 chunk/INFO exact roundtrip |
| PCM codec | 複数 bit depth、left-justify | 維持 | M6 | 確認済み: `integration.TestOfficialPluginConformance` の 12/16 valid bits、little/big endian、mono/stereo decode/encode exact と Fast/Realtime file roundtrip |
| ADPCM (IMA, MS) | encode/decode | 維持 | M8 | 仕様 vector + roundtrip |
| G.711 (A-law, μ-law) | encode/decode | 維持 | M8 | 仕様 table 照合 |
| MP3 format | elementary stream、Xing/VBRI header、scan | 維持 | M8 | 仕様 vector |
| MP3 decode | layer I/II/III | 維持 | M8 | conformance vector + PCM tolerance |
| FLAC format | native stream、STREAMINFO、seektable | 維持 | M8 | conformance corpus |
| FLAC encode/decode | 並列実装、apodization、LPC | 維持 | M8 | conformance corpus + lossless roundtrip exact |

## metadata

| 機能 | 現状 | 判断 | 担当 | 確認方法 |
|---|---|---|---|---|
| ID3v1 / ID3v2 | parse/marshal | 維持 | M8 | 仕様 vector |
| Vorbis Comment | parse/marshal | 維持 | M8 | 仕様 vector |
| RIFF INFO | parse/marshal | 維持 | M6 | 確認済み: `integration.TestOfficialPluginConformance` の parse/marshal、duplicate/order、未知 raw と `TestWAVEMetadataRoundTripUsesTagBoundParser` の container roundtrip |
| multi-value の順序保持 | WAVE で固定済み | 維持 | M3/M7 | Document の順序 test。M3 分は `media/metadata` の `TestDocumentKeepsOrderDuplicateKeysAndOrigin` で確認済み |
| 重複 key の保持 | WAVE で固定済み | 維持 | M3/M7 | Document の重複 test。M3 分は同上の test で確認済み |
| 未知 payload の raw 保持 | WAVE で固定済み | 維持 | M3/M7 | RawBlock の roundtrip。M3 分は `TestRawBlockKeepsUninterpretedPayloadForLosslessRewrite` で確認済み |
| 未知項目の欠落報告 | 現状は黙って落ちる経路がある | 変更 | M7 | loss report の test |

## 音声処理

| 機能 | 現状 | 判断 | 担当 | 確認方法 |
|---|---|---|---|---|
| processor 17 種 | compressor、convert、convolver、dcoffset、delay、equalizer、fade、gain、gate、linear、mixer、normalize、remix、resample、retime、reverb、trim | 維持 | M8 | impulse/step/sine/noise と chunk 境界不変性 |
| filter ごとの byte↔float 変換 | 各 filter が個別に実施 | 廃止 | M8 | 変換回数が filter 数に比例しないことを benchmark counter で確認 |
| 並列 convolver / FLAC | worker 数指定 | 維持 | M5/M8 | M5 は `resource.Grant`、`TestPrepareRejectsAggregateRuntimeResourcesBeforeOpen`、node-local task starter で Job-local grant の予約と実消費上限を確認済み。worker 1/N の出力不変 test は実 codec を戻す M8 |

## runtime と観測

| 機能 | 現状 | 判断 | 担当 | 確認方法 |
|---|---|---|---|---|
| observation Off / Progress / Metrics | pipeline の 3 mode | 変更 | M5 | `TestOffCreatesNoCounterAndNeverReadsClock` と `TestObservationStrategiesDoNotEvaluateDetailedTraitsWhenOffOrBasic` で確認済み |
| cancel 伝播 | pipeline 全体 | 維持 | M5 | queue wait、peer task、PCM Host Run の cancel/leak test で確認済み |
| seek | MP3 demuxer、FLAC seektable | 維持 | M7 + M6/M8 | seek 精度と preroll の test |
| worker pool | `registry.NewWorkerPool(N)` | 変更 | M5 | `resource.Grant`、runtime projection、aggregate reservation test で Job-local resource として確認済み |
| CPU/SIMD dispatch | exported mutable flag と process-wide env 切替 | 変更 | M5/M8 | M5 review で exported mutable state を削除。M8 は immutable `plan.Platform` から Compile/Open 時に direct function を固定し、item loop が feature/env を読まず、Plan fingerprint が選択を説明することを検査 |

## I/O と surface

| 機能 | 現状 | 判断 | 担当 | 確認方法 |
|---|---|---|---|---|
| local file 入出力 | CLI が `os.Open` と temporary file を直接扱う | 変更 | M6/M9 | M6 確認済み: `integration.TestOfficialPluginConformance` の capability 選択、`plugin/file` の commit/abort/cancel/temporary cleanup、positioned write の memory/disk spool、および `cli.TestRunConvertsWaveToRequestedFileFormat` の公式 CLI 接続。M9 は stdin/stdout と全 surface を追加 |
| stdin/stdout | CLI | 維持 | M9 | CLI test |
| Go library API | `sdk/conversion` | 変更 | M6/M9 | M6 確認済み: `standard.Convert`、`standard.NewFileJob`、`standard.NewHost` の example/E2E。M9 は multi-stream と全 surface の policy を追加 |
| CLI | `godec` と 14 flag | 変更 | M6/M9 | M6 確認済み: injected Host 上の file input/output、Plan preview、raw 明示 config、progress、cancel、安定 exit code。M9 は stdin/stdout と残りの flag を追加 |
| playback | Oto 直結の `PlaybackSink` | 変更 | M9 | typed Endpoint + 専用 command |
| WASM binding | 8 関数、全量 `[]byte` | 変更 | M9 | versioned DTO と handle 状態機械の test |
| example web | server + client | 変更 | M9 | catalog 駆動 editor の test |

M5 cut 後はいったん surface 実装を置かず、旧 CLI/WASM/demo source を互換層として残さなかった。M6 で Go と CLI の最短経路を同じ Host façade から新設し、M9 は stdin/stdout、WASM、demo を含む全 surface をその経路へ広げる。判断と後続の確認義務は削除せず、この表で維持する。

## 未定

着手時までに決めるもの。未定のまま旧経路を削除しない。

| 機能 | 現状 | 決める時期 |
|---|---|---|
| `sdk/dsp` の公開 utility | 第三者も利用しうる public API | M8 |
| `sdk/bits` の `production` build tag | assertion semantics を切り替える | M8（[F52](findings.md)） |

*codec 省略時に decoder/encoder を開く挙動* はかつてここにあったが、[C4](decisions.md) が確定判断であり下の B1 がその適用を記録しているため、未定ではない。同じ問題を未決と既決の 2 行で持たない。

## 挙動変更の記録

現行と異なる挙動を新経路で採用した場合、ここへ記録する。記録のない差異は改善ではなく回帰として扱い、原因を調べる。

正式 release 前のため差異そのものは許容するが、**差異を残さないまま能力を落とさない**。旧実装は M5 の切断後も `_legacy/` と M0 baseline commit `4429711a` から読めるため、差異が意図的か bug かを判断する参照として利用できる。ただし実行して比較することはできないので、判断の根拠は仕様と conformance corpus に置く。

| ID | 変更 | 理由 | 担当 |
|---|---|---|---|
| B1 | 無指定出力で decoder/encoder を開かず、可能なら copy/remux を選ぶ | [C4](decisions.md)。入力の情報を保持する既定へ変える | M7 |
| B2 | metadata の表現不能項目を黙って捨てず warning/loss report にする | [C10](decisions.md) | M7 |
| B3 | 数値誤差を許容する variant を policy で選択可能にする | [C7](decisions.md)、[C15](decisions.md) | M5/M8 |
| B4 | FLAC encoder の `Apodizations []Apodization`（関数値）を kind と parameter を持つ data 表現へ変える | 関数値は canonical 表現を持てず、`Tukey(0.5)` と `Tukey(0.9)` を区別できない。異なる bitstream を生むのに Plan と fingerprint に残らず、[performance.md](performance.md#artifactstable) の `ArtifactStable` を満たせない。現状 CLI/WASM からも設定できないため、data 化して初めて surface へ出せる | M8 |
| B5 | file 出力は新規 target を `0666` と process umask で作り、既存 target の permission bit を維持する。一方、既存 target の ACL と file attribute は継承しない | temporary file は同一 directory に通常の file 作成と同じ permission で作り、既存 target があれば rename 前にその permission bit を適用する。rename 後は対応 OS で親 directory を sync する。commit は `os.Rename` とし、Windows でも置換自体は成立させるが、ACL/attribute の継承に必要な `ReplaceFile` と `golang.org/x/sys` は実需要が出るまで採らない | M6 |
| B6 | 出力が同じ job の他の boundary と同一の対象を指す変換を拒否する | 成功 commit が原本を復元不能に破壊するため、既定にしない。同名の打ち間違いだけで起きる。判定は Host が acquire 前に行い、scheme 非依存の下限は reference fingerprint の一致、精緻化は sink trait の `WithEquivalence` が担う。`plugin/file` は output が既存なら `os.SameFile`、非存在なら正規化 path 比較で答える。in-place 対応は需要が確認された milestone で扱い、その時は (1) Windows が `os.Open` に `FILE_SHARE_DELETE` を付けないため入力 handle が開いている間 `os.Rename` が失敗する問題への対応と、(2) 原本を破壊する以上の明示 opt-in の両方を前提とする。(1) は `syscall.CreateFile` を直接使う形になるが、`os.Open` が持つ長い path の `\\?\` 前置処理が抜けるため、`os.NewFile` で包む前にそこを合わせる必要がある | M6（拒否）／需要確認後（in-place） |
| B7 | WAVE の payload、入力由来 chunk、RIFF chunk 外の末尾領域は順序・padding・byte 列を exact に復元するが、file 全体の byte 一致は保証しない | [media](media.md#m6-完了条件) のとおり、全出力の先頭に `ds64` と同じ 28 byte payload を持つ `JUNK` chunk（header 込み 36 byte）を予約する。header 長を固定して `RIFF`/`data` size の後追い patch と 4 GiB 超での RF64 化を成立させるためである | M6 |

## 更新規則

- 現行機能を新しく見つけた場合は、担当 milestone を決めて表へ追加する。
- 判断を `未定` から変える時は、理由が [decisions.md](decisions.md) の判断に依存するならその ID を書く。
- 意図せぬ差異を見つけた場合は「挙動変更の記録」へ足すか bug として直すかを決める。放置しない。
- 各 milestone の完了確認で、その milestone が担当する行の確認方法を実行する。
- M8 で `_legacy/` を削除する前に、`未定` の行が残っていないことと、表の全行が新経路に対応する実装または記録された廃止判断を持つことを確認する。
