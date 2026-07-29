# build、dependency、supply chain

## 結論

source dependency だけでなく、generator、compiler、external reference tool、container base、remote asset、test corpus を一つの build input model で追跡する。

build は次の二段階に分ける。

1. 明示 manifest/lock に従って input を取得し、digest/licenseを検証する。
2. networkを無効化した hermetic build/test が、検証済み input だけから artifact を作る。

公式 artifact は source commit、dirty state、toolchain、dependency、plugin composition、build flag、generated source、wire/corpus version、container base、license/SBOM と結び付ける。単に `go.mod` と `bun.lock` が存在するだけでは不十分である。

## 現状の監査結果

### Go dependency

workspace の cache 済み module graph を調べた範囲では、Go dependency の root license は MIT、Apache-2.0、BSD/ISC 系で、copyleft license text は見当たらなかった。

主な direct dependency:

- optional playback: Oto、PureGo。Apache-2.0
- CLI: Cobra。Apache-2.0、pflag。BSD系、cancelreader。MIT、x/term。BSD系
- example server: Echo と関連 package。MIT
- runtime/tool: x/sync、x/mod 等。BSD系

これは調査時点の local module graph に対する事実であり、将来のversion updateやbuild tag別graphを保証しない。releaseごとにlock/module graphと最終artifactを再走査する。

Apache-2.0/BSD/ISC dependencyがあることと、Godec自身のsourceをMITで提供することは両立できる。ただしthird-party code/dataをMITへrelicenseせず、各license/NOTICE/patent条件を配布物に残す。

Oto/PureGoはplayback distributionだけに置き、pure-Go standard transcode artifactへ不要なplatform code/license surfaceを持ち込まない。

### npm dependency

調査時点の Bun install には140個の一意な package/versionがあり、package metadataは次の内訳だった。

| license | packages |
|---|---:|
| MIT | 118 |
| ISC | 14 |
| Apache-2.0 | 5 |
| BSD-3-Clause | 2 |
| CC-BY-4.0 | 1 |

copyleft fieldやlicense欠落は見当たらなかった。CC-BY-4.0は`caniuse-lite@1.0.30001806`である。build-time databaseとしてだけ使われ最終npm/web artifactへ含まれないか、含まれるなら必要なattributionをNOTICEへ載せるかをartifact scanで判定する。

package manifestのlicense fieldだけを法的正本とせず、lockfileから取得した実packageのlicense text、NOTICE、bundled dataを保存・検査する。

### manifest外 tool

現在の build/test は module/package manifestに現れない executable/inputを使う。

- `gowasm-bindgen@v1.1.0`: commandではversion指定済みだが`go run module@version`で起動し、通常のmodule tool graphから外れる。
- TinyGo: local調査環境は0.41.1だがrepository manifestにversionがない。
- FFmpeg: PCM snapshot/reference decodeに使用。
- `flac`: FLAC roundtrip/conformance testに使用。
- Bun、Docker/BuildKit、Git、GitHub CLI、PowerShell、Make。
- Docker base imageとfrontend。
- Dockerfileのremote Git asset。

`gowasm-bindgen`のcache済みsourceはMITだった。TinyGo runtime/compiler、FFmpeg/flac executable、container baseのlicense/configは別途artifact/toolchain manifestで追跡する。testでexternal executableを起動するだけのものと、runtime/JSをartifactへcopyするものを区別する。

### container build

現行 containerには次の非再現要因がある。

- `golang:1.26-bookworm`、`gcr.io/distroless/static-debian12:nonroot`、`oven/bun:1-alpine`、`alpine`がdigest未固定。
- Dockerfile frontend `docker/dockerfile:1`もfloating tag。
- client buildはroot `bun.lock`をcontextへ含めず、`bun install`にfrozen lockを要求しない。
- client単独contextにはworkspace dependency `@godexture/js`のsource/artifactがなく、documented commandとworkspace構成が一致しない。
- server単独contextはlocal monorepo sourceでなくpublished `v0.0.3` moduleをdownloadし得るため、同じcommitのcore/plugin変更をimageへ反映しない可能性がある。
- server imageはpinned Git commitのasset repositoryをremote `ADD`するが、fetch phase、content digest、license manifestがbuild graphの外にある。

