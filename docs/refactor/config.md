# config contract

## 結論

config は「Go struct に値を入れる補助機能」ではなく、component の control-plane contract として設計する。

- Go struct は plugin が `Compile` で受け取る型付きの値表現である。
- `config.Schema[C]` は field identity、default、preset、decode、validation、表示、canonicalization を定義する唯一の外部契約である。
- library から渡す完全な `C` と、CLI/JSON 等から渡す疎な `Patch` を区別する。
- config は plan 作成前に一度だけ解決し、`Compile`、`Open`、`Run` へ immutable な値として渡す。
- 現行の functional option generator と checked-in `config_options.go` は削除する。
- reflection は schema の登録と surface projection に限定し、data plane では使わない。

この設計により、plugin 開発者は同じ field を struct tag、default 変数、`Validate`、generated option、CLI description、dynamic resolver に重複して記述しなくてよい。利用者は従来どおり「設定しなければ適切な default」で使え、必要な場合だけ型付き config を渡せる。

## 現行設計の問題

現在の config semantics は複数の場所に分かれている。

- config struct と `name`、`help`、`depends-on`、`check`、`editor` tag
- `DefaultXConfig`
- config ごとの `Validate`
- `Dynamic.ResolveConfiguration`
- `sdk/config.Resolve`
- `sdk/cliflag` の reflection decode/describe/bind
- `ApplyPreset(int)`
- generated constructor、`WithX`、`WithPreset`
- catalog や各 surface の field description

この構成では、field の追加や意味の変更が複数経路へ波及する。特に `plugin/audio/config_options.go` は 1,453 行あり、config の本体より生成された API の方が大きい。さらに単一 preset の生成分岐には compile 不能なコードを出すバグがあり、生成物と generator source の drift も起きている。

generator の bug だけを修正しても、重複した public API と source of truth の分散は残る。互換性を残さない前提では、長期的な解決は generator の削除である。

## public model

### Schema

概念上の API は次のようにする。正確な builder 名は実装時に Go の型推論と error handling を検証して決めるが、責務は変えない。

```go
type EncoderConfig struct {
    Compression int
    Verify      bool
}

var EncoderSchema = config.Struct(
    func() EncoderConfig {
        return EncoderConfig{
            Compression: 5,
            Verify:      false,
        }
    },
).
    Field(
        "compression",
        func(c *EncoderConfig) *int { return &c.Compression },
        config.Int().
            Range(0, 8).
            Unit("level").
            Help("Compression effort"),
    ).
    Field(
        "verify",
        func(c *EncoderConfig) *bool { return &c.Verify },
        config.Bool().
            Help("Verify encoded frames"),
    ).
    Preset("fast", func(c *EncoderConfig) {
        c.Compression = 0
    }).
    Preset("balanced", func(c *EncoderConfig) {
        c.Compression = 5
    }).
    Validate(validateEncoderConfig)
```

外部 field ID の `"compression"` は wire/CLI contract なので明示する。Go field rename と wire identity を暗黙に結び付けない。field accessor は型付きにし、対象 field の型変更を compiler が検出できるようにする。

struct tag を schema の主契約にしない。tag だけでは conditional field、nested value、secret、custom codec、preset、cross-field invariant、canonical encoding を十分に表せず、結局別の経路が必要になるためである。単純な schema 用の補助として tag から description を導出する adaptor は許容できるが、公式 plugin は明示 schema を使う。

### 完全値と疎な Patch

Go library の利用者は型付きの完全値を渡せる。

```go
cfg := flac.EncoderSchema.Default()
cfg.Compression = 8

job := transcode.Job{
    Encoder: flac.Encode(cfg),
}
```

config を渡さない経路では schema default を使う。完全な `EncoderConfig` を明示した場合は zero value も含めて全 field が意図された値であり、「zero だから未指定」と推測しない。

CLI、JSON、WASM 等では省略と明示 zero を区別する必要があるため、値を直接 decode せず疎な `config.Patch` を作る。

```text
Patch {
  preset: "fast"
  fields:
    compression: 3
}
```

