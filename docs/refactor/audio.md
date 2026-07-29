# audio data plane

## 現行 filter chain の問題

現行 `media.Frame` は実際には `*media.AudioFrame` を隠す interface である。それに加え、ほぼすべての audio filter が次の処理を個別に行う。

```text
AudioFrame byte planes
  -> audio.DecodeInto
  -> per-filter float32 scratch Block
  -> filter algorithm
  -> audio.EncodeInto
  -> new AudioFrame byte planes
```

`gain`、compressor、equalizer、delay、reverb、resample、trim、mixer 等がこの形である。Scratch により一部の `[]float32` allocation は再利用されるが、各非 no-op filter は次を繰り返す。

- sample format から float32 への全 sample copy/conversion
- float32 から元 sample format への全 sample copy/conversion
- 新しい AudioFrame と byte buffer の取得
- old/new frame の ownership/refcount 操作

入力がすでに F32 planar でも、現在の `ToFloat32`/`FromFloat32` は byte representation と float32 slice の間を全 sample copy する。node ごとの channel を execution island へ fuse するだけでは、このコストは消えない。

16 個の filter chain なら、必要な変換が入口と出口の二回だけで済む場合でも、最大 32 回の全 sample conversion を行い得る。これは architecture refactor で最も大きな性能回帰・改善余地の一つである。

## 原則

1. decoded audio sample は format-less `[]byte` ではなく、scalar type が分かる typed frame として運ぶ。
2. wire endian、PCM packing、24-bit packing は Packet/codec boundary の責務にする。
3. filter が内部で sample format を decode/encode しない。
4. planner が filter region の前後に必要最小限の converter を明示挿入する。
5. compatible filter chain は同じ frame buffer を ownership move または copy-on-write で引き継ぐ。
6. sample rate、channel layout 等の stream invariant を frame ごとに複製・再検証しない。

## typed sample schema

概念上、decoded frame は scalar type を Go type で表す。

```go
type Sample interface {
    ~uint8 | ~int16 | ~int32 | ~float32 | ~float64
}

type Frame[S Sample] struct {
    pts    timing.Timestamp
    planes buffer.Planes[S]
}
```

schema identity は Go type だけに依存せず marker で定義できる。

```go
type f32pID struct{}
type s32pID struct{}

var F32P = schema.Define[f32pID, *Frame[float32]](...)
var S32P = schema.Define[s32pID, *Frame[int32]](...)
```

`p` は planar representation を表す。packed/interleaved representation を data plane として本当に必要とする場合は別 schema/型を定義する。通常の filter region は planar を canonical representation とする。

int32 frame の有効 bit depth、sample rate、channel layout は stream descriptor property に置く。変化しない情報を全 frame に持たせない。frame は PTS、sample count、owned planes 等の item-local 情報だけを持つ。

midstream format change は frame field の突然の変更ではなく、新しい stream epoch/typed event とする。

## component capability

component は受け入れる sample schema と、保持・変更する property を Compile で宣言する。

```text
FLAC Decoder: Packet -> S32P(valid bits = source depth)
PCM Decoder:  Packet -> S16P | S32P | F32P
Converter:    S16P | S32P | F64P <-> F32P
Gain:         F32P -> F32P, preserves rate/layout
Resampler:    F32P(rate A) -> F32P(rate B)
Remix:        F32P(layout A) -> F32P(layout B)
Encoder:      supported typed frames -> Packet
```

同じ algorithm に F32/S32/SIMD implementation がある場合、同じ component identity の variant として宣言できる。実装が F32 だけなら planner が converter を配置する。

filter ごとの config から `Format`/`BitsPerSample` を外す。format conversion は明示 Converter component の責務であり、意味を変えない gain/equalizer が入力の wire storage format を再 encode する必要はない。

## conversion placement

例:

```text
S16P decoder
  -> Converter(S16P -> F32P)
  -> Gain
  -> Equalizer
  -> Compressor
  -> Resampler
  -> Converter(F32P -> S32P)
  -> encoder
```

converter の数は filter 数ではなく、schema region の境界数で決まる。optimizer は隣接 converter の相殺、no-op、encoder/decoder native variant を評価する。

stream copy の場合は decoded audio schema 自体を通らない。

## ownership と in-place 処理

通常 Processor は borrowed input を受ける。payload を変更する filter は `Edit` 相当の API で writable view を得る。

