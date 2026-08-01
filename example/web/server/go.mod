module github.com/godexture/godec/example/web/server

go 1.26.4

require (
	github.com/godexture/godec v0.0.0-00010101000000-000000000000
	github.com/labstack/echo/v4 v4.15.4
)

require (
	github.com/labstack/gommon v0.5.0 // indirect
	github.com/mattn/go-colorable v0.1.15 // indirect
	github.com/mattn/go-isatty v0.0.22 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasttemplate v1.2.2 // indirect
	golang.org/x/crypto v0.53.0 // indirect
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.38.0 // indirect
	golang.org/x/time v0.15.0 // indirect
)

// Design-period local composition only: v0.0.0-00010101000000-000000000000
// is the conventional Go zero pseudo-version signaling "no real tagged
// release exists yet", resolved via the replace directive below. Once the
// root module publishes real versions, this must become a genuine pinned
// version and the replace must be removed -- a downstream consumer of this
// module ignores this file's own replace directives entirely, so release
// correctness cannot depend on it.
replace github.com/godexture/godec => ../../..
