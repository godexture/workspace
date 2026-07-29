# test corpus、fixture、example asset

## 結論

code、small hermetic fixture、full conformance corpus、benchmark corpus、demo media を同じ repository/module 配布物として扱わない。

- 通常の `go test` は small hermetic fixture と procedural generator だけで offline に完結する。
- full conformance corpus は content-addressed manifest で取得し、integration job が明示的に使う。
- native/reference comparison と長時間 benchmark corpus はさらに別の opt-in tier にする。
- example asset は一つの正本から build/package し、同じ大容量 media を directory/submodule ごとに複製しない。
- fixture/corpus の source、license、digest、生成 recipe を production dependency と同じ水準で追跡する。

monorepo は code と contract を atomic に変更するための境界であり、数百 MiB の外部 corpus をすべて Git/Go module zip へ含めるという意味ではない。

## 現状の監査結果

調査時点では次の容量がある。

| directory | files | bytes |
|---|---:|---:|
| `core/test/assets` | 1 | 37,015,596 |
| `example/assets` | 7 | 48,788,521 |
| `example/web/assets` | 7 | 48,788,521 |
| `plugins/codec-flac/test/testdata` | 95 | 310,101,031 |
| `plugins/codec-mp3/test/testdata` | 13 | 11,216,042 |
| `plugins/codec-pcm/test/testdata` | 10 | 302,907,103 |

`core/test/assets/sample_lpcm.wav`、`example/assets/lpcm.wav`、`example/web/assets/lpcm.wav` は SHA-256 が同一である。example の MP3、ADPCM、license も二つの directory に同一内容が複製されている。

PCM の五つの `.snapshot` は各約53 MiBで、decoded sample を `0.000000` のような十進文字列一行ずつに展開している。binary source より snapshot が大きく、float formatting、改行、巨大 diff、review不能な generated text に storage/CI cost を払っている。

FLAC conformance corpus は専用 Git submoduleで、license/source information がある一方、最大約87 MiBの単体 fileを含む。これは codec conformance には価値があるが、foundation/product module の download や通常 unit test に含める理由にはならない。

PCM corpus には同じ水準の README/license/origin file が見当たらない。生成物であっても source media と期待値の由来、生成 tool/version、再配布条件を明示する必要がある。

## test tier

### Tier 0: unit/property

各 package/module の通常 `go test` で必ず走る。network、native executable、外部 corpus を要求しない。

- 空、最小、境界、最大近傍の手書き vector
- fixed seed の procedural audio/video/packet generator
- truncated/corrupt/oversized input generator
- property/fuzz regression seed
- 数 KiB〜小さな MiB 級の仕様由来 vector
- scalar/reference と optimized variant の differential

repository は Tier 0 の total size budget を manifest で監視する。閾値は実際の test time/module zip sizeを測って設定し、巨大な fixture を一件ずつ例外追加しない。

### Tier 1: integration

公式 plugin、format、metadata、surface を横断する。

- representative multi-stream media
- WAVE/PCM、MP3、FLAC vertical path
- metadata/unknown block preservation
- browser/WASM small streaming fixture
- third-party video/subtitle/schema fixture

code と少数の小型 media は `integration` nested module に置ける。foundation/product module が公式 plugin testdata を import/配布しない。

### Tier 2: conformance

規格団体、reference implementation、外部 project が提供する full corpus を使う。

- FLAC conformance/faulty/uncommon corpus
- MP3 reference vectors
- 将来の video/subtitle/container corpus
- native/reference decoder comparison

通常 test と release gate では manifest が要求する corpus を明示取得する。network がない環境では「test success」と装って skip せず、`corpus unavailable` と `not requested` を structured result で区別する。release conformance job は unavailable を failure にする。

### Tier 3: benchmark/stress

長時間、高 channel、高解像度、極端な metadata、fuzz corpus、soak test を置く。functional correctness test の timeout と artifact size を支配させない。

paired benchmark の input manifest と output correctness summary を保存し、raw profile/result はCI artifactとして retention policyを持たせる。Gitへ時系列のprofileを蓄積しない。

## procedural fixture

PCM/filter test は大容量の音源と一行一sample snapshotより、仕様が明確な deterministic generator を優先する。

```text
SignalSpec {
  seed
  sampleRate
  channels
  frames
  waveform/segments
  amplitude
  boundaryPattern
}
```

- integer/fixed-point で生成できる入力は host float math に依存させない。
- silence、impulse、step、sine、sweep、noise、DC、clipping、denormal policy を小さく組み合わせる。
- `min/max/zero/rounding tie` の raw integer pattern を明示する。
- pseudo-random generator の algorithm/version と seed を固定する。
- failure 時は最初の mismatch、max error、index/channel、周辺 window を表示する。

lossless output は streaming digest、sample count、descriptor、boundary window を比較できる。digest だけでは原因位置が分からないため、failure 時に reference と actual を再実行して bounded diff を出す。

