# syntax=docker/dockerfile:1
FROM golang:1.26-bookworm AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server .

    FROM scratch AS assets
ARG ASSETS_REF=fb51dc9a8f2ccc8b028939f3b7dd0dc36abec937
ADD --keep-git-dir=false https://github.com/godexture/assets.git#${ASSETS_REF} /

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
COPY --from=assets / /assets/

EXPOSE 8787
ENTRYPOINT ["/server", "-addr=:8787", "-assets=/assets"]
