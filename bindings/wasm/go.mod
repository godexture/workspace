module github.com/godexture/godec/bindings/wasm

go 1.26.4

require github.com/godexture/godec v0.0.0-00010101000000-000000000000

require (
	github.com/spf13/pflag v1.0.10 // indirect
	golang.org/x/sync v0.21.0 // indirect
)

// Design-period local composition only: v0.0.0-00010101000000-000000000000
// is the conventional Go zero pseudo-version signaling "no real tagged
// release exists yet", resolved via the replace directive below. Once the
// root module publishes real versions, this must become a genuine pinned
// version and the replace must be removed -- a downstream consumer of this
// module ignores this file's own replace directives entirely, so release
// correctness cannot depend on it.
replace github.com/godexture/godec => ../..
