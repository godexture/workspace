# Roadmap checkpoint

> 実装進捗: **8 / 12 マイルストーン完了（M0〜M7）**。M6 の再完了は 2026-08-17、M7 の完了検証は 2026-08-22（M7-6 の stack review 反映まで）。

この文書を M0〜M11 の状態、直近の成果、次の作業、blocker の正本とする。目標と完了条件は [refactor.md](../refactor.md#実装ロードマップ)、各領域の contract はリンク先の設計資料を正本とする。完了までの個別修正や監査の時系列は Git 履歴で追跡し、ここへ再掲しない。

## 更新規則

- 状態は `未着手`、`進行中`、`完了` の三つとする。M10 は他の milestone と並行できるため、複数 milestone が同時に `進行中` でもよい。
- 着手、意味のある中間成果、新しい blocker、完了判定の時に、該当行の状態と checkpoint を更新する。
- checkpoint は現在地を判断できる一文にとどめ、完了済み作業の長い列挙、benchmark の単発値、test log は残さない。再現条件や未解消事項が必要なら専用文書へリンクする。
- `完了` は [refactor.md](../refactor.md#実装ロードマップ) の成果と、詳細資料でその milestone に明示された固有条件を満たした場合だけ付ける。

## 進捗

| ID | 状態 | checkpoint |
|---|---|---|
| M0 | 完了 | correctness、failure semantics、metadata/stream、worker/variant、performance の比較条件を [baseline.md](baseline.md) に固定した。 |
| M1 | 完了 | source monorepo、最終 package path、tracked workspace、単一 release train、generator bootstrap を確定した。 |
| M2 | 完了 | diagnostic、typed config、marker identity、immutable plugin composition、catalog validation、Host construction を完成した。 |
| M3 | 完了 | metadata/side/event、Access/Endpoint foundation contract と第三者拡張点を完成した。 |
| M4 | 完了 | typed Compile/Suggest、bounded solver、public Plan/private Program、binding、実 linear PCM を完成した。 |
| M5 | 完了 | typed runtime、ownership/COW、bounded queue、cancel、Finalize、transactional lifecycle を完成し、旧 contract を切断した。 |
| M6 | 完了 | file/WAVE/PCM、probe/inspect/spool/transaction、standard/testkit/CLI の実経路と R-17 の final contract を repository-wide verification まで確認した（2026-08-17）。 |
| M7 | 完了 | static `Spec.Ports` と typed Router/RoutedReader、buffer を置かない `SerialFanIn` と `flow.Direct` の island gate、単一 `mdat` の unfragmented MP4 direct reader/writer、aggregate `ScratchMaxBytes` と node-local journal、preserve-all/exact `MapStream`、選択 track subset を一つの `muxLayout` で書く remux、必要時だけ選ぶ MP4 PCM binding を完成し、M7-C01〜M7-C12 の negative gate、multi-track × 多 sample を含む physical order、1k/1M resource gate、公式 composition 全体の conformance を確認した（2026-08-22、M7-6 の stack review 反映まで）。remux は source の格納順を保ち、全 track を保つ場合は入力と byte 一致する。 |
| M8 | 未着手 | 公式 family 移行とともに `ilst`/generic loss/strictness、finite seek を実 consumer から確定し、`_legacy/` を削除する。 |
| M9 | 未着手 | stdin/stdout、WASM、demo、rich selector、pure fragmented/sequential MP4、output boundary spool、device/session Endpoint を扱う。 |
| M10 | 未着手 | milestone と並行できる品質・配布基盤を扱う。 |
| M11 | 未着手 | 移行文書を終端処理し、設計文書を恒久化する。 |

## 現在の注記

- M0〜M6 は完了。M6 の再完了判定、根拠、将来の残件は [M0〜M6 実装監査](review-m0-m6.md#m6-再完了条件) に集約し、領域ごとの現行 contract は [runtime](runtime.md)、[access](access.md)、[config](config.md)、[quality](quality.md) を正本とする。
- 現在の foundation は `flow.Item` の単一 ownership slot、Ledger/Domain/Span の failure evidence、Access snapshot/callback boundary、bounded/loss-aware result である。`Fail` が Ledger への唯一の failure ingress で、`Close` は bounded wait と cleanup の完了を担う。cancel normalization は trusted な `context.Cause` の pure single chain だけを採用し、CLI の `ExitCanceled` は caller context state のみを authority とする。旧 review の経緯は [review-m0-m6](review-m0-m6.md) の superseded 注記から辿る。
- M7-0 は dynamic per-track Shape と過剰な同時 surface 追加を撤回し、static `Spec.Ports`、logical `Many` edge、callback の同期直列化と input ordinal を持つ `SerialFanIn`、explicit MP4 graph、default/exact mapping、unfragmented MP4 remux を M7-1〜M7-4 へ分解した。M7-1〜M7-3 は実装・レビュー済みで、M7-4 は conformance 採取・physical order vector・benchmark・resource gate を追加した。MP4 correctness/exact は track ordinal、`Packet.Sequence`、PTS/DTS/duration、per-track sample table、格納順の保持を基準とする。複数 `mdat` は silent mishandling せず、bounded disk index/scratch consumer まで unsupported とする。pure fragmented/sequential MP4、output boundary spool、rich selector、generic loss DTO/`ilst`、public seek、別 container の time-ordered fan-in は実 consumer と backpressure 設計が揃うまで延期した。offline preset の scratch 既定値は 64 MiB、`Realtime` は disabled (0) で、実際の node claim 分だけ journal を作成する。M7 の次作業と traceability は [M7-0](m7-0.md) を正本とする。
- M7-3 の実装で確定した二つの product 判断を記録する。(1) `sidx`/`ssix`/`mfra`/`tfra`/`iloc` のように sample table の外へ byte offset を記録する box を持つ movie は、track subset だけでなく全 track の remux も拒否する。remux は mdat payload を sample 単位で書き直すため、track を落とす選択ではこれらの offset が stale になる。M7-6 でこれを「拒否」から「出力が source を再現する選択だけ受理」へ変えた。判定は plugin 内で完結する: layout が全 track・同一 payload 位置・同一総 size を満たすかを Compile で見て、各 sample が読んだ位置と同じ位置へ書かれるかを Process で確認する。runtime の direct island が将来どうなろうと、ずれれば fail closed になる。(2) `tref` の target track ID は inspection に保持しない。track model を slice 無しで保つ代わりに、`tref` を持つ track を含む subset は fail closed とする。全 track を保つ remux は `trak` を verbatim に複製するため影響しない。
- MP4 の PCM Binding の実 consumer は MP4 → WAVE である。MP4 の mux は preserving remuxer なので MP4 出力は常に copy になり、binding が観測できるのは出力 format が別の wire 表現を要求する場合だけである。little-endian `sowt` は copy、big-endian `twos` は decode/encode を経由する。
- M7-4 で conformance の採取経路を変えた。typed runner は一 stream を一 input port で駆動するため、carrier を持たない MP4 reader と repeated descriptors を受ける mux を `Subject` として表せない。両者は Set を狭めて gate から外れていたので、実行した `plan.Plan` から component を採取する `testkit.Coverage.Observe` を追加し、公式 composition 全体を verify するようにした。MP4 専用の private path は作っていない。
- M7 完了判定で一つだけ寛容に読んだ条件を明記する。「Plan が per-track sample-table reconstruction を示す」は専用 field ではなく、mux node の chunk-offset journal claim（`Node.Scratch` と `Plan.Scratch()`）と `mp4-remux` structural effect で示している。sample table の再生成に必要な state は journal そのものなので、claim が投影されていれば Plan から読み取れると判断した。専用の投影が必要になった場合は M9 の surface 作業で追加する。
- M7-5 は #43〜#53 の stack review で見つけた前提の破れを閉じた。`SerialFanIn` は callback を直列化するだけで到着順は決めないのに、`placeBuffers` が input へ source buffer を挿入して単一 routed producer を route ごとの drain task へ置き換えていた。到着順を mdat の配置に使う MP4 mux はこれで非決定的に失敗し、multi-track × 多 sample では GOMAXPROCS=1 でほぼ常に落ちた。設計文書はこの構成を前提と書いていたが、検査する主体が無く、corpus も「multi-track なら 1 sample」「多 sample なら single track」に分かれていて誰も踏まなかった。現在は serial input を buffer せず、`flow.Direct` を宣言した component が単一 routed producer の island を要求し、満たせない topology は Planning error になる。詳細は [F56/F57](findings.md)。
- M7-5 で同 review の残り指摘も閉じた。`MapStream` が表せるのは「どの track を残すか」だけで、複製と並べ替えは M9 の selector surface へ延期する。`edts` を持つ track は copy では保持されるが decode では失われるため、decodable PCM として広告しない。`plan.Buffer` は `Connections` で private queue 数を示し、Many edge の実コストが `Limit` だけでは過小評価になる問題を閉じた。MP4 の未知 box 受理範囲（[B11](capability.md)、[F58](findings.md)）は記録の上で M8 へ送った。
- M7-6 は #43〜#55 の stack review で見つけた設計上の指摘を閉じた。(1) [F60](findings.md): remux が mdat を track 順に書き直して interleave された movie を de-interleave していた。延期理由だった「ordered policy と cross-route backpressure が要る」は cross-track の *timestamp* 順を作る場合の話であり、source の格納順は同一 file 内の byte offset として入力に既にある。demux が per-track cursor を offset で merge して格納順に emit し、mux が track ごとの journal region へ `Scratch.WriteAt` で記録する形にした結果、全 track を保つ remux は入力と byte 一致する。(2) [F59](findings.md): 全 MP4 fixture が「1 track = 1 chunk」で、chunk-offset table の再構成も journal の page またぎも end-to-end で通っていなかった。fixture builder に chunk 軸を足した。(3) 受理範囲の記録漏れ（[B12](capability.md)/[B13](capability.md)）と、MP4 が remux でしか出力になれないこと（[B14](capability.md)、担当 M9）を記録した。(4) mapping projection が reader の shape に「input port を持たない」ことを要求していた結合を外した。(5) [B15](capability.md): `sidx`/`iloc` 等を持つ movie を、出力が source を再現する選択に限って受理するようにした。判定は plugin 内で閉じ、Compile で layout を、Process で各 sample の着地位置を確認する。
