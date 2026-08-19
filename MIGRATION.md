# Migration notes

What breaks between versions, and what to do about it. Each entry says who it
affects — somebody who **embeds** this module, or somebody who **owns a world**
this server wrote — because the two rarely overlap.

`0.1.0` is the first tag, so the entries under it are migrations off an
untagged commit rather than off a release — they are here for anybody who was
building against this repository before it had versions. From `0.1.0` onward
each entry names the versions it spans, and the public surface under `api/` is
what decides whether an entry is needed: `task api:check` fails on an
incompatible change, and `task api:accept` records the new surface in the same
commit as the change and the note.

While the module is in `0.x`, a minor release may break the public API. Each
break gets an entry here and a `**Breaking:**` marker in the changelog.

## 0.1.0

### `server` is a framework, not a program — for embedders

`cmd/server` is gone. A server is built by calling `server.New` with options
and run by calling its `Start`; `examples/vanilla` is what `cmd/server` became
and is the complete example. There is no drop-in replacement for
`go run ./cmd/server`, deliberately: the thing that used to be a program is
now the argument list.

```go
store, err := server.FileStore(dataDir, log)
// ...
srv, err := server.New(
    server.WithSettings(cfg),
    server.WithWorldStore(store.World()),
    server.WithSideStore(store.Side()),
    server.WithPlayerStore(store.Players()),
    server.WithLegacyMigration(dataDir),
)
// ...
err = srv.Start(ctx)
```

### `WithStore` split into three — for embedders

The single `Store` seam named a format in its methods, so a consumer could not
supply one half without reimplementing the other. It is now three seams that
name only public value types:

| Was | Is | Holds |
| --- | --- | --- |
| `WithStore` | `WithWorldStore` | vanilla chunk data, in Anvil region files |
| | `WithSideStore` | everything vanilla has no field for, keyed by chunk |
| | `WithPlayerStore` | per-player state, as the public `PlayerData` |

All three are optional. Omitting one runs without that kind of persistence
rather than failing, which is what makes an in-memory test server one line.

### Block states are handles, not numbers — for embedders

A block is a `world.State`, an opaque handle minted by a `StateRegistry` built
from a `data.Set`. Code that stored a numeric block id must resolve a canonical
name through the registry instead. **A handle is not stable across registries
or across runs**: it never reaches disk or the wire, where a canonical name and
each version's own encoding go respectively. `pkg/world/v47` is the only place
a handle becomes a number a protocol 47 client understands.

### The JSON world files are migrated once, automatically — for world owners

Before M11.3 a world's player-made blocks lived in `world/overrides.json` and
its chests in `world/chests.json`, while the region files were written but
never read back. The JSON was the truth and the regions were a stale copy.

`server.WithLegacyMigration(dir)` folds both files into the world it has just
loaded and renames each to `<name>.migrated`. `examples/vanilla` passes it for
its data directory, so a world that server owns migrates on first start
without being asked. It runs once, because the renamed files no longer match.
**Nothing is deleted** — a rename is one command to undo, and a migration that
deletes its input leaves nobody anything to go back to.

An embedder that composes its own stores gets no migration unless it passes
the option. That is the deliberate half of the design: a module that never
wrote those JSON files should not go looking for them.

To undo it, stop the server and rename the two files back, accepting that
anything played since the migration lives only in the region files.

### Identity is reconciled at load, every start — for world owners

The sidecar's generation stamp cannot match across a restart, because
generation is a per-run counter the world file does not carry. Nothing is
discarded on a mismatch — that would discard all item and block identity on
every restart — so every column is reconciled as it loads: orphaned block IDs
retired, shortfalls minted, surpluses retired, survivors claimed. An external
edit to a world therefore shows up as a recorded reconciliation rather than as
silent corruption. This is expected behaviour and not a fault to fix by
dropping identity when the stamp disagrees.
