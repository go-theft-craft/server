# Server Play-State Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the server's last locally-owned wire types with `minecraft-protocol`'s generated protocol 47 packets, delete the server's remaining code generation, and let the session propose state transitions instead of the connection mirroring them.

**Architecture:** `writePacket` currently marshals a local struct through the shared reflect codec and hands the stream a raw payload. Generated packets encode through the session instead, so the migration changes one function and then retypes its call sites. The change is wide — 19 files, 71 packet types, about 80 call sites — and its safety net is the byte-parity fixture suite M3 built, which must keep passing unchanged at every step.

**Tech Stack:** Go 1.26.6 via `openserbia/go-flake`, Devbox, Task, `minecraft-protocol` as a released module.

## Global Constraints

- Work in `/home/ocharnyshevich/pet.projects/go-theft-craft/server`.
- Run every command as `devbox run -- task <name>`. Never call `go` directly.
- **The byte-parity fixtures are the contract.** M3 captured them from the unmigrated server and they must pass, unchanged, after every task. A fixture that needs editing to pass is a migration bug, not a stale fixture — stop and find out why.
- This migration changes no byte on the wire. The status response keeps advertising `"1.8.8"`, not `"1.8.9"`; reconciling those two names is a separate decision.
- `devbox.json` must keep setting `GOROOT` explicitly. Without it a shell entered from a sibling repository leaks its `GOROOT` and every build fails on a toolchain mismatch.
- Leave changes uncommitted only when told to. Each task ends with a commit.
- Never add the `Co-Authored-By` or `Claude-Session` trailer to a commit message.
- Run `devbox run -- task precommit` before every commit.

## Dependencies

None. M6.1 needs only the released `minecraft-protocol`, whose `generated/java/v1_8` already carries all 74 clientbound and every serverbound protocol 47 play packet. It is independent of M4, M5, M6.3, and M7, and can run at any time.

## What is being replaced

`pkg/gamedata/versions/pc_1_8` holds 112 generated packet types, of which **74 identifiers are referenced outside the package** across 19 files. Those 74 include three constants — `ProtocolVersion`, `VersionName`, and `MetadataEnd` — which need homes of their own, and a handful M3 already migrated on the wire but which tests still reference.

The naming conventions differ and there is no mechanical rule that maps one to the other:

| Server | Generated |
| --- | --- |
| `pkt.AbilitiesCB` | `v1_8.PlayClientboundAbilities` |
| `pkt.AbilitiesSB` | `v1_8.PlayServerboundAbilities` |
| `pkt.PositionCB` | `v1_8.PlayClientboundPosition` |
| `pkt.PositionSB` | `v1_8.PlayServerboundPosition` |
| `pkt.KickDisconnect` | `v1_8.PlayClientboundKickDisconnect` |
| `pkt.SetProtocol` | `v1_8.HandshakingServerboundSetProtocol` |

Field names and types also differ, because the generator names fields from the schema and `cmd/codegen` named them from a hand-maintained list. **Do not assume a field survives a rename.** Task 1 produces the mapping and Task 2 proves it compiles before any behavior moves.

## File Structure

**Deleted at the end:**

| Path | Why |
| --- | --- |
| `pkg/gamedata/versions/pc_1_8/` | Replaced by `generated/java/v1_8` |
| `pkg/gamedata/` | Nothing else remains in it |
| `cmd/codegen/` | Generated only the package above |
| `cmd/dmd/` | Downloaded the `protocol.json` that `cmd/codegen` read |

**Modified:**

| File | Change |
| --- | --- |
| `internal/server/conn/stream.go` | `writePacket` writes `Value`, not `Payload`; drop the local state mirror |
| `internal/server/conn/handler_play.go` | Retype every play handler |
| `internal/server/conn/handler_handshake.go`, `handler_status.go` | Retype the last references |
| `internal/server/conn/inventory.go`, `commands.go`, `tab_complete.go`, `legacy_ping.go` | Retype |
| `internal/server/player/manager.go`, `metadata.go`, `item_entity.go`, `player.go` | Retype |
| `internal/server/server.go` | Retype |
| `pkg/world/chunk.go` | Retype |
| `internal/server/conn/*_test.go`, `internal/server/player/*_test.go` | Retype |
| `Taskfile.yml` | Drop the `codegen` and `dmd` tasks |
| `docs/`, `CHANGELOG.md`, `../headless-minecraft/MASTER_PLAN.md` | Documentation |

---

## Stage A — Establish the mapping

### Task 1: Generate and review the type mapping