monorepo化後はroot contextと一つのrelease planからserver/client/WASMをbuildし、current sourceとlockを確実に使う。

## dependency class

dependencyを用途で分類する。

| class | 例 | production artifactへの条件 |
|---|---|---|
| foundation runtime | standard library、x/sync等 | product policy/license gate |
| official plugin runtime | pure-Go codec/format | standard compositionで選択 |
| surface runtime | Cobra、Echo、React |該当CLI/server/web artifactのみ |
| optional platform | Oto/PureGo/device SDK | opt-in distributionのみ |
| build tool | generator、TinyGo、Bun、TypeScript | version/digest/licenseをprovenanceへ |
| test/reference | FFmpeg、flac、native decoder | production graphへ入れずtest resultへversion/config |
| corpus/data | conformance/demo assets | manifest/digest/license、artifact inclusionを明示 |

同じmodule/packageが複数classに現れる場合、実際のartifactごとに到達可能なdependency graphを作る。repository全体のunionだけで「production dependency」と判断しない。

## manifest

repository rootにmachine-readable build manifestを置く。正確なfile formatは実装時に選べるが、意味は次を持つ。

```text
Toolchain {
  go
  tinygo
  bun
  nodeCompatibility
  typescript
  dockerFrontend
}

ExternalTool {
  name
  version
  source
  digest?
  license
  requiredFor[]
}

Artifact {
  id
  target
  composition
  inputs[]
  command
  output
  licensePolicy
}
```

- versionだけで同一binaryを保証できないtoolはdistribution digestも固定する。
- platform package managerで取得するtoolはexpected version rangeと実測versionをprovenanceへ残す。
- secret/credentialはmanifestへ値を入れず、required secret name/authorityだけを宣言する。
- input/output pathはrepository root基準でcanonical化し、workspace外へのwriteを明示する。

Goで実装されたbuild toolは`tools` moduleのGo `tool` directiveでversionを固定し、`go tool <path>`から実行する。現在のGo toolchainは`go.mod`の`tool` declarationをサポートする。`go run module@version`をrelease buildのhidden dependencyにしない。

TinyGo、Bun、Docker等のnon-Go toolは同等のtoolchain lockで固定する。developerがsystem installを使える場合も、version mismatchをbuild開始時にfail-fastする。

## build graph

各commandは入力、出力、network、cache、secret、platform requirementを宣言する。

```text
fetch/verify
  -> generate
  -> build
  -> unit/integration/conformance
  -> package
  -> scan/SBOM
  -> sign/attest
  -> publish
```

### fetch/verify

- Go module、npm package、container image、corpus、demo assetを取得する。
- lock、checksum、image digest、corpus manifestを検証する。
- license/NOTICE/source metadataを収集する。
- network accessをこのphaseへ限定する。

### generate

- clean verified toolchainで全generatorを実行する。
- 生成順序、environment、locale、timezone、newlineを固定する。
- 生成物をformat/typecheck/compileし、checked-in stateと比較する。
- config generatorは目標設計どおり削除する。

### build/test

- network off/cache-onlyで実行する。
- source treeへ暗黙のdownload/generated writeを行わない。
- temporary/output directoryをcommandごとに分ける。
- 失敗後にpartial artifactをrelease候補として残さない。

### package

- executable/library、WASM、worker、JS、types/validator、license、NOTICE、SBOMをartifact setとしてまとめる。
- content digestを計算してからimmutable stagingへpublishする。
- package managerごとのpublishable file listを検査し、testdata/profile/secret/source-only fileの混入を防ぐ。

## container

target design:

- base imageとDockerfile frontendをdigestで固定し、更新bot/PRで意図的に更新する。
- root monorepo contextからcurrent product source、root lock、JS workspaceをbuildする。
- `bun install --frozen-lockfile`相当を使い、workspace packageを含むlockを利用する。
- Go dependency cacheはverified module graphに限定し、build時にworkspaceのcurrent packageを使う。
- remote Git `ADD`をやめ、fetch/verify済みasset stage/contextをcopyする。
- multi-architecture imageはplatform別base digestとartifact digestをmanifest listへ記録する。
- runtimeはnon-root、read-only root filesystemを基本にし、job temporary/output用のbounded writable mountだけを要求する。
- serverのnetwork/file authority、port、health、shutdown timeout、temporary cleanupをimage metadata/documentationへ示す。
- image layer、OS package、Go/npm dependency、asset licenseを一つのSBOMへ統合する。

