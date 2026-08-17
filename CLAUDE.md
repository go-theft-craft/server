# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

A Java Edition 1.8.9 (protocol 47) Minecraft server in Go. It serves a real world: handshake, status, ping, login, play, chunks, inventory, crafting, mining, combat, and persistence.

The wire protocol and the game data both come from
[`minecraft-protocol`](../minecraft-protocol), which this repository consumes as
a released module. This repository owns no wire code of its own: every packet is
a generated `minecraft-protocol` type. The only protocol constants that stay
local are the three in `internal/server/protocolinfo` (the advertised version
name deliberately differs from the generated one).

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
| `task build` | Build the three examples to `build/` (linux/amd64,arm64, CGO disabled) |
| `task deps` | Download, tidy, and vendor Go dependencies |
| `task fmt` | Format code (gci for imports, gofumpt for formatting) |
| `task lint` | Run golangci-lint (runs fmt first) |
| `task test` | Run all tests with coverage, then the examples lane |
| `task test:examples` | Build and start each example in the nested `examples` module |
| `task test:race` | Run all tests with the race detector |
| `task cleanup` | Remove `build/` directory |
| `task test:interop` | Run the pinned Node client against the server over loopback |

Run a single test: `go test -mod vendor -run TestName ./path/to/package/...`

## Architecture

- **`server/`** — the framework: `New` and its options, the `Store` and
  `Observer` seams, and the process and network counters. It is public, so
  another module can build a server from it.
- **`config/`** — settings, their defaults, and the file-over-flags merge.
- **`examples/`** — a nested module (its own `go.mod`, with a `replace` for the
  parent) holding `minimal`, `flat`, and `vanilla`. `task build` builds these;
  there is no `cmd/`.
- **`internal/server/`** — the parts an outside module has no business naming.
  - `conn/` — one `Connection` per client. It owns a `protocol.Stream` from its
    first byte: the stream does framing, compression, and encryption, and the
    connection dispatches by state. Handshake, status, login, and play all run
    on generated `minecraft-protocol` packets; login is delegated whole to
    `login.Acceptor`.
  - `player/`, `storage/`, `packet/` — player state, persistence, and the
    protocol constants the handlers name instead of writing hex literals.
- **`pkg/world/`** — the version-neutral world model, plus generation, Anvil
  region files, and NBT.
  - A block state is an opaque `world.State` handle minted by a
    `StateRegistry` built from a `data.Set`. A handle never reaches disk or
    wire: storage holds canonical names and the wire holds each version's own
    encoding. Two registries may give the same block different handles, and a
    test asserts they do.
  - A `Section` is immutable and a `Chunk` holds `*Section` pointers, so a
    write swaps a pointer under a compare-and-swap and a `Snapshot` is a map
    copy. That is what lets `v47` memoize encoded section bytes on the section
    pointer.
  - `v47/` is the protocol 47 adapter and the only place a handle becomes a
    number a client understands. `gen/`, `anvil/`, and `v47/` all depend on
    `world`; `world` depends on none of them, which is why `world.Generator` is
    declared in `world` and satisfied structurally by `gen.Generator`.
  - The world has no override map. A player's edit lives in the chunk it
    belongs to, and so do its chest contents.
  - `nbt/` reads as well as writes, and `anvil/` does too. The world is read
    back from its region files: `world.Loader` is the seam the store plugs
    into, and a load that *fails* marks the column `Unreadable` rather than
    generating over it.
- **Persistence** — three seams in `server`, all naming public value types:
  `WorldStore` (Anvil regions), `SideStore` (a chunk-keyed sidecar, written
  empty for M11.5 to fill), and `PlayerStore` (`PlayerData` as JSON). The
  implementations live in `server/` rather than `internal/server/storage`,
  because they hand back public types an internal package cannot name;
  `internal/server/storage` keeps the file primitives and the one-way
  migration off the pre-M11.3 JSON world files.
- **`interop/`** — the loopback lane that runs a pinned Node
  `minecraft-protocol` 1.66.2 client against this server.
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