```go
func (p *Gain) Process(
    _ context.Context,
    in flow.Input[*audio.Frame[float32]],
    out flow.Emitter[*audio.Frame[float32]],
) error {
    frame := in.Edit()
    for plane := range frame.Planes() {
        gain.Apply(frame.Plane(plane), p.factor)
    }
    return out.Move(frame)
}
```

意味:

- linear path で input が exclusive なら同じ buffer を直接変更する。
- read-only processor は borrow し、buffer を変更しない。
- fan-out 後の shared buffer を変更する branch だけ copy-on-write する。
- no-op は ownership をそのまま move する。
- sample count/layout を変える processor は必要な output buffer を allocator から得る。

plugin author は refcount 値、`Retain`、`Release` を操作しない。Host/schema の ownership implementation が exclusive/shared を管理する。

## buffer representation

frame ごとに各 plane を別 allocation せず、可能なら一つの aligned backing buffer と plane offset を持つ。

- scalar/SIMD alignment
- plane size と padding
- allocator/resource grant
- read-only/shared flag
- optional device memory handle

を buffer contract が扱う。`Frame[S]` は backing buffer の ownership handle を持つが、plugin に allocator/pool implementation を公開しない。

大きさが同じ出力を作る processor は host-provided reusable buffer を使える。前 frame の buffer を常に pool へ戻して直後に同サイズを取り直す pattern を避ける。

## validation

stream/edge Open 時:

- schema scalar/planarity
- sample rate
- channel layout
- valid bits
- processor constraints

frame construction 時:

- plane count
- plane length
- sample count
- alignment/buffer bounds

hot loop:

- validated typed slice を使用
- schema/type/format switch を繰り返さない
- production fast path で全 property を再検証しない

unsafe/SIMD function の直前には必要な length/alignment contract を一度検証する。

## numerical policy

F32 filter chain、FMA、reduction order、FFT partition は数値差を生じ得る。一方、整数 PCM の pack/unpack や整数 DSP のように scalar と bit-exact な SIMD もあるため、`SIMD = 非決定的` と一括りにしない。

- variant は exact、bounded、semantic-only、schedule/chunk/worker dependence を宣言する。
- `Fast` は correctness を維持した上で、宣言された tolerance 内の F32/SIMD/FMA/parallel variant を選べる。
- `Stable` は execution signature、partition、reduction tree を固定する。exact SIMD は排除しない。
- `Portable` は宣言した architecture/thread domain の byte reproducibility を満たす variant だけを選ぶ。scalar/F64 であることだけを根拠にしない。

planner は converter、sample schema、variant、CPU feature、worker、block/FFT partition、fusion、seed を Plan に記録する。同じ「Gain」でも policy によって implementation を選べるが、Fast を理由に converter を filter ごとへ戻したり、unchecked input を許可したりしない。

許容誤差は component conformance test に tolerance/metric として宣言する。NaN、Inf、clipping、denormal の policy も algorithm family ごとに固定する。stateful filter は worker 数だけでなく chunk boundary、長時間 drift、SNR、phase/stability を検査する。全体の policy vector と実行署名は [性能と再現性](performance.md) に従う。

## multi-input

mixer、sidechain compressor、convolver は、全 input を単に F32P にするだけでは不十分である。

- time base/sample rate
- channel layout
- PTS alignment
- frame/hop size
- EOF/padding

を Compile と fan-in policy で解決する。worker goroutine の到着順では処理しない。必要な resample/remix/rechunk は graph に明示する。

## benchmark contract

次を新しい architecture benchmark に追加する。

1. `S16P -> Gain x N -> S16P`、N = 1/4/16
2. `F32P -> in-place filter x N`
3. read-only chain と modifying chain
4. fan-out 1/2/4 と copy-on-write branch 数
5. resample/remix の allocation と buffer reuse
6. scalar/SIMD、Fast/Stable/Portable
7. 256/1024/4096 sample、mono/stereo/multichannel

受け入れ条件:

- compatible N-filter region の sample format conversion は入口/出口の最大二回で、N に比例しない。
- linear in-place chain の payload allocation/copy は filter 数に比例しない。
- fan-out なしの ownership transfer に atomic refcount increment がない。
- observation off で plane size 計算・timestamp conversion を追加しない。
- Plan に conversion 数と selected sample schema が表示される。

benchmark counter と profile で `Conversions/op`、`FrameAllocations/op`、`BytesCopied/op` を確認する。ns/op だけでは architecture regression を判定しない。
