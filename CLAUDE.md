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
| `task build` | Build the examples to `build/` (linux/amd64,arm64, CGO disabled) |
| `task deps` | Download, tidy, and vendor Go dependencies |
| `task fmt` | Format code (gci for imports, gofumpt for formatting) |
| `task lint` | Run golangci-lint (runs fmt first) |
| `task test` | Run all tests with coverage, then the examples lane |
| `task test:profile` | Run the observability off-profile benchmark and its exit-criterion test |
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
- **World generation** — `pkg/world/gen` is named types built from typed
  parameters. A `Factory` owns a name, a version, defaults, a parser, and a
  constructor; a `Registry` is a *value* passed through
  `server.WithGeneratorRegistry`, never a package global, so two servers in one
  test binary cannot see each other's generators. Every block a parameter names
  is a canonical name resolved once through the state registry. The golden
  table in `pkg/world/gen/testdata/golden.json` pins terrain per
  (generator, version, seed, parameter hash) and is regenerated only with
  `-update`, deliberately, in the commit that moves terrain.
- **Persistence** — three seams in `server`, all naming public value types:
  `WorldStore` (Anvil regions), `SideStore` (a chunk-keyed sidecar, written
  empty for M11.5 to fill), and `PlayerStore` (`PlayerData` as JSON). The
  implementations live in `server/` rather than `internal/server/storage`,
  because they hand back public types an internal package cannot name;
  `internal/server/storage` keeps the file primitives and the one-way
  migration off the pre-M11.3 JSON world files.
- **Provenance** — off by default, and the test that matters most is the one
  saying so (`TestProvenanceOffAllocatesNothingExtra`). `world.ItemID` is a
  persisted epoch and a counter; `world.ItemIndex` is the write path for item
  movement and reports a duplication with both locations and the actor;
  `server.Recorder` takes records off the tick through a bounded queue that
  drops and counts rather than blocking; `internal/server/provenance` is the
  rotating NDJSON store with a manifest and a per-file bloom filter. Records
  name blocks canonically, never by handle.
  - Every click path writes through the index. `internal/server/conn/identity.go`
    holds the five primitives that are now the only code changing how many items
    a slot holds — `transfer`, `swapSlots`, `take`, `consume`, `dropFromSlot` —
    and the cursor is a slot number (`slotCursor`) so a click that touches it is
    an ordinary transfer. Do not do arithmetic on `ItemCount` in a handler; a
    count that moves without its IDs breaks the invariant that
    `len(IDs) == ItemCount`. `TestRandomClickSequencesNeverBreakTheInvariant` is
    what says it holds.
  - **Block identity is sparse and lives in the sidecar.** A placed block gets
    an ID (`internal/server/storage/blockid.go`); a generated one never does,
    and `TestUniversalIdentityIsOffByDefault` is what says a build cannot drift
    into giving every block one. Container item identity is in the same sidecar
    (`containerid.go`), because the vanilla format has a field for the items and
    none for their identity.
  - **Identity from disk is reconciled before it can be clicked**
    (`internal/server/storage/reconcile.go`): orphaned block IDs retired,
    shortfalls minted, surpluses retired, survivors claimed in the index. Run
    from `storeLoader.LoadChunk` on the column *before the world publishes it*,
    which is the only window in which writing into a chunk's containers is
    safe — reading the world for a chunk it is still loading would ask it to
    load the chunk again. Player inventories are reconciled in
    `playerBridge.LoadPlayer`, under protocol slot numbering, which is what the
    index and every click path use.
  - **The sidecar generation stamp cannot match across a restart.** Generation
    is a per-run counter the world file does not carry. Nothing is discarded on
    a mismatch — that would discard all identity on every restart — so every
    column is reconciled at load. Do not "fix" this by dropping identity when
    the stamp disagrees.
- **Commands** — a `Command` is a value with a `Signature`, and the signature is
  the single declaration behind three things: the parser (`server/args.go`),
  tab-complete (`server/complete.go`), and the protocol 775 brigadier tree
  (`server/commands/v775`). Adding an argument to one and not the others stopped
  being possible.
  - The ten built-ins are in `server/builtin.go`, in package `server` and not in
    the `server/commands/builtin` the plan named: a command has to name
    `server.Command` and the server has to default to the built-ins, which
    together are an import cycle.
  - `internal/server/conn` cannot name a `server.Command`, so the command and
    completion paths are two function seams (`conn.Dispatcher`,
    `conn.Completer`) set by the server, and `server/conncaller.go` is the
    adapter that turns a `*conn.Connection` into a `server.Caller`.
  - **Suggestions are behavior.** `server/complete_table_test.go` holds 79 cases
    pinned from the `switch cmdName` block that used to be in
    `tab_complete.go`, before it was deleted. A case that changes is a change to
    what a player sees, and the commit that changes it has to say why.
  - `Param.NoSuggest` and `Param.Also` exist because of that table:
    `/tp`'s first argument must offer player names rather than the caller's own
    x, and `/gamemode` must still take `sp` without offering all twelve forms.
  - Permissions default to allowing everything. This server has no operator
    list, and introducing one by default would lock people out of their own
    worlds. An `Authorizer` gates suggestion as well as execution.
  - `server/commands/vanilla/testdata/commands-1.8.txt` is derived from the 1.8
    client language file in `minecraft-protocol`, not typed from memory. Its
    header says which part of it — the aliases — has no upstream source.

- **Observability** — off by default, and `TestOffProfileAllocatesNothingForMeasurement`
  is what says so. `server/labels.go` holds the closed label set and the feature
  list, which is the API: adding a feature means editing that file. `Measure`
  returns a package-level no-op closure and reads no clock when there is no
  observer, which is what makes it acceptable inside the 625-iteration loop that
  sends a join's chunks. `pkg/world` and `internal/server/conn` cannot name a
  `Feature` — they sit below the package that publishes it — so the seam is a
  function taking a string, and the names are declared in `pkg/world/measure.go`
  with a test in `server` asserting the two spellings agree.
  - **Anything more frequent than once per tick per player is accumulated**
    (`server/tickstats.go`) and flushed as one sample per (feature, player).
    Sampling such a path directly is how measurement becomes load. The design
    called the accumulator lock-free on the tick goroutine; it is not, because a
    block write happens on the connection's goroutine.
  - Chunk work is labelled by 32×32 region, not by chunk. `WithChunkDetail()`
    changes that and states its cardinality where somebody reads it.
  - The Prometheus client lives in `examples/go.mod` only. The core module's
    dependency list does not grow for this.

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
