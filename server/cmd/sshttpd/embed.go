package main

import "embed"

// StaticFS embeds the frontend build output.
// Run `make build` or copy client/dist to server/cmd/sshttpd/static before building.
//
//go:embed all:static
var StaticFS embed.FS
