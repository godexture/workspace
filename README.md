# Godec

Godec is a pre-v1, pure-Go foundation for building extensible media processing and transcoding applications. Applications compose an explicit immutable plugin set; Godec then plans a typed graph and runs it with bounded resources, explicit ownership, cancellation, and transactional output handling.

> Project status: the M0–M5 foundation refactor is complete. The current tree provides the catalog, planner, runtime, and a raw PCM vertical path. WAVE/file conversion, the convenience `standard` package, CLI, MP3/FLAC, WASM, and the demo application return in M6–M9. Do not treat the current API as stable before v1.

## What is here now

- Marker-derived plugin identities and an immutable, validated [`plugin.Set`](plugin/)
- Typed configuration, media schemas, ports, metadata, timing, and buffer ownership
- Deterministic graph validation and bounded bridge solving with a public, inert [`plan.Plan`](plan/)
- A failure-safe [`host.Host`](host/) lifecycle with bounded queues, worker grants, cancellation, finalization, output transactions, and structured results
- A real raw PCM implementation in [`plugin/pcm/linear`](plugin/pcm/linear/)
- Focused examples in package tests, available through `go doc` and pkg.go.dev-compatible tooling

The supported-capability roadmap and intentional post-M5 hiatus are tracked in [the refactor plan](docs/refactor.md) and [the current checkpoint](docs/refactor/checkpoint.md).

## Requirements

- Go 1.26.4 or newer, matching [`go.mod`](go.mod)
- Git submodules only when optional demo assets or the FLAC conformance corpus are needed; normal builds and unit tests do not require them

## Start exploring

```go
package main

import (
    "fmt"

    "github.com/godexture/godec/host"
    "github.com/godexture/godec/plugin/pcm/linear"
)

func main() {
    h, err := host.New(host.Plugins(linear.Set()))
    if err != nil {
        panic(err)
    }

    fmt.Println(h.Catalog().Len())
}
```

This composes the raw PCM components without package initialization or a process-global registry. See the executable examples in [`host`](host/example_test.go), [`job`](job/example_test.go), and [`plugin/pcm/linear`](plugin/pcm/linear/example_test.go) for the current API. A short file-to-file conversion example will become the primary quick start when M6 adds the WAVE/file path.

## Develop and verify

Run focused tests while changing a package:

```sh
go test ./host ./plugin/pcm/linear
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