`Patch` は surface 間の公開 Go struct を兼ねる wire DTO ではない。各 surface が versioned DTO/flag を受け取り、schema の field codec を使って `Patch` に投影する。unknown field は error にする。

### Resolved

解決結果は概念上、次を持つ。

```go
type Resolved[C any] struct {
    Value       C
    Provenance  config.Provenance
    Diagnostics []diagnostic.Item
    Fingerprint config.Fingerprint
}
```

- `Value`: default、preset、明示値を適用し、normalize/validate 済みの immutable snapshot
- `Provenance`: field ごとの `default`、`preset`、`explicit`、`normalized`
- `Diagnostics`: deprecated alias ではなく、丸めや正規化等の説明
- `Fingerprint`: planner memoization と Plan 再現性のための canonical identity

`Resolved` の内部に surface 固有 flag object や生の string map を保持しない。secret は Plan、log、diagnostic では必ず redaction する。

## 解決順序

疎な入力は次の一つの pipeline で解決する。

```text
schema default
  -> named preset
  -> explicit patch
  -> context-free normalization
  -> schema validation
  -> canonicalization
  -> immutable Resolved[C]
```

優先順位は常に `default < preset < explicit` とする。preset は default に対する named patch であり、明示 field を上書きしない。複数 preset の暗黙合成は行わず、一つを選ぶ。必要なら plugin が意味の明確な個別 field を公開する。

現行の `Preset(id, func(*C))` では callback が触れた field を記録できないため、provenance の `preset` は default と適用後の field canonical 値が異なる場合だけ付与する。preset が default と同じ値を再代入した field は `default` と記録される。触れた field を正確に追跡する API は、別の product 判断が必要になるため後続 milestone で扱う。

`ApplyPreset(int)` の magic number は共通 contract から除く。`fast`、`balanced`、`maximum` のような安定した名前を使う。FLAC の compression level のように数値自体に規格・実装上の意味がある場合は、preset ではなく range を持つ通常 field とする。

環境変数、設定 file、CLI flag の優先関係は application/surface の責務である。foundation は任意の環境を読み取らず、選択済みの完全値または `Patch` だけを解決する。

## default と入力依存値

「入力と同じ format/codec を維持する」は component config の hidden mutation で実現しない。入力が判明する前に schema default を解決し、入力依存の選択は `Compile` が Plan に記録する。

例えば sample rate は `0 = auto` のような暗黙 sentinel ではなく、意味を持つ型で表す。

```go
type Rate struct {
    Mode RateMode
    Hertz int
}
```

`ModeAuto` なら `Compile` が入力 descriptor から実効 rate を決める。schema fingerprint は「auto を要求したこと」を表し、Plan fingerprint は選ばれた実効 descriptor も含む。これにより、同じ config が入力ごとに silently mutate されることを避けられる。

input descriptor、host policy、hardware capability と照合して初めて分かる問題は schema validation ではなく `Compile` の structured diagnostic にする。

## 動的 field と topology

現行の `Dynamic.ResolveConfiguration` は、値の正規化、UI field state、入力依存 validation、port shape を一つの mutation hook に集めない。

- config 自体の構文・範囲・cross-field invariant: `Schema.Resolve`
- UI での active/hidden、range、choice の表示: `Schema.View`
- 入力 descriptor との互換性: component `Compile`
- config で変わる input/output port 数: component `Shape`

`depends-on` は表示上の condition であり、inactive field を黙って無視する意味にはしない。inactive field が明示された場合に error、warning、利用のどれにするかを field policy として schema に明記する。

mixer の port 数等を `[in=2,out=1]` という別 parameter map で config type の外に置かない。安定した config 型に count または typed port definition を持たせ、解決済み config を `Shape` が読む。これにより parameter と config の二段 decode、実行時に変わる config type、別々の validation をなくす。

可変長 equalizer は comma 区切り string と動的 slot の組で表さず、型付き slice にする。

```go
type EqualizerConfig struct {
    Bands []Band
}

type Band struct {
    Frequency frequency.Hz
    Gain      decibel.Value
    Width     float64
}
```

