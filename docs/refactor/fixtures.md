# test corpus、fixture、example asset

## 結論

code、small hermetic fixture、full conformance corpus、benchmark corpus、demo media を同じ repository/module 配布物として扱わない。

- 通常の `go test` は small hermetic fixture と procedural generator だけで offline に完結する。
- full conformance corpus は data submodule または content-addressed manifest で版を固定し、integration job が明示的に取得して使う。
- native/reference comparison と長時間 benchmark corpus はさらに別の opt-in tier にする。
- example asset は独立した一つのrepositoryを正本にできる。各exampleを単独で利用できることに実益があれば、同じrevisionを複数pathのdata submoduleとして参照してよい。
- fixture/corpus の source、license、digest、生成 recipe を production dependency と同じ水準で追跡する。

monorepo は code と contract を atomic に変更するための境界であり、数百 MiB の外部 corpus をすべて Git/Go module zip へ含めるという意味ではない。

## 現状の監査結果

調査時点では次の容量がある。

| directory | files | bytes |
|---|---:|---:|
| `core/test/assets` | 1 | 37,015,596 |
| `example/assets` | 7 | 48,788,521 |
| `example/web/assets` | 7 | 48,788,521 |
| `plugin/flac/test/testdata` | 95 | 310,101,031 |
| `plugin/mp3/test/testdata` | 13 | 11,216,042 |
| `plugin/pcm/test/testdata` | 10 | 302,907,103 |

`core/test/assets/sample_lpcm.wav`、`example/assets/lpcm.wav`、`example/web/assets/lpcm.wav` は SHA-256 が同一である。example の MP3、ADPCM、license も二つの directory に同一内容が複製されている。

PCM の五つの `.snapshot` は各約53 MiBで、decoded sample を `0.000000` のような十進文字列一行ずつに展開している。binary source より snapshot が大きく、float formatting、改行、巨大 diff、review不能な generated text に storage/CI cost を払っている。

FLAC conformance corpus は専用 Git submoduleで、license/source information がある一方、最大約87 MiBの単体 fileを含む。これは codec conformance には価値があるが、foundation/product module の download や通常 unit test に含める理由にはならない。

M1 後は data/asset gitlink として `example/assets`、`example/web/assets`、`plugin/flac/test/testdata/conformance` の3件を意図的に残した。M5 で web wiring を削除した際、前二者が同じ repository の同じ commit を指す重複であることを再評価し、`example/assets` へ統合した。M5 review では Go code を持たない `plugin/flac` tree から corpus を `testdata/flac/conformance` へ移した。現在の2件は product source の分割ではなく、codeと独立して更新・配布される任意取得dependencyである。M10でも一律削除せず、通常testからの分離、固定revision、license、未取得時の挙動を整備する。

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
- WAVE/PCM、MP4、MP3、FLAC vertical path
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

Tier 2 を要求する job だけが、data submoduleまたはmanifestが固定するcorpusを明示取得する。通常の change-scoped test には含めない。network がない環境では `corpus unavailable` と `not requested` を structured result で区別し、release conformance job では unavailable を failure にする。

**format family を新経路へ移す milestone の完了確認では、その family の Tier 2 を必ず実行する。** 正式 release 前は旧実装との出力一致を求めないため、新経路の正しさを示す根拠が仕様と conformance corpus しかない。M5 の切断後は旧実装を実行して比較することもできない。M6 は WAVE/PCM、M7 は MP4、M8 は MP3/FLAC が対象で、この時 unavailable を成功扱いにしない。

MP4 は video/subtitle track を stream copy でしか扱わないため、corpus に必要なのは decode 可能な video ではなく、複数 track、per-track timescale、未知 box、edit list、fragmented/non-fragmented の両形式を含む小型 fixture である。conformance corpus 相当は仕様由来の手書き vector と procedural generator で構成でき、大容量の外部 corpus を必須にしない。

### Tier 3: benchmark/stress

長時間、高 channel、高解像度、極端な metadata、fuzz corpus、soak test を置く。functional correctness test の timeout と artifact size を支配させない。

benchmark の input manifest と output correctness summary を保存する。paired result/profile は精密な採用判断または回帰調査で取得した場合だけ CI artifact とし、Gitへ時系列で蓄積しない。

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

