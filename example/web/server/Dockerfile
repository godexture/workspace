# syntax=docker/dockerfile:1

# Builds the Server-mode API (example/web/server) as a static binary.
#
# example/web/server's go.mod doesn't list core/sdk/the codec+format plugins
# main.go imports -- it only ever builds inside the repo's go.work workspace.
# That workspace reaches outside example/web, so build with the repo root as
# context:
#   docker build -f example/web/server/Dockerfile .

FROM golang:1.26-bookworm AS build
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
COPY example/web/server/ ./example/web/server/

# Recreate a workspace scoped to just what example/web/server's main.go
# imports, instead of copying in the full (unrelated) repo go.work list.
RUN go work init && \
    go work use \
        ./core ./pkg ./example/web/server \
        ./plugins/codec-flac ./plugins/codec-mp3 ./plugins/codec-pcm \
        ./plugins/filter-audio ./plugins/format-flac ./plugins/format-mp3 \
        ./plugins/format-wav ./plugins/metadata-id3 ./plugins/metadata-vorbiscomment

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./example/web/server

# No cgo (audio codecs are pure Go here) so a fully static binary on the
# "static" distroless variant keeps the runtime image minimal.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
COPY example/assets/ /assets/

EXPOSE 8787
ENTRYPOINT ["/server", "-addr=:8787", "-assets=/assets"]