CLI は同じ schema から repeatable flag、indexed path、JSON file のいずれかへ投影できる。複雑な値を単一の escape 規則へ無理に押し込まない。

## field type と codec

標準 schema は少なくとも次を扱う。

- bool、符号付き/符号なし整数、有限 float、string
- duration、byte size、frequency、ratio、decibel 等の単位付き値
- enum
- optional/auto を明示する sum type
- nested struct
- ordered slice
- discriminated union
- secret

第三者の custom type は `config.Codec[T]` を schema field に関連付けられるようにする。codec は decode、human-readable encode、canonical encode、type description を提供する。`encoding.TextMarshaler`/`TextUnmarshaler` 用 adaptor は提供できるが、それだけを唯一の拡張方法にはしない。

unordered map は canonical key codec と順序規則を宣言できる場合だけ受け入れる。NaN/Inf、同値な複数表記、負の zero、単位の別表記等は codec が canonical form を決める。canonical form を作れない field は Plan fingerprint を不安定にするため schema 登録を失敗させる。

## validation と diagnostic

schema 構築時に次を自己検証する。

- duplicate/空 field ID
- duplicate/空 preset ID
- invalid default と invalid preset
- unknown/cyclic field dependency
- field 型と codec の不一致
- canonicalization 不能な型
- invalid range、step、choice
- secret の default/表示規則
- nested path の衝突

plugin package の import 時 panic を必須にしない。schema builder は構築 error を保持でき、`plugin.Define`/`host.New` が component identity と field path を含む aggregate error として報告する。conformance testkit は同じ検証を直接実行する。

ユーザー入力の error は field path、入力 source、期待型、制約を構造化して返す。

```text
encoder.flac.compression:
  source: cli
  value: 12
  rule: range
  expected: 0..8
```

文字列だけの `Validate() error` は adaptor として利用できるが、公式 schema は複数 error を一度に返せる structured validator を使う。

## immutability と canonicalization

`Schema.Resolve` は default/value を新しい snapshot へ copy する。caller が後で元の slice/map/config を変更しても、Plan の意味は変わらない。config は小さな control-plane value であることを前提に一度だけ defensive copy し、frame/artwork buffer の clone と同じ仕組みにはしない。

snapshot の唯一の機構は field codec の `Clone` とする。任意の Go 値を推測して複製する generic reflection clone は使わない。[F26](findings.md) のとおり、reflection clone は pointer、slice、map、interface、unexported field の意味を推測し、特に unexported field を静かに shallow copy して snapshot でない値を snapshot と見せる。したがって次を守る。

- reference 型を扱う codec は `Clone` を宣言する。宣言がなければ schema 登録を失敗させる。
- snapshot は「default factory が fresh な値を返す」ことと「登録 field ごとの codec `Clone`」だけで構成する。
- 登録されていない field は canonical/fingerprint にも snapshot にも参加しないため、`C` に未登録の field があれば schema 登録を失敗させる。ただし blank field と zero-size field は config の意味を運ばないため検査対象から除く。mutable field だけを検査すると、未登録の scalar field を持つ二つの値が同じ fingerprint を持ち、planner cache key が別の config を同一視する。config 型の一部を意図的に schema 外へ置くことは認めない。

exported `var DefaultXConfig` は設けない。`Schema.Default()`またはpluginの`Default()`は呼出しごとにfreshな値を返し、slice、map、pointer、functionを含むnested fieldもsnapshot化する。現行FLAC configのようにdefault structを単純代入すると`Apodizations`のbacking arrayを共有し、一つのcallerによる変更が後続Jobのdefaultを変え得る。

schema/descriptor自体は毎回組み立て直さなくてよい。private fieldとcopyを返すread APIによってimmutabilityを型で強制できる場合は、interned valueを共有する。つまり「default valueはfresh」「schema definitionはfrozen」を区別する。

canonical encoding は次を満たす。

- map iteration、registration order、pointer address、reflection address に依存しない
- field ID 順序と nested collection の順序が定義される
- enum、数値、duration、単位付き型の表現が一意である
- secret の生値を public Plan に含めない
- schema identity/version と effective config を区別できる

