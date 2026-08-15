# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

A Java Edition 1.8.9 (protocol 47) Minecraft server in Go. It serves a real world: handshake, status, ping, login, play, chunks, inventory, crafting, mining, combat, and persistence.

The wire protocol and the game data both come from
[`minecraft-protocol`](../minecraft-protocol), which this repository consumes as
a released module. This repository owns no wire code of its own; the last local
wire types are the play packet structs in `pkg/gamedata/versions/pc_1_8`, which
M6 replaces with generated ones.

## Development Environment Setup

Uses [Devbox](https://www.jetify.com/devbox) (Nix-based) for reproducible tooling. Setup:
```
direnv allow    # auto-loads environment via .envrc
devbox shell    # or use direnv for automatic activation
```

Key tools come from the `openserbia/go-flake` pins, the same ones
`minecraft-protocol` uses: Go 1.26.6, golangci-lint, gofumpt, govulncheck,
gopls, and delve, plus gci, go-task, and ginkgo.

`devbox.json` sets `GOROOT` explicitly. Without it, entering a shell from a
sibling repository leaks that repository's GOROOT and every build fails with a
toolchain version mismatch.

## Build & Development Commands

All commands use [Task](https://taskfile.dev) (`task <name>`):

| Command | Description |
|---------|-------------|
| `task build` | Build binary to `build/app` (linux/amd64,arm64, CGO disabled) |
| `task deps` | Download, tidy, and vendor Go dependencies |
| `task fmt` | Format code (gci for imports, gofumpt for formatting) |
| `task lint` | Run golangci-lint (runs fmt first) |
| `task test` | Run all tests with coverage |
| `task cleanup` | Remove `build/` directory |
| `task test:interop` | Run the pinned Node client against the server over loopback |
| `task gen:dmd` | Download Minecraft data schemas |
| `task gen:codegen` | Regenerate the play packet structs from `scheme/pc-1.8` |

Run a single test: `go test -mod vendor -run TestName ./path/to/package/...`

## Architecture

- **`cmd/server/`** — The server binary. `task build` builds this, not the repository root.
- **`internal/server/`** — The server itself.
  - `conn/` — one `Connection` per client. It owns a `protocol.Stream` from its
    first byte: the stream does framing, compression, and encryption, and the
    connection dispatches by state. Handshake and status run on generated
    packets; login is delegated whole to `login.Acceptor`; play still decodes
    the local structs with the shared reflect codec, which reads the same `mc`
    tags.
  - `player/`, `storage/`, `packet/` — player state, persistence, and the
    protocol constants the handlers name instead of writing hex literals.
- **`pkg/world/`** — world storage, generation, Anvil region files, and NBT.
- **`pkg/gamedata/versions/pc_1_8/`** — the last local wire types: the play
  packet structs and the version constants. M6 deletes them.
- **`cmd/codegen/`** — generates only those packet structs now. Every game-data
  registry it used to emit comes from `minecraft-protocol/data` instead.
- **`cmd/dmd/`** — downloads the PrismarineJS schemas that `cmd/codegen` reads.
- **`interop/`** — the loopback lane that runs a pinned Node
  `minecraft-protocol` 1.66.2 client against this server.
- **`scheme/`** — downloaded schema JSON. Not tracked; `task gen:dmd` fetches it.
- **`vendor/`** — vendored Go dependencies. All builds use `-mod vendor`.

## Protocol rules

- Never widen a limit or relax a decode check to make a test pass. A generated
  codec that rejects a real client's packet is a bug in `minecraft-protocol`:
  fix it there, add a byte fixture, and re-run.
- The byte-parity fixtures in `internal/server/conn/testdata` are what the
  server puts on the wire. Regenerating them with `-update` asserts the bytes
  were meant to change, so do it deliberately and in the same commit as the
  code.
- Never commit a private key, a session token, or a fixture containing one.

## Development Rules

- **Always use `devbox run`** to execute any command: `devbox run -- <command>`. This ensures the correct Nix-managed toolchain is used. Example: `devbox run -- task build`, `devbox run -- go test ...`.
- **Always use Taskfile** for linting, building, formatting, deps, tidy, and other development commands. Never run `go build`, `gofumpt`, `golangci-lint`, etc. directly — use the corresponding `task` command instead.
- **New CLI commands**: when adding a new command under `cmd/`, always add a corresponding task to `Taskfile.yml` (or the appropriate included Taskfile).

## Code Style

- Import ordering enforced by `gci`: stdlib → third-party → `github.com/openserbia` → project module
- Formatting via `gofumpt` (stricter than `gofmt`)
- Linting via `golangci-lint`
- Build uses `-trimpath` and strips debug info (`-w -s` ldflags)
- Vendored dependencies — always run `task deps` after modifying go.mod