lossy codec/filter は巨大な decimal snapshot ではなく、規格 conformance、tolerance、SNR、peak、phase、drift 等の algorithm contractで検査する。`fmt` の小数表示が同じであることを音質・再現性の代理にしない。

generator と codec implementation が同じ誤りを共有しないよう、次を組み合わせる。

- 仕様から独立に導出できる小型 known-answer vector
- separate simple reference implementation
- 外部 conformance vector
- encode→decode property
- scalar/optimized differential

## corpus manifest

外部 corpus は Git submodule の checkout stateでなく、review可能な content-addressed manifest で固定する。

```text
Corpus {
  id
  version
  sourceURL
  sourceRevision
  archiveSHA256
  archiveBytes
  license
  licenseURL
  files[] {
    path
    sha256
    bytes
    tags
  }
}
```

- URLだけでなく archive/file digestを固定する。
- license textとredistribution/test-only条件をcacheとCI artifactへ添付する。
- file tagでvalid/faulty/large/slow/featureを選択する。
- testはfilesystem walk順でなくmanifest順に実行する。
- corpus version更新は file/digest/license/test expectation の差分としてreviewする。

fetcher は repository tool として次を満たす。

- user cache directoryまたはCI cacheへのatomic download
- partial downloadのresumeまたは安全な破棄
- digest確認後だけpublish
- archive traversal、symlink escape、decompression bombの防止
- max compressed/uncompressed bytes、file count、path length
- offline/cache-only mode
- proxy/mirrorを明示設定可能
- 同じdigestの重複downloadを避ける

corpus 自体を Go `embed` したり production binary から参照したりしない。

## repository と module boundary

推奨 layout:

```text
integration/                 nested Go module
├─ testdata/
│  └─ small/                small redistributable fixtures only
├─ corpus/
│  ├─ flac.json
│  ├─ mp3.json
│  └─ stress.json
└─ internal/fixture/         procedural/reference helpers

example/
├─ assets.json               demo asset manifest
└─ web/                      build consumes manifest
```

full corpus は workspace 外の content-addressed cache に置く。Git submoduleを使わない。`integration` nested module は native/reference dependency と小型 fixture を product moduleから隔離するが、数百 MiB の corpus を module zipへ移すだけの場所にはしない。

test helperのうち第三者 pluginにも有用な generator/assertionだけを public `testkit` へ置く。公式 corpus path、download cache、reference executableは integration internal に保つ。

## example asset

exampleとweb clientが同じmediaを使う場合、source asset/manifestは一つにする。

- build/dev command が同じ verified cache/sourceから必要なtargetへcopy/link/packageする。
- browser demoに37 MiBの無圧縮WAVEを既定bundleしない。短い小型fixtureをdefaultにする。
- 長い/high-quality demoはoptional downloadとし、size/content type/licenseを表示する。
- serverとclient buildが別artifactを必要としてもsource digestは共有する。
- application testはassetの存在だけでなくmanifest digestを検査する。

symbolic linkはWindows、archive、npm publishで扱いが不安定なため、source distributionの正本として依存しない。

## provenance と security

fixtureもuntrusted inputとして扱う。

- source project/revision/URL
- SHA-256、byte size、media properties
- SPDXまたは明示license
- redistributionの可否
- 生成recipe、tool/version
- 手動修正の有無とpatch
- expected valid/invalid classification

parser testはcorpus pathやmetadataをそのままshell commandへ連結しない。CIで外部archiveを展開する処理はproduct parserと同じくbounds/path traversalをtestする。

copyleft corpusをtest-onlyで利用する場合も、公式source/npm/Go module artifactへ再配布するか、CIで外部取得するかをlicenseごとに判断する。test-onlyであることはlicense義務を自動的に消さない。

## CI

```text
pull request:
  Tier 0 + selected Tier 1

scheduled:
  full Tier 1 + cached Tier 2 + fuzz corpus

release:
  required Tier 0/1/2 + license/digest verification

benchmark:
  selected Tier 3 + paired result/profile artifact
```

各jobは requested corpus version、cache hit/miss、実行/skip/error数、総input bytesを機械可読に報告する。corpusが巨大だからという理由でtimeoutを無制限にせず、tag/shardとbudgetを使う。

## 完了条件

- product module zipと通常testがfull conformance/benchmark corpusを含まない。
- clean checkoutのTier 0がnetwork/native dependencyなしで完了する。
- full corpusがsource revision、digest、license、sizeを持つmanifestで固定される。
- Git submoduleをcode/corpusのversion pinに使わない。
- PCMの一行一sample巨大snapshotがsmall vector、digest、metric、failure diffへ置換される。
- 同一demo mediaがcore/example/web directoryへ重複保存されない。
- foundation testが公式format/codec assetへ依存しない。
- corpus fetch/extractがatomic、bounded、digest-verified、path-safeである。
- third-party pluginがpublic testkitのsmall generator/assertionを利用できる。
- release artifactのSBOM/provenanceが同梱testdata/example assetのlicenseも追跡する。