private `Program` は実行に必要な secret を保持できる。公開 `Plan` は redacted value と、必要なら domain-separated digest だけを持つ。

secret は redaction が本質であるため、surface 表現の decode と encode を逆関数にできない。したがって次の二つを両方守る。

- 構造化 codec の surface encode は secret field を出力しない。decode 側はその field を「未指定」として扱い、default を使う。`Patch` の省略と明示 zero の区別にそのまま乗る。
- redaction marker を値として decode することを error にする。marker が表示、保存 graph、手入力のどこから来ても secret にならない。

前者だけでは利用者が marker を手で入力した経路が残り、後者だけでは secret を含む config の roundtrip が常に失敗する。人間向けの `Codec.Encode` は `<redacted>` を返してよいが、それは表示専用であり wire 表現ではない。

## CLI、WASM、HTTP への投影

surface は schema の read-only description を利用する。

- CLI: flag 名、help、repeatability、enum choice、default 表示
- WASM/HTTP: versioned JSON Schema 相当の DTO description
- UI: editor hint、unit、range、conditional visibility
- catalog: field、preset、default、capability の検索可能な説明
- docs: schema から生成する reference

ただし editor hint や help text は validation semantics ではない。UI が hint を無視しても同じ `Schema.Resolve` が正しさを保証する。

CLI の短い `name:key=value` syntax は単純 scalar に限定して残せる。nested/repeated config は明確な repeatable flag または config file を使い、独自 escape grammar を無制限に拡張しない。

## planner と runtime

planner は `Resolved[C]` を component の typed `Compile` へ渡す。candidate 探索で config 候補を作る場合も、`Suggest` が返した bounded candidate を必ず同じ schema で resolve してから同じ `Compile` に通す。

config は component identity ではない。同じ component marker が異なる config で複数 node に現れてよい。planner cache key は component identity、schema identity、canonical config fingerprint、input descriptor fingerprint、policy fingerprint から作る。

`Open` は config を再 decode/validate しない。`Run` は string map、reflection、schema lookup、field provenance を参照しない。runtime object が必要とする値は typed plan `P` に compile 済みである。

## generator の扱い

`tools/cmd/config-generator`、`tools/internal/config-generator`、checked-in `config_options.go` は最終設計から削除する。

理由:

1. functional option は field ごとに public symbol と apply method を増やすが、config の意味を追加しない。
2. struct literal と schema default で型付き library UX を十分に提供できる。
3. generated API が schema、validation、surface projection と別の source of truth になる。
4. 大きな生成 file が review、検索、API documentation を圧迫する。
5. generator 自体の分岐に既に compile bug と drift がある。

移行中に旧 generator を一時的に再実行する必要がある場合だけ、確認済みの単一 preset bug を最小修正し compile test を付ける。新 architecture の恒久的な品質投資として generator test suite を拡張しない。各 plugin を typed schema へ移した時点で、その plugin の `go:generate` directive と生成物を同時に削除する。

enum table、codec lookup table、SIMD table 等、手書きより生成が適切な別 generator は維持できる。config reference document や surface schema の生成が必要なら、Go source を再解析せず実行可能な `config.Schema` を入力にする。

## 性能

この変更は data-plane 性能を悪化させない。

- schema reflection/decode/validation/canonicalization は host 構築または plan compile 時だけである。
- config は component ごとに一度 resolve し、candidate の再評価は fingerprint で memoize できる。
- `Open`/`Run` は typed `C`/`P` のみを扱う。
- frame/packet ごとの config lookup、map lookup、reflection、serialization、atomic を禁止する。
- immutable snapshot の copy は小さな control-plane value に限定する。

planning 時間への影響は benchmark する。少数 field の schema resolve、nested/repeated config、candidate memoization、canonicalization を個別に測り、候補数に比例して同じ patch を再 decode しない。

## M2 完了条件