## corpus の版と取得方法

外部corpusには、用途に応じて次の二方式を使える。

- data submodule: 一つの独立repositoryをcorpus全体として再利用し、Git revisionで版を固定する。全体取得で問題なく、upstreamの履歴・license・directory構造をそのまま使いたい場合に選ぶ。現行FLAC conformance corpusはこの方式を維持する。
- manifest/cache: 複数sourceを束ねる、一部fileだけ取得する、Git以外のarchiveを使う、同一contentを複数corpus間でcache共有する場合に選ぶ。

いずれもsource、revision/version、license、取得sizeをreview可能にする。submoduleをmanifestへ機械的に置き換えず、取得選択性やcache共有という具体的な必要が生じた時だけ移行する。

manifest方式を選ぶ場合は、次のようなcontent-addressed記述を使う。

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

manifest方式のfetcherを実装する場合は次を満たす。

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

例:

```text
integration/                 nested Go module
├─ testdata/
│  └─ small/                small redistributable fixtures only
├─ corpus/
│  ├─ flac/                 optional data submodule
│  ├─ mp3.json              manifest when selective fetch is useful
│  └─ stress.json
└─ internal/fixture/         procedural/reference helpers

example/
├─ assets/                   shared asset repository as data submodule
└─ web/
   └─ assets/                same source mounted here when standalone use matters
```

full corpus は product module zipへ含めず、data submoduleなら明示的なcheckout、manifest方式ならworkspace外のcontent-addressed cacheへ置く。`integration` nested module は native/reference dependency と小型 fixture を product moduleから隔離するが、数百 MiB の corpus を module zipへ移すだけの場所にはしない。

test helperのうち第三者 pluginにも有用な generator/assertionだけを public `testkit` へ置く。公式 corpus path、download cache、reference executableは integration internal に保つ。

## example asset

exampleとweb clientが同じmediaを使う場合、独立したasset repositoryを一つの正本にする。各exampleを単独で初期化できる必要があるなら、同じrepositoryを複数のgitlink pathから参照してよい。

- 複数pathのpinを同じrevisionにするか、異なる版が必要な理由を明示する。
- build/dev command はcheckout済みsubmoduleまたは同じ固定revisionのsourceから必要なtargetへcopy/packageする。
- browser demoに37 MiBの無圧縮WAVEを既定bundleしない。短い小型fixtureをdefaultにする。
- 長い/high-quality demoはoptional downloadとし、size/content type/licenseを表示する。
- serverとclient buildが別artifactを必要としてもsource digestは共有する。
- application testは通常test用の小型fixtureと、任意取得のdemo assetを区別する。release/demo buildは使用したasset revisionを記録する。

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
  selected Tier 3 + result artifact; paired/profile only when needed
```

各jobは requested corpus version、submodule checkoutまたはcacheの利用状態、実行/skip/error数、総input bytesを機械可読に報告する。corpusが巨大だからという理由でtimeoutを無制限にせず、tag/shardとbudgetを使う。

## 文書全体の完了条件

この節は fixture/corpus 方針の最終状態を示す gate であり、個別 milestone の完了判定には各 milestone 固有の条件を用いる。この文書の主担当は M10 である。

- product module zipと通常testがfull conformance/benchmark corpusを含まない。
- clean checkoutのTier 0がnetwork/native dependencyなしで完了する。
- full corpusとdemo assetがsource revision/version、license、sizeを持つdata submoduleまたはmanifestで固定される。
- source codeのversion pinにGit submoduleを使わない。data submoduleは独立した任意取得のtest/demo assetに限定する。
- corpus未取得時に通常testが0件実行を成功とせず、`not requested`、`unavailable`、required jobのfailureを区別する。
- PCMの一行一sample巨大snapshotがsmall vector、digest、metric、failure diffへ置換される。
- 同一demo mediaは一つのasset repositoryを正本とし、複数pathに置く場合も独立した内容や履歴へforkしない。
- foundation testが公式format/codec assetへ依存しない。
- manifest/cache方式を使うcorpusのfetch/extractはatomic、bounded、digest-verified、path-safeである。
- third-party pluginがpublic testkitのsmall generator/assertionを利用できる。
- release artifactのSBOM/provenanceが同梱testdata/example assetのlicenseも追跡する。