A 71-type rename done by hand and by eye will get one wrong, and a packet whose
fields are transposed still compiles. Produce the mapping mechanically, review
it once, and treat it as the migration's reference.

**Files:**
- Create: `docs/migration/2026-08-15-packet-mapping.md`

**Interfaces:**
- Produces: a reviewed table mapping every referenced `pkt.` identifier to its generated counterpart, with field differences called out.

- [ ] **Step 1: List what the server actually uses**

```bash
grep -rho 'pkt\.[A-Z][A-Za-z0-9_]*' --include='*.go' . \
  | sort -u | sed 's/pkt\.//' > /tmp/server-types.txt
wc -l /tmp/server-types.txt   # expect 74
```

- [ ] **Step 2: List what the generated package offers**

```bash
grep -o '^func ([A-Za-z0-9_]*) PacketID() int32' \
  ../minecraft-protocol/generated/java/v1_8/packets.go \
  | sed 's/^func (//;s/) PacketID() int32//' | sort > /tmp/generated-types.txt
wc -l /tmp/generated-types.txt
```

- [ ] **Step 3: Pair them by packet ID, not by name**

Names differ; packet IDs do not. Write a throwaway Go program under
`/tmp/mapping` that imports both packages, calls `PacketID()` on a value of
every type in each, and prints the pairs grouped by `(state, direction, id)`.
For the server side, read the ID from its `PacketID()` method the same way.

Direction is implicit in the server's `CB`/`SB` suffix and explicit in the
generated name, so a pair that disagrees on direction is a mapping error to
resolve by hand, not a rename to apply.

- [ ] **Step 4: Diff the fields**

For each pair, print both structs' field names and types with
`reflect.TypeOf`. Record every difference in the mapping document under the
type it affects. These are the places the migration can silently change
behavior, and they are what Task 3 onward must check individually.

- [ ] **Step 5: Write the mapping document**

`docs/migration/2026-08-15-packet-mapping.md` holds one table with columns:
server type, generated type, packet ID, and field differences. Where a server
type has no generated counterpart, say so explicitly and say what happens to
it — the three constants and any type only tests reference are the expected
cases.

- [ ] **Step 6: Commit**

```bash
git add docs/migration/2026-08-15-packet-mapping.md
git commit -m "docs: map the server's packet types onto generated protocol 47"
```

### Task 2: Rehome the three constants

`ProtocolVersion`, `VersionName`, and `MetadataEnd` are not packets and have no
generated equivalent. They must survive the package's deletion.

**Files:**
- Create: `internal/server/protocolinfo/protocolinfo.go`, `internal/server/protocolinfo/protocolinfo_test.go`
- Modify: every file referencing the three constants

**Interfaces:**
- Produces: `protocolinfo.ProtocolVersion`, `protocolinfo.VersionName`, `protocolinfo.MetadataEnd`.

- [ ] **Step 1: Write the failing test**

```go
package protocolinfo_test

import (
	"testing"

	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/server/internal/server/protocolinfo"
)

func TestProtocolVersionMatchesTheGeneratedDescriptor(t *testing.T) {
	if got, want := protocolinfo.ProtocolVersion, v1_8.Version().Protocol; got != want {
		t.Errorf("server advertises protocol %d, generated descriptor says %d", got, want)
	}
}

func TestVersionNameStaysWhatTheServerAdvertised(t *testing.T) {
	// Deliberately not v1_8.Version().Name, which is "1.8.9". Both are
	// protocol 47 and this migration changes no byte on the wire.
	if got := protocolinfo.VersionName; got != "1.8.8" {
		t.Errorf("version name is %q, want 1.8.8", got)
	}
}

func TestMetadataEndIsTheProtocol47Terminator(t *testing.T) {
	if got := protocolinfo.MetadataEnd; got != 0x7F {
		t.Errorf("metadata terminator is 0x%02X, want 0x7F", got)
	}
}
```

- [ ] **Step 2: Run and verify failure**

```bash
devbox run -- task test -- ./internal/server/protocolinfo
```

Expected: FAIL, package does not exist.

- [ ] **Step 3: Implement**

```go
// Package protocolinfo holds the protocol 47 constants that are not packets.
//
// They lived in pkg/gamedata/versions/pc_1_8 beside the generated packet
// structs. M6.1 deletes that package, and these three have no generated
// counterpart: the protocol number is checkable against the descriptor, the
// advertised version name is deliberately different from it, and the metadata
// terminator is a codec detail the server's own entity metadata writer needs.
package protocolinfo

const (
	// ProtocolVersion is Java Edition 1.8's wire protocol number.
	ProtocolVersion int32 = 47

	// VersionName is what the status response advertises.
	//
	// It stays "1.8.8" rather than following minecraft-protocol's "1.8.9".
	// Both are protocol 47, and this migration changes no byte the server
	// puts on the wire. Reconciling the two names is a decision on its own.
	VersionName string = "1.8.8"

	// MetadataEnd terminates an entity metadata list in protocol 47.
	// Protocol 775 terminates at 0xFF instead, which is why this is a
	// version-scoped constant rather than a shared one.
	MetadataEnd byte = 0x7F
)
```