M2 は typed config contract を foundation package として新設する milestone である。公式 plugin の config 移行、generator と `config_options.go` の削除、CLI/WASM への投影実装は M8/M9 の作業であり、M2 には要求しない。identity/catalog 側の条件は [plugins](plugins.md#m2-完了条件) を参照する。

- `config.Schema[C]` が field identity、default、preset、decode、validation、canonicalization、表示 description の唯一の外部契約になる。struct tag、default 変数、個別 `Validate` を主契約にしない。
- 完全値 `C` と疎な `Patch` を区別し、省略と明示 zero を取り違えない。unknown field は error にする。これは top-level の field ID だけでなく、nested/slice/map の surface decode にも適用する。
- 解決順序が `schema default -> named preset -> explicit patch -> normalization -> validation -> canonicalization` で固定され、優先順位が常に `default < preset < explicit` になる。複数 preset を暗黙合成しない。
- `Resolved[C]` が `Value`、field ごとの `Provenance`、`Diagnostics`、`Fingerprint` を持ち、解決後に caller が元の slice/map を変更しても意味が変わらない。
- `Schema.Default()` が呼び出しごとに fresh な値を返し、slice、map、pointer、function を含む nested field も snapshot 化する。snapshot は field codec の `Clone` だけで構成し、generic reflection clone を使わない。新 package に exported `DefaultXConfig` を作らない。
- 未 Build の zero `Schema` を含む invalid schema が `Valid()` で true を返さない。schema identity と version は必須とする。
- `C` の field が schema に未登録の場合、schema 登録を失敗させる。canonical/fingerprint に入らない field が config の意味を変えることを許さない。
- 構造化 codec の surface 表現は decode と encode が逆関数になる。`Nested` が返した encode 結果を同じ codec の decode が読み戻せる。secret field はこの対象外とし、surface 表現に現れず、redaction marker の decode は error になる。
- schema 構築時の自己検証（duplicate/空の field・preset ID、invalid default/preset、unknown/cyclic dependency、field 型と codec の不一致、canonicalization 不能型、invalid range/step/choice、secret の default/表示規則、nested path 衝突）が import 時 panic ではなく、component identity と field path を含む aggregate error として host 構築時に報告される。
- ユーザー入力 error が field path、入力 source、期待型、制約を持つ構造化 diagnostic になり、複数 error を一度に返せる。
- canonical fingerprint が map iteration order、registration order、pointer address、process 再起動に依存しない。canonical form を作れない field は schema 登録を失敗させる。
- 標準 field 型（bool、整数、有限 float、string、単位付き値、enum、optional/auto の sum type、nested struct、ordered slice、discriminated union、secret）を扱え、第三者が `config.Codec[T]` を core 編集なしで追加できる。
- secret が `Resolved` の公開表現、diagnostic、error、log に raw value として現れない。
- 入力依存の `auto` を config mutation で表さない型（`Rate` 相当の明示 sum type）を提供する。実際の解決は `Compile` の責務として M4 で扱う。
- 上記を unit/property test で検査し、schema resolve と canonicalization の代表 benchmark を取る。候補ごとの再 decode を避ける memoization の測定は M4 の planner 側で行う。

## 文書全体の完了条件

この節は config contract の最終状態を示す gate であり、M2 単独の完了判定には上記「M2 完了条件」だけを用いる。`config_options.go` と generator の削除は、各 plugin を typed schema へ移した後に完了する。

- field の外部意味が一つの `Schema` から library、CLI、WASM、HTTP、catalog、docs へ投影される。
- config 未指定と明示 zero が区別される。
- default、preset、explicit の優先順位と provenance が全 surface で一致する。
- input 依存の auto 解決が config mutation ではなく Plan に記録される。
- nested/repeated/custom type を第三者が core 編集なしに追加できる。
- invalid schema は host 構築または conformance test で component identity と field path を伴って失敗する。
- canonical fingerprint が map/registration order と process 再起動に依存しない。
- public Plan/log/diagnostic に secret が現れない。
- `config_options.go` と config generator が repository から消える。
- observation 無効時の runtime profile に config reflection、decode、map lookup が現れない。
