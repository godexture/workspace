# Godec

Godec is a pre-v1, pure-Go foundation for building extensible media processing and transcoding applications. Applications compose an explicit immutable plugin set; Godec then plans a typed graph and runs it with bounded resources, explicit ownership, cancellation, and transactional output handling.

> Project status: milestones M0–M6 of the foundation refactor are complete. The current tree provides local file conversion for WAVE and linear PCM through the same catalog, planner, runtime, library, and CLI path. MP4, MP3/FLAC, the complete CLI, WASM, and the demo application follow in M7–M9. Do not treat the current API as stable before v1.

## What is here now

- Marker-derived plugin identities and an immutable, validated [`plugin.Set`](plugin/)
- Typed configuration, media schemas, ports, metadata, timing, and buffer ownership
- Deterministic graph validation and bounded bridge solving with a public, inert [`plan.Plan`](plan/)
- A failure-safe [`host.Host`](host/) lifecycle with bounded queues, worker grants, cancellation, finalization, output transactions, and structured results
- Transactional local-file access, content-based WAVE detection, RIFF metadata preservation, and real WAVE/linear PCM implementations
- The [`standard`](standard/) composition, one-call file conversion, and an injected-Host [`cli`](cli/) used by [`cmd/godec`](cmd/godec/)
- A public [`testkit`](testkit/) and an independent [`integration`](integration/) module that exercise official and third-party-style plugins through the same contracts
- Focused examples in package tests, available through `go doc` and pkg.go.dev-compatible tooling

The supported-capability roadmap and remaining work are tracked in [the refactor plan](docs/refactor.md) and [the current checkpoint](docs/refactor/checkpoint.md).

## Requirements

- Go 1.26.4 or newer, matching [`go.mod`](go.mod)
- Git submodules only when optional demo assets or the FLAC conformance corpus are needed; normal builds and unit tests do not require them

## Start exploring

```go
package main

import (
    "context"
    "log"

    "github.com/godexture/godec/standard"
)

func main() {
    if _, err := standard.Convert(context.Background(), "input.wav", "output.wav"); err != nil {
        log.Fatal(err)
    }
}
```

The same path is available from the official command:

```sh
go run ./cmd/godec input.wav output.wav
go run ./cmd/godec --plan input.wav output.raw
```

Input and output must identify different files. Raw PCM input is accepted only with an explicit format and all media-defining properties; run `go run ./cmd/godec --help` for the M6 flags. See the executable examples in [`standard`](standard/example_test.go), [`host`](host/example_test.go), and [`job`](job/example_test.go) for the layered API.

## Develop and verify

Run focused tests while changing a package:

```sh
go test ./host ./plugin/file ./plugin/wave ./plugin/pcm/linear ./standard ./cli
```

Run the repository-wide milestone/release gates from the repository root:

```sh
go run ./tools/cmd/generate
go run ./tools/cmd/docs-check
go run ./tools/cmd/test-runner --simd
```

The full SIMD gate can take substantially longer on a cold cache because optional conformance-heavy packages dominate its runtime. Detailed test tiers and performance rules live in [quality.md](docs/refactor/quality.md) and [performance.md](docs/refactor/performance.md).

## Architecture

Dependencies flow in one direction:

```text
foundation <- plugins <- standard <- applications / CLI / WASM
```

Public packages define contracts for applications and third-party plugins. Planner, scheduler, queue, allocator, and task implementations remain private under [`internal`](internal/). See [architecture.md](docs/refactor/architecture.md) for package boundaries and [experience.md](docs/refactor/experience.md) for the intended user, plugin-author, and maintainer workflows.

## Contributing

Read [`AGENTS.md`](AGENTS.md) before making structural changes. Keep responsibilities cohesive, remove superseded paths instead of preserving compatibility layers, and scale verification to the risk of the change. The active implementation sequence and unresolved handoffs are recorded in [checkpoint.md](docs/refactor/checkpoint.md).

## License

[MIT](LICENSE)