- [ ] **Step 4: Repoint every reference**

```bash
grep -rln 'pkt\.\(ProtocolVersion\|VersionName\|MetadataEnd\)' --include='*.go' .
```

Change each to `protocolinfo.` and add the import. Leave the `pkt` import in
place where the file still uses packet types; Task 3 onward removes those.

- [ ] **Step 5: Run and verify**

```bash
devbox run -- task test
```

Expected: PASS, including every byte-parity fixture.

- [ ] **Step 6: Commit**

```bash
git add internal/server/protocolinfo/ 
git add -u
git commit -m "refactor: move the protocol 47 constants out of the packet package"
```

---

## Stage B — Change how packets are written

### Task 3: Write decoded values instead of raw payloads

This is the task the rest depends on, and the one that fixes M3's recorded
finding: a session proposes a transition only for a packet it can inspect, and
a connection that writes a raw payload gets none.

**Files:**
- Modify: `internal/server/conn/stream.go`
- Modify: `internal/server/conn/stream_test.go`

**Interfaces:**
- Produces: `writePacket(p protocol.Packet) error` taking a packet whose `Value` is a generated type.

- [ ] **Step 1: Write the failing test**

```go
func TestWritePacketHandsTheSessionADecodedValue(t *testing.T) {
	// A packet written with Value set must reach the wire with the same
	// bytes the reflect-codec path produced, and must let the session see
	// what was written.
	c, peer := newTestConnection(t)
	defer peer.Close()

	err := c.writePacket(protocol.Packet{
		State:     v1_8.StatePlay,
		Direction: protocol.DirectionClientbound,
		ID:        v1_8.PlayClientboundKeepAlive{}.PacketID(),
		Value:     &v1_8.PlayClientboundKeepAlive{KeepAliveID: 4242},
	})
	if err != nil {
		t.Fatalf("writePacket: %v", err)
	}

	got := readOneFrame(t, peer)
	want := marshalWithOldCodec(t, &pkt.KeepAliveCB{KeepAliveID: 4242})
	if !bytes.Equal(got, want) {
		t.Errorf("generated encoding differs from the reflect codec:\n got %x\nwant %x", got, want)
	}
}

func TestWritePacketRejectsAPacketWithNoValue(t *testing.T) {
	c, peer := newTestConnection(t)
	defer peer.Close()

	err := c.writePacket(protocol.Packet{
		State:     v1_8.StatePlay,
		Direction: protocol.DirectionClientbound,
		ID:        0x00,
	})
	if err == nil {
		t.Fatal("writePacket accepted a packet with neither Value nor Payload")
	}
}
```

Keep `marshalWithOldCodec` as a test helper that calls `java.Marshal` against
the old struct. It is the bridge that proves the two encodings agree, and it is
deleted in Task 8 along with the package it marshals.

- [ ] **Step 2: Run and verify failure**

```bash
devbox run -- task test -- ./internal/server/conn
```

- [ ] **Step 3: Implement**

Replace `writePacket`'s body. It no longer marshals:

```go
// writePacket sends one clientbound packet.
//
// It hands the stream a decoded value rather than a raw payload, so the
// session encodes it and can inspect it. That is what lets the session
// propose a state transition: M3 left play on raw payloads and mirrored the
// state locally as a result, and this is where that mirror stops being
// necessary.
func (c *Connection) writePacket(p protocol.Packet) error {
	if p.Value == nil && p.Payload == nil {
		return fmt.Errorf("write packet 0x%02X: neither value nor payload", p.ID)
	}

	return c.stream.Write(c.ctx, p)
}
```

Every call site now builds the whole `protocol.Packet`. That is more typing per
call than the old one-argument form, so add a helper beside it:

```go
// send builds and writes one clientbound play packet.
func (c *Connection) send(value protocol.PacketValue) error {
	return c.writePacket(protocol.Packet{
		State:     c.streamState(),
		Direction: protocol.DirectionClientbound,
		ID:        value.PacketID(),
		Value:     value,
	})
}
```

