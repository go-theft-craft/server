# Release and versioning rules

This repository uses [Semantic Versioning 2.0.0](https://semver.org/) and Go
module tags in `vMAJOR.MINOR.PATCH` form. Minecraft versions and protocol
numbers do not determine the module version: this server speaks protocol 47 and
that fact has no bearing on whether the next tag is a minor or a patch.

## Version stability

Releases start in the `v0.x.y` series. A `v0` minor release may break an API
while the design settles. Every breaking `v0` change requires a changelog
entry, a `**Breaking:**` marker, and an entry in [MIGRATION.md](MIGRATION.md).

`v1.0.0` starts compatibility guarantees for the public API. From `v2.0.0` the
module path takes the matching major suffix,
`github.com/go-theft-craft/server/v2`.

## Public compatibility contract

The contract is what `api/` records and `task api:check` enforces: the exported
names, signatures, interfaces, and constants of the ten packages in
`api/api.txt`, plus their documented behaviour. `internal/` is not in it, and
neither is the nested `examples` module.

Two things belong to the contract that a Go API check cannot see, because
breaking either costs somebody their world rather than their build:

- **The on-disk formats.** Anvil region files, the chunk-keyed sidecar, and
  per-player JSON. A release that cannot read what its predecessor wrote is
  breaking, and it ships a migration or it does not ship.
- **What the server puts on the wire.** The byte-parity fixtures in
  `internal/server/conn/testdata` are the record. They live under `internal/`
  and are part of the contract anyway: a client is a consumer that cannot be
  recompiled.

## Pick the version change

| Change | Version |
| --- | --- |
| Add an option, seam, generator, or command | Minor |
| Add a field to a struct callers construct with literals | Major after `v1` |
| Remove or rename an exported name | Major after `v1`, minor with migration notes before |
| Change an on-disk format without reading the old one | Major after `v1`, and never without a migration |
| Change what the server sends a client | Minor, with the fixture update in the same commit |
| Correct behaviour without breaking valid callers | Patch |
| Tighten a bound to fix a security issue | Patch, with a `Security` entry and an impact note |
| Change documentation, tests, or lanes only | Patch |

`task api:check` decides the first three rows rather than a reading of the
diff. If it fails and the change is meant, `task api:accept` records the new
surface in the same commit, where a reviewer sees both halves at once.

## Changelog rules

Maintain [CHANGELOG.md](CHANGELOG.md) during development, under `Unreleased`,
in `Added`, `Changed`, `Deprecated`, `Removed`, `Fixed`, or `Security`. Write
entries for users rather than for commit history. At release time rename
`Unreleased` to the version and UTC date in `YYYY-MM-DD` form, and add a fresh
empty `Unreleased` above it.

## Release flow

Release only from a clean `main` whose CI checks pass.

1. Choose the version from the rules above and update `CHANGELOG.md`.
2. Add migration entries for anything breaking.
3. Commit the release preparation.
4. Run `devbox run -- task release:check VERSION=vMAJOR.MINOR.PATCH`.
5. Create an annotated tag on the verified commit.
6. Push the commit and the tag.
7. Create a GitHub release from the matching changelog section.
8. Confirm `go list -m github.com/go-theft-craft/server@vMAJOR.MINOR.PATCH`
   resolves.

`release:check` rejects a dirty tree, a local `replace` directive, and an
invalid version. **Do not move or reuse a published tag.** The module mirror
serves immutable snapshots and the checksum database records them in an
append-only log; rewriting history removes content from GitHub only. Publish a
patch release for a correction.

## Upstream

This repository requires
[`minecraft-protocol`](https://github.com/go-theft-craft/minecraft-protocol)
for every packet and every dataset. Take a released version of it — never a
`replace` directive pointing at a checkout — and tag it before tagging this.
Its own `RELEASING.md` names this repository as a consumer, which means a
release there is not finished until this one requires it or has recorded why
it does not.

The nested `examples` module carries the same version indirectly and is not
tagged separately.

## Consumers

Nothing requires this module yet. The lane that drives `headless-minecraft`'s
client against this server
([record](../headless-minecraft/docs/verification/2026-08-19-owned-server-lane.md))
runs a built binary rather than importing the module, so it does not make that
repository a consumer. When something does require this module, it belongs in
a table here, and a release stops being finished at the tag.
