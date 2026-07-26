# syntax=docker/dockerfile:1

# Builds the static client bundle, including the @godexture/js WASM runtime
# it embeds. Build context must be the repo root -- the bun workspace root
# (package.json/bun.lock) lives there, and the WASM bindings pull in the
# same Go modules as the server:
#   docker build -f example/web/client/Dockerfile .

# --- @godexture/js's WASM payload -------------------------------------------
FROM tinygo/tinygo:0.41.1 AS wasm
WORKDIR /src

COPY core/ ./core/
COPY pkg/ ./pkg/
COPY plugins/codec-flac/ ./plugins/codec-flac/
COPY plugins/codec-mp3/ ./plugins/codec-mp3/
COPY plugins/codec-pcm/ ./plugins/codec-pcm/
COPY plugins/filter-audio/ ./plugins/filter-audio/
COPY plugins/format-flac/ ./plugins/format-flac/
COPY plugins/format-mp3/ ./plugins/format-mp3/
COPY plugins/format-wav/ ./plugins/format-wav/
COPY plugins/metadata-id3/ ./plugins/metadata-id3/
COPY plugins/metadata-vorbiscomment/ ./plugins/metadata-vorbiscomment/
COPY bindings/wasm/ ./bindings/wasm/

RUN go work init && \
    go work use \
        ./core ./pkg ./bindings/wasm \
        ./plugins/codec-flac ./plugins/codec-mp3 ./plugins/codec-pcm \
        ./plugins/filter-audio ./plugins/format-flac ./plugins/format-mp3 \
        ./plugins/format-wav ./plugins/metadata-id3 ./plugins/metadata-vorbiscomment

# bindings/js/src/generated/{go-main.ts,worker.js} (gowasm-bindgen's output
# from bindings_gen.go) are committed to the repo, so only main.wasm itself
# needs rebuilding here.
# tinygo/tinygo runs as a non-root user, so /out (owned by root) isn't
# writable -- write inside /src (WORKDIR, owned by that user) instead.
RUN --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p out && \
    tinygo build -target wasm -o out/main.wasm ./bindings/wasm && \
    cp "$(tinygo env TINYGOROOT)/targets/wasm_exec.js" out/wasm_exec.js

# --- Client bundle -----------------------------------------------------------
FROM oven/bun:1 AS build
WORKDIR /src

COPY package.json bun.lock ./
COPY bindings/js/ ./bindings/js/
COPY example/web/client/ ./example/web/client/

RUN --mount=type=cache,target=/root/.bun/install/cache \
    bun install --frozen-lockfile

RUN mkdir -p bindings/js/dist && \
    cp bindings/js/src/generated/worker.js bindings/js/dist/worker.js
COPY --from=wasm /src/out/main.wasm /src/out/wasm_exec.js bindings/js/dist/
RUN cd bindings/js && bun run build:ts

RUN cd example/web/client && bun run build

# Build only -- no serving layer. example/web/client/dist holds the built
# static bundle; whatever deploys it (e.g. godec.trap.jp's static hosting)
# serves that directory directly.
