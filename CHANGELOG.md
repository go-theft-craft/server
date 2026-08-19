# Changelog

This file records notable user-visible changes. It follows [Keep a Changelog](https://keepachangelog.com/en/2.0.0/) and [Semantic Versioning](https://semver.org/).

## Unreleased

### Added

- The M11 framework: `server.New` and its options turn this repository from a
  program into a framework another module can build a server from. The wire
  protocol and the game data come from the released `minecraft-protocol`
  module; every packet is a generated type, and the only local protocol
  constants are the three in `internal/server/protocolinfo`.

- A version-neutral world model (`pkg/world`): block states are opaque
  handles minted by a registry built from a data set, sections are immutable
  and swapped under a compare-and-swap, and the protocol 47 adapter is the
  only place a handle becomes a number a client understands.

- Persistence through three seams — `WorldStore` (Anvil regions), `SideStore`
  (a chunk-keyed sidecar), and `PlayerStore` — with a one-way migration off
  the pre-M11.3 JSON world files, and a world that is read back from its
  region files rather than regenerated over.

- World generation as named types built from typed parameters, with a golden
  table pinning terrain per (generator, version, seed, parameter hash).

- Item and block provenance, off by default and measured to allocate nothing
  when off: persisted item identities, a write path that keeps
  `len(IDs) == ItemCount` under every click sequence, and a rotating NDJSON
  store with a manifest and per-file bloom filters.

- Observability, off by default and measured to allocate nothing for
  measurement when off, with a closed label set and per-tick accumulation for
  anything more frequent than once per tick per player.

- Commands declared once: a `Command`'s `Signature` is the single declaration
  behind the parser, tab-complete, and the protocol 775 brigadier tree, with
  79 pinned suggestion cases.

- The release gate every other repository has: `task fmt:check`, `secrets`,
  `vuln`, `verify`, and `release:check VERSION=…`, plus CI running `verify`
  on push and pull request. This is the sixth repository to carry the same
  five task names, so "run every gate in every repository" stays one command.

- The public surface is frozen: `api/` holds per-package export data,
  `task api:check` compares the tree against it through `apidiff`, and
  `task api:accept` rewrites it deliberately in the same commit as the change
  it accepts. `verify` runs the check. The tooling is a nested `apicompat`
  module, so a module embedding this server does not inherit `apidiff` and its
  loader.

- `MIGRATION.md`: what breaks and what to do about it, for embedders and for
  world owners separately.

### Changed

- `task test` no longer passes `-mod vendor`, and `task deps` no longer
  vendors: `vendor/` was gitignored and untracked, so the gate passed on a
  prepared machine and failed in every fresh clone with "inconsistent
  vendoring". Dependencies resolve from the module cache, as the other five
  repositories do.

- `task deps` no longer runs `go env -w GOSUMDB=off`, which wrote to the
  user's Go environment file and silently opted every later `go get`, in
  every repository on the machine, out of `sum.golang.org` — in a project
  publishing versions into that same append-only log. It also no longer runs
  `go mod tidy`; tidying is its own deliberate task.
