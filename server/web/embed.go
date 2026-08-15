// Package web carries the built website as files compiled into the binary.
//
// The declaration lives beside the React sources for the reason
// server/migrations/embed.go gives: go:embed cannot reach outside its own
// package directory. Nothing else here is Go, and the Go tool ignores
// node_modules by name, so the two trees sit side by side without noticing each
// other.
//
// dist/ is build output. It is produced by `npm run build` -- by the node stage
// in server/Dockerfile for a real build -- and is not in git, save for the
// .gitkeep that keeps the directory present. That file is the whole reason the
// pattern below says `all:`: an embed pattern that matches nothing is a compile
// error, and `go vet ./...` runs in CI before anything has built the UI. The
// `all:` prefix is what makes a dotfile count as a match, so a checkout with no
// npm anywhere near it still compiles.
//
// A binary built that way serves a plain-text 503 rather than a website. See
// internal/api/site.go.
package web

import "embed"

//go:embed all:dist
var FS embed.FS