Check the real name of the generated packets' shared interface before writing
`protocol.PacketValue`; if the root package does not declare one, declare a
local `packetValue interface{ PacketID() int32 }` in this file, which is what
`stream.go` already has.

- [ ] **Step 4: Run and verify it passes**

```bash
devbox run -- task test -- ./internal/server/conn
```

Expected: PASS, including the byte-equality test.

- [ ] **Step 5: Commit**

```bash
git add internal/server/conn/stream.go internal/server/conn/stream_test.go
git commit -m "refactor(conn): write decoded packet values instead of raw payloads"
```

---

## Stage C — Retype the call sites

Migrate one area per task. Each is independently reviewable, each ends green,
and each keeps the parity fixtures passing. Work in this order, cheapest and
most isolated first.

### Task 4: `pkg/world` and player metadata

**Files:**
- Modify: `pkg/world/chunk.go`, `internal/server/player/metadata.go`, `internal/server/player/item_entity.go`
- Modify: `internal/server/player/metadata_test.go`

- [ ] **Step 1: Run the parity fixtures and record the baseline**

```bash
devbox run -- task test -- ./internal/server/conn -run Parity -v 2>&1 | tail -20
```

Expected: PASS. Note the count; it must not change.

- [ ] **Step 2: Retype, one type at a time**

For each `pkt.X` in these files, substitute the generated type from Task 1's
mapping and fix the field references the mapping flagged as different. Compile
after each type rather than after each file:

```bash
devbox run -- go build ./... 
```

- [ ] **Step 3: Run and verify**

```bash
devbox run -- task test
```

Expected: PASS, with the same parity count as Step 1.

- [ ] **Step 4: Commit**

```bash
git add -u
git commit -m "refactor(world,player): use generated protocol 47 packets"
```

### Task 5: `internal/server/player` manager and tracking

**Files:**
- Modify: `internal/server/player/manager.go`, `player.go`
- Modify: `internal/server/player/manager_test.go`, `tracking_test.go`

- [ ] **Step 1: Retype the manager**

Same substitution as Task 4. `player.NewPlayer` takes a
`writePacket func(java.PacketValue) error` parameter; change it to the
connection's `send` signature so a player writes generated values too.

- [ ] **Step 2: Retype the tests**

`packetCollector.writePacket` in `manager_test.go` mirrors the production
signature and must change with it.

- [ ] **Step 3: Run and verify**

```bash
devbox run -- task test
```

Expected: PASS, parity count unchanged.

- [ ] **Step 4: Commit**

```bash
git add -u
git commit -m "refactor(player): use generated protocol 47 packets"
```

### Task 6: The connection handlers

The largest area: `handler_play.go`, `handler_handshake.go`,
`handler_status.go`, `inventory.go`, `commands.go`, `tab_complete.go`,
`legacy_ping.go`, and `server.go`.

**Files:**
- Modify: the eight files above and their tests

- [ ] **Step 1: Retype the clientbound writes**

Every `c.writePacket(&pkt.X{...})` becomes `c.send(&v1_8.PlayClientboundX{...})`.

- [ ] **Step 2: Retype the serverbound decodes**

The play read path currently decodes into local structs. The stream already
returns `protocol.Packet` with `Value` populated by the generated session, so
a handler switches on the generated type rather than unmarshalling:

```go
switch value := packet.Value.(type) {
case *v1_8.PlayServerboundPositionLook:
	...
}
```

Delete the per-handler `java.Unmarshal` calls this replaces. M3 warned that
every play packet was being decoded twice and that the generated decode is
strict where the old loop was not — this task is where the second decode goes
away, and where a serverbound packet whose generated model is wrong becomes a
visible failure rather than a silent one.

- [ ] **Step 3: Run and verify**

```bash
devbox run -- task test
```

Expected: PASS, parity count unchanged. A parity failure here is a real
encoding difference; find it before continuing.

- [ ] **Step 4: Commit**

```bash
git add -u
git commit -m "refactor(conn): use generated protocol 47 packets in every handler"
```

### Task 7: Drop the local state mirror

M3 recorded that the connection mirrors the session's state into a local enum,
because a raw-payload write proposed no transition. Task 3 removed that cause.

**Files:**
- Modify: `internal/server/conn/stream.go` and whichever file holds the local state enum

- [ ] **Step 1: Write the failing test**

```go
func TestSessionStateFollowsAWrittenPacket(t *testing.T) {
	// Writing the login success packet must move the session to play
	// without the connection setting the state itself.
	c, peer := newTestConnection(t)
	defer peer.Close()

	if err := c.send(&v1_8.LoginClientboundSuccess{ /* ... */ }); err != nil {
		t.Fatalf("send: %v", err)
	}

	snapshot, err := c.stream.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snapshot.State != v1_8.StatePlay {
		t.Errorf("session is in %q after login success, want play", snapshot.State)
	}
}
```

