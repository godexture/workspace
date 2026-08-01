# M0/M1 checkpoint

この文書は M0/M1 を完了へ移すための実装 backlog と検証結果を整理する。進捗状態の正本は [refactor.md](../refactor.md)、品質契約は [quality.md](quality.md)、repository/package 方針は [architecture.md](architecture.md) とする。

## 現在の判定

- M0: 完了。以下7項目すべてに対応する test/tool/document を追加し、`docs/refactor.md` を更新済み。詳細は [baseline.md](baseline.md) を正本とする。
- M1: 進行中。source history と Go module の統合は済んだが、最終 family path、clean-checkout bootstrap、配布 metadata、surface validation が不足する。
- `example/assets`、`example/web/assets`、FLAC conformance corpus の3 gitlinkは、codeと独立した任意取得の共有データとして意図的に維持し、M1 blockerにしない。

## M0 で行うコード作業

### 1. WAVE truncated input testを厳密化する 〔対応済み: plugins/format-wav/internal/truncated_test.go〕

- truncation長は `0...len(full)` の両端を含め、完全な入力も検査する。
- RIFF/fmt/data headerの境界と、data payloadの開始offsetを明示する。
- demuxerが返したpayload量をfile全長ではなく、実際に存在するdata chunk payload量と比較する。
- zero-length packetの無限列を許さず、packetを全経路でreleaseする。
- truncated payloadの終端errorを分類し、panicしないことだけで成功にしない。

完了条件: header分をpayloadとして水増しするmutant、宣言sizeをfabricateするmutant、完全入力を拒否するmutantをtestが検出する。

### 2. lifecycle failure injectionを完成させる 〔対応済み: sdk/testutil/fault、plugins/format-{wav,mp3,flac}/internal/failure_test.go。node Close自体は全formatでno-opのため検証対象が存在しない旨を記録〕

- demux read、decode/encode Flush、mux AddStream/SetMetadata/WriteHeader/WritePacket/WriteTrailer、sink write、node Closeへ個別にfailureを注入する。
- primary failureとFinalize/Close failureを同時に発生させ、`errors.Is`で双方を保持することを検査する。
- reverse close order、exactly-once close、cancel時のjoin、packet/frame/buffer releaseを検査する。
- WAVE、MP3、FLACのformat wrapperを代表対象にし、generic pipeline mockだけで完了扱いにしない。

完了条件: 各failure phaseでerrorとcleanup結果が決定的に集約され、resource/goroutineを残さない。

### 3. 現行stream/metadata挙動を固定する 〔対応済み: sdk/conversion/passthrough_test.go〕

- target codec/formatを省略した現行routeがdecoder/encoderを開くことを明示的に検査する。
- known metadata、unknown/raw metadata、duplicate/orderの現行伝播・欠落を記録する。
- byte-equal PCM roundtripをstream copyの証拠として扱わない。

完了条件: M7でstream copyを導入した時、codec Openの消滅と情報保持の差を同じfixtureで比較できる。stream copyそのものはM7まで実装しない。

### 4. scalar/SIMDとworkerのdifferential harnessを作る 〔対応済み: tools/cmd/differential(104/104一致)、既存FLAC parallelism testとplugins/filter-audio/internal/convolver/impulse_test.goが1/4/16 worker end-to-end比較を担う〕

- 同一fixtureをscalar build、SIMD build、SIMD-capable build内のforced-scalar pathで実行する。
- exact kernelはartifactまたはlogical output exact、bounded kernelはtolerance、lossless encoderはdecoded PCM exactを検査する。
- item/frame/sample数、順序、timestamp、metadata、digestを比較する。
- FLAC parallelismとconvolver worker 1/4/16をend-to-end outputまで比較する。

完了条件: buildごとの個別passではなく、同一inputに対するcross-build resultが一つのreportに出る。

### 5. filter chain benchmarkを組み直す 〔対応済み: plugins/filter-audio/chain_pipeline_test.go〕

- 1/4/16段gain chainを実 `core/pipeline` 経路で走らせる。
- direct engine chainをlower boundとして別benchmarkにする。
- construction/config/Openとsteady-state frame processingを分離する。
- small/medium/large block、allocation、processed frame/sample count、output toleranceを記録する。
- 後のruntime置換時に旧新をAB/BAで交互実行できるrunner形状にする。

完了条件: scheduler/edge/ownership overheadとfilter kernel costを別々に比較できる。

### 6. observation leak/profile baselineを強化する 〔対応済み: core/pipeline/observation_profile_test.go〕

- aggregateな `runtime.NumGoroutine` 差だけでなく、期限付き収束とgoroutine stack identityを検査する。
- observation off/progress/metricsごとにprocessed item countを検証し、dropした高速化を成功にしない。
- CPU/block/goroutine/heap profileのcommand、input、toolchain、要約を保存する。

完了条件: unrelated goroutineの終了でleakが相殺されず、profile採取を別の開発者が再現できる。

### 7. baseline artifactを固定する 〔対応済み: docs/refactor/baseline.md〕

- machine-readable input manifestにgenerator/fixture digest、size tier、build mode、worker、expected semanticsを記録する。
- human-readable summaryに実行command、toolchain、correctness result、allocation、profile所見を記録する。
- raw profileと時系列benchmark resultはCI artifactとし、Gitへ蓄積しない。