example clientをbuild-only imageとして残す場合は、実行imageと誤認させずartifact export targetとする。実際にserveするimageを提供するなら、HTTP caching/MIME/CSP/compressionとruntime configの責務を明示する。

## reproducibility

media outputのStable/Portableと、compiler artifactのreproducible buildを分ける。

build provenance:

- source commitとdirty flag
- repository/module/workspace manifest digest
- Go/TinyGo/Bun/TypeScript/compiler/LLVM version
- GOOS/GOARCH/GOEXPERIMENT/CGO/build tags
- selected standard plugin composition/catalog fingerprint
- generated source digest
- dependency/lock/container base digest
- build command/environment allowlist
- output digest

`-trimpath`を使うだけでreproducible buildを宣言しない。timestamp、archive order、source map path、npm tar metadata、WASM custom section、container config/layer timestampをartifact別に検証する。

`sdk/bits`の`production`のように、通常testとreleaseでcorrectness assertionを切り替える独自tagは廃止する。残すbuild tagはavailability/platform/instrumentationの意味をmanifestに列挙し、release artifactと同じsemantic variantをCIでtestする。

bit-for-bit reproducibilityが提供できないartifactも、同じinputから何が変わり得るかをprovenanceに記録する。release gateは「再現可能」「差分理由が宣言済み」「未検証」を区別する。

## license policy

公式Godec sourceの目標licenseはMITとする。

- permissive dependencyを許可するが、license text/NOTICE/patent noticeを保持する。
- copyleft runtime dependencyをofficial artifactへ追加しない。
- test/reference toolやcorpusはproduction link/bundleと別graphで評価する。
- build toolが生成物へruntime/code/dataをcopyする場合、build-onlyと決めつけずoutput licenseを追跡する。
- SPDX expression、source/version/digest、modified statusをcomponent/data/toolごとに持つ。
- license scannerのunknown/deprecated/custom resultを黙ってallowしない。
- third-party pluginのlicenseをfoundationが禁止しないが、standard/official compositionのpolicy gateとは分ける。

FFmpeg等をtest executableとして使用する場合は、実行binaryをofficial artifactへ同梱・linkせず、test provenanceにversion/build configurationを記録する。特定の利用方法がlicense義務へ与える影響はrelease時に法務確認できるevidenceを残し、architecture documentだけで法的判断を断定しない。

## CI と release

必須検査:

- lock/module/tool/corpus/image manifestのdrift
- network-off hermetic rebuild
- pure-Go/CGO-disabled standard build
- artifact別dependency reachability
- license/NOTICE/SBOM completeness
- source→artifact provenance
- generated source drift
- container/npm/Go module file allowlist
- secret/absolute local path/usernameのartifact scan
- release artifact digestのplanとの一致

publishはartifactごとに別commandを即時実行せず、全targetのdigest/license/test statusを持つrelease planをreviewしてから行う。partial publishはstateを保存してresumeし、既に公開されたversionを上書きしない。

## 完了条件

- Go/npm source dependencyだけでなくTinyGo、Bun、bindgen、FFmpeg/flac、container base、asset/corpusがmanifestにある。
- release buildがversion未確認のsystem toolとfloating container tagを使わない。
- Go build toolが`tool` directive等のmodule graphに入り、hidden `go run module@version`を必要としない。
- client containerがroot lockとworkspace dependencyを使い、frozen installでbuildできる。
- server/containerが同じmonorepo commitのfoundation/pluginを使い、published旧versionへsilent fallbackしない。
- remote assetはsource commitだけでなくcontent/license manifestでverify済みである。
- official standard artifactのreachable dependencyにcopyleft runtimeがない。
- project MIT、third-party license/NOTICE、CC-BY等のdata attributionがartifactごとに成立する。
- source commit/toolchain/flags/composition/input/output digestをartifactから追跡できる。
- fetch後のbuild/test/packageをnetworkなしで再実行できる。