- [ ] **Step 2: Run and verify failure**

- [ ] **Step 3: Remove the mirror**

Delete the local state field and make `streamState()` read
`stream.Snapshot(ctx).State`. M1 recorded that a running stream owns its
session exclusively and `Snapshot` is the only safe way to observe it, so do
not reach into the session.

`Snapshot` takes a context and can fail, and `streamState()` currently cannot.
Give the connection a cached state updated from the transition it observes,
or change `streamState()` to return an error — the first is simpler and is
what the write path needs, since it already holds `c.ctx`.

- [ ] **Step 4: Run and verify**

```bash
devbox run -- task test
```

Expected: PASS, parity count unchanged.

- [ ] **Step 5: Commit**

```bash
git add -u
git commit -m "refactor(conn): read protocol state from the session, not a local mirror"
```

---

## Stage D — Delete and gate

### Task 8: Delete the packet package and the code generation

**Files:**
- Delete: `pkg/gamedata/`, `cmd/codegen/`, `cmd/dmd/`, and the downloaded schemas they read
- Modify: `Taskfile.yml`

- [ ] **Step 1: Confirm nothing references them**

```bash
grep -rn 'gamedata/versions/pc_1_8' --include='*.go' . ; echo "(empty = clean)"
grep -rn 'cmd/codegen\|cmd/dmd' Taskfile.yml .github/ 2>/dev/null
```

- [ ] **Step 2: Delete**

```bash
git rm -r pkg/gamedata cmd/codegen cmd/dmd
```

Remove the downloaded `protocol.json` and any schema files `cmd/dmd` fetched,
and remove their paths from `.gitignore` if they are listed there.

- [ ] **Step 3: Drop the tasks**

Remove the `codegen` and `dmd` tasks from `Taskfile.yml`, and any task that
depends on them. M3 noted `cmd/dmd` survived only because the retained packet
codegen read what it downloaded; both go together.

- [ ] **Step 4: Remove the test bridge**

Delete `marshalWithOldCodec` from Task 3's test file and the byte-equality test
that used it. It compared the new encoding against a package that no longer
exists; the parity fixtures are what carry that guarantee from here on.

- [ ] **Step 5: Run the full gate**

```bash
devbox run -- task verify
```

Expected: PASS. If `task build` pointed at a directory that is now gone, fix
the task — M3 found and fixed one of those already.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "refactor: delete the server's last wire types and code generation"
```

### Task 9: Documentation, milestone records, and the client check

**Files:**
- Modify: `README.md`, `CHANGELOG.md`, `docs/`, `../headless-minecraft/MASTER_PLAN.md`
- Create: `docs/verification/2026-08-15-m6-1-client-check.md`

- [ ] **Step 1: Document**

Update any document that describes the server as owning packet structs or
running its own code generation. README's development section loses the
codegen step.

- [ ] **Step 2: Run a real client**

The parity fixtures prove the bytes did not change; they do not prove the
strict generated decode accepts everything a real client sends. M3's client
check found zero decode errors for handshake, status, and login. This task's
check covers play, which is the half M3 deliberately left on local structs.

Run a vanilla 1.8.9 client through a full session: join, move, break and place
blocks, open a chest, craft in the 2x2 and 3x3 grids, shift-click the output,
take damage, die, respawn, chat, run a command with tab completion, and
disconnect. Record every decode error, and record zero if there are none.

Write the record to `docs/verification/2026-08-15-m6-1-client-check.md` in the
same shape as M3's.

- [ ] **Step 3: Update the milestone record**

Mark M6.1 complete in `MASTER_PLAN.md`. Record any generated codec that
rejected a real client's packet, because each one is a bug to fix in
`minecraft-protocol`, not in the server.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "docs: record the server play-state migration"
```

---

## Self-review notes

- **Task 1 is not optional overhead.** Seventy-one renames where field names
  also moved is exactly the change that compiles clean and puts the wrong
  number on the wire. The mapping document is what makes Tasks 4 through 6
  reviewable by someone who did not write them.
- **The parity fixtures are checked at every task, not just at the end.** They
  are the only thing standing between this migration and a silent wire change,
  and a failure is much cheaper to localise one task at a time.
- **Task 7 could be deferred.** Removing the state mirror is a cleanup that M3
  explicitly handed to M6, not a prerequisite for deleting the packet package.
  If Task 6 runs long, Task 7 can move to its own change without blocking
  Task 8.