完了条件: 未追跡fileや口頭手順なしでM0の比較条件を再構成できる。

## M1 で行うコードベース作業

### 1. 最終 family package pathへ移動する 〔対応済み: plugin/{flac,mp3,pcm,wave,audio,id3,vorbiscomment}、plugin/identity/identity_test.go〕

- `plugin/flac`、`plugin/mp3`、`plugin/pcm`、`plugin/wave`、`plugin/audio`、`plugin/id3`、`plugin/vorbiscomment` を最終pathとして作る。
- FLAC/MP3は同じfamily内へCodec/Format/Parser実装を集め、公開親package以外は必要に応じて `internal` にする。
- WAVE/PCMは独立familyのままにし、将来Bindingで接続する。
- config/marker typeを最終pathへ置き、reflection由来identityのsnapshot testを追加する。
- 旧 `plugins/codec-*`、`plugins/format-*` pathのwrapper/aliasは残さない。

完了条件: 後からfamily directoryへ `go.mod` を置いてもimport pathとmarker identityが変わらない。

### 2. clean-checkout workspace/tool bootstrapを作る

- monorepoの正規workspaceとして `go.work` と必要なら `go.work.sum` を追跡する。
- ignoredな `test-runner.exe` / `generate.exe` を前提にせず、tracked sourceから起動するcommandを用意する。
- AGENTS.md、開発手順、CIで同じcommandを使う。
- workspace buildに加え、nested moduleを `GOWORK=off` で検証し、暗黙のlocal source解決を検出する。

完了条件: clean checkoutから追加の手作業なしに全moduleのbuild/test/generateを開始できる。

### 3. module version/release relationを明示する 〔対応済み: bindings/wasm, example/go, example/web/server の go.mod〕

`v0.0.3` という実在しないバージョンを require していたのを、Go の慣習的な zero pseudo-version `v0.0.0-00010101000000-000000000000` に置き換え、各 go.mod の `replace` directive 直前に、設計期間限定であること・real release 公開後は `replace` を外し実バージョンへ差し替える必要があること・`replace` は import する側からは無視されるため release の正しさをそこに依存させてはならないことを明示するコメントを追加した。`tools` は root module へ依存しないため対象外。実際の release publish 順序の自動検査（root を先に publish → nested がそのバージョンだけを参照）は、release pipeline 自体が存在しない現時点では対象外とし、[supply.md](supply.md) が定める M10 の release plan へ委ねる。

- nested moduleのroot dependencyに、存在しないreleaseを完成済みversionのように書かない。
- design期間のlocal compositionと、publish時に有効なroot version pinを分ける。
- release時はrootを先にpublishし、nested artifact/moduleがそのversionだけを参照することを自動検査する。
- consumerから無視される `replace` にrelease correctnessを依存させない。

完了条件: workspace外から対象module/artifactをbuildしても、同じsource contractを解決する。

### 4. repository/package metadataを更新する

- `bindings/js/package.json` のrepository URLをmonorepoへ、directoryを `bindings/js` へ変更する。
- README、badge、source link、generator input、package metadataから旧repository pathを除く。
- Vorbis Commentのdefault vendor文字列はrepository URLではなくartifact identityとして扱い、変更するか維持するかをtestとrelease policyで明示する。

完了条件: 配布物からsource repositoryとsubdirectoryを正しく辿れ、旧repository名が意図不明のまま出力へ残らない。

### 5. Go以外のsurfaceも移行検証する

- `GOOS=js GOARCH=wasm` のbuildをroot validationへ含める。
- root `bun.lock` を使うfrozen install、bindings TypeScript build/test、web client typecheck/testをtracked commandにする。
- browser機能をBun制約で実行できない場合、exit 0の通常成功ではなくstructured skipとして報告する。
- M1では少なくともbuild/typecheckを必須にし、real-browser lifecycle testはM9/M10へ接続する。

完了条件: native Goの63 package passだけでmonorepo移行成功と判定しない。

## 後続 milestoneへ残す作業

- stream copy/remux、保存優先default、multi-stream mapping: M7
- Format/Codec/Metadata間の直接importをBinding/standard compositionへ移す: M8
- real browser lifecycleとsurface統一: M9/M10
- FLAC conformance testで、submodule未取得による0件実行を通常成功にせず、`not requested`とrequired jobのfailureを分ける: M10
- demo buildがcheckout済みasset submoduleを使うようにし、GitlinkとDockerfileの別々のrevision pinを一つの入力へ揃える: M10
- data/asset submoduleの取得tier、revision/license/provenance整備: M10
- public plugin testkit、root CI report、SBOM/NOTICE: M10
- 旧core/SDK/factory/resolver/routing/registryの最終削除: M11

## 推奨実装順

1. M0のtruncated/lifecycle testを修正する。
2. M0のdifferential・pipeline benchmark・baseline manifestを完成させる。
3. M0を完了にしてbaseline commitを固定する。
4. 公式pluginを最終family pathへ移し、reflection identityを固定する。
5. workspace/bootstrap、module pin、repository metadataを修正する。
6. native scalar/SIMD、WASM、JS/typecheckをclean checkout相当で検証し、M1を完了にする。
