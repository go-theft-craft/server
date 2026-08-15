# Shared Protocol Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Run the server's connection on `minecraft-protocol`'s managed stream, with generated protocol 47 codecs for handshake, status, ping, and login, working compression, shared encryption and login mechanics, and packet-sending disconnects — without changing gameplay behavior.

**Architecture:** `login.Acceptor` lands first in `minecraft-protocol`, as the server-side counterpart to M2's client negotiator, tested against it over `net.Pipe`. `server` then replaces `Connection`'s blocking read loop and `io.ReadWriter` with a `protocol.Stream`. `Connection.writePacket` keeps its signature so its eighty call sites do not move; its body marshals through `wire/java` and writes an envelope with a raw payload. Handshake, status, and login handlers switch on generated packet values; play handlers keep their local structs and decode from `Packet.Payload`. Game-data registries move to `minecraft-protocol/data` repo-wide.

**Tech Stack:** Go 1.25.2 in `server` and 1.26.5 in `minecraft-protocol`, Devbox, Task, standard library, pinned Node `minecraft-protocol` 1.66.2 as a test client.

## Global Constraints

- Run every command as `devbox run -- task <name>` in the repository being changed. Never call `go` directly.
- `minecraft-protocol` has **no external dependencies** and must still have none. Do not add one for the acceptor.
- Gameplay behavior does not change. Any observable difference in the world, inventory, or player state is a migration bug.
- `Connection.writePacket(p mcnet.Packet) error` keeps its exact signature until M6.
- Do not migrate play packets to generated types. Play keeps its local structs.
- Do not touch `proxy`.
- Never widen a limit or relax a decode check to make a test pass. A generated codec that rejects a real client's packet is a codec bug — fix it in `minecraft-protocol` and record it.
- Never commit a private key, a session token, or a fixture containing one.
- Leave changes uncommitted only when told to. Each task ends with a commit, in the repository it changed.
- Never add the `Co-Authored-By` or `Claude-Session` trailer to a commit message.

## Dependencies

M2.5 and M2 must be complete. M2 supplies the conduit, the encryption control,
`java.ComputeServerHash`, the `Verifier` interface, and the descriptor login
roles the acceptor dispatches on. M2.5 supplies the final shape of the generated
protocol 47 API, which is why it lands before this migration rather than after.

## File Structure

**`minecraft-protocol` — new:**

| File | Responsibility |
| --- | --- |
| `login/acceptor.go` | Server-side login sequence over descriptor roles |
| `login/acceptor_test.go` | Success and every failure mode, against the client negotiator |
| `wire/java/keyexchange.go` | Server-side RSA decrypt and constant-time token compare |
| `wire/java/keyexchange_test.go` | Malformed secret, wrong token, wrong length |

**`server` — new:**

| File | Responsibility |
| --- | --- |
| `internal/server/conn/stream.go` | Stream construction, limits, and the packet-envelope helpers |
| `internal/server/conn/conn_test.go` | End-to-end connection harness over `net.Pipe` |
| `internal/server/conn/parity_test.go` | Byte-parity fixtures captured from the old path |
| `internal/server/conn/legacy_ping.go` | Pre-frame hook and the legacy response |
| `internal/server/conn/verifier.go` | Mojang `hasJoined` as a `login.Verifier` |
| `interop/node_client_test.go` | Pinned Node client against the Go server |
| `interop/node/client.mjs`, `interop/node/package.json` | Loopback-only Node client harness |

**`server` — modified:**

| File | Change |
| --- | --- |
| `go.mod` | `require`/`replace` for `minecraft-protocol` |
| `internal/server/conn/connection.go` | Stream ownership; `rw`, mutex, and `enableEncryption` removed |
| `internal/server/conn/handler_handshake.go` | Generated `HandshakingServerboundSetProtocol` |
| `internal/server/conn/handler_status.go` | Generated status packets |
| `internal/server/conn/handler_login.go` | Delegates to `login.Acceptor` |
| `internal/server/conn/handler_play.go` and other play files | `mcnet.Unmarshal` → `java.Unmarshal` on `Packet.Payload` |
| `internal/server/config/config.go` | `CompressionThreshold` |
| `internal/server/server.go` | Graceful shutdown through the stream |
| all files importing `pkg/gamedata` | `data.Set` |
| `README.md`, `CLAUDE.md`, `Taskfile.yml` | Documentation and tasks |

**`server` — deleted:**

`pkg/protocol/`, `internal/server/conn/cfb8.go`, `internal/server/conn/cfb8_test.go`,
`internal/server/conn/encrypted_conn.go`, the server-hash half of
`internal/server/conn/crypto.go`, `pkg/gamedata/` except
`versions/pc_1_8/packets.go` and its `protocol.go`, the data templates under
`cmd/codegen/`, `cmd/dmd/`, and `scheme/pc-1.8/`.

---

## Stage M3.1 — The shared server-side login half

### Task 1: Server-side key exchange primitives

**Repository:** `minecraft-protocol`

**Files:**
- Create: `wire/java/keyexchange.go`, `wire/java/keyexchange_test.go`

**Interfaces:**
- Produces: `DecryptSharedSecret(*rsa.PrivateKey, []byte) (SharedSecret, error)`, `VerifyToken(*rsa.PrivateKey, expected, encrypted []byte) error`, `ErrVerifyTokenMismatch`.

- [x] **Step 1: Write the failing test**

Generate a key pair in the test with `rsa.GenerateKey`; never load one from
disk. Cover: a secret encrypted by M2's client half decrypts to the same
sixteen bytes; a secret of the wrong length is rejected before it becomes a
`SharedSecret`; ciphertext encrypted under a different key fails; a verify token
that differs in one byte fails with `ErrVerifyTokenMismatch`; a token of the
wrong length fails with the same error and without comparing contents; the
comparison uses `crypto/subtle`.

Assert that the returned `SharedSecret` still redacts itself under `%v`, `%s`,
and `%#v`, so the server-side path cannot leak what the client-side path
protects.

- [x] **Step 2: Run and verify failure**

```bash
devbox run -- task test -- ./wire/java
```

- [x] **Step 3: Implement**

`rsa.DecryptPKCS1v15` for both values, `subtle.ConstantTimeCompare` for the
token, and length checks before either. Return the M2 `SharedSecret` type rather
than a `[]byte`.

- [x] **Step 4: Commit** as `feat(java): add server-side key exchange`.

### Task 2: The login acceptor

**Repository:** `minecraft-protocol`

**Files:**
- Create: `login/acceptor.go`, `login/acceptor_test.go`
- Modify: `login/doc.go`

**Interfaces:**
- Produces: `NewAcceptor(*rsa.PrivateKey, ...AcceptorOption) (*Acceptor, error)`, `(*Acceptor).Accept(context.Context, *protocol.Stream) (Profile, error)`, `WithVerifier(Verifier)`, `WithCompressionThreshold(int)`, `WithServerID(string)`.

- [x] **Step 1: Write the failing test**

Drive the acceptor against M2's client `Negotiator` over `net.Pipe`, with a real
`protocol.Stream` on each side. This is the test that could not exist if the two
halves lived in different repositories, so make it the centrepiece.

Cover: offline login (no verifier) reaching play; online login with a stub
verifier, including the cipher installing on both sides at the same frame
boundary; compression negotiated at a threshold, with a packet above and a
packet below it crossing correctly afterwards; a verifier that rejects,
producing a disconnect packet the client can read before the socket closes; a
malformed username; a verify-token mismatch; a client that disconnects after
login start; and cancellation at every phase.

Assert that the acceptor makes no HTTP request — the `Verifier` is the only
outbound edge, and the test's verifier records that it was called with the hash
the client computed.

- [x] **Step 2: Run and verify failure**

```bash
devbox run -- task test -- ./login
```

- [x] **Step 3: Implement**

Dispatch on the descriptor login roles from M2 Task 7; do not name a generated
packet type. Order matters and is fixed: read login start; if a verifier is
configured, send the encryption request, read the response, decrypt, compare the
token, install the cipher through `Stream.Control`, compute the hash, call
`Verify`; then, if a threshold is configured, send `set_compression`; then send
login success. Return only after the session has reached play.

- [x] **Step 4: Run race and interop gates**

```bash
devbox run -- task test:race -- ./login
devbox run -- task verify
```

- [x] **Step 5: Commit** as `feat(login): add the server-side login acceptor`.

Implemented, with one deviation recorded. The plan asked the acceptor to
dispatch on the descriptor login roles rather than name a generated packet
type. Role dispatch covers only inbound packets: the role table maps a
`(state, direction, id)` triple to a role, and there is no role-based way to
*construct* the clientbound encryption request, set-compression, and success
packets, each of which needs fields set. The acceptor would therefore have
named protocol 47 types for its outbound half regardless, leaving it
protocol-47-bound while paying for a lookup that buys nothing. It dispatches on
concrete generated types, exactly as the client negotiator already does, and
M2's recorded decision stands unchanged: the `login` package is protocol 47
only, and M4 parameterizes both halves together.

Two things found while implementing:

- The acceptor installs the cipher before it calls the verifier, not after.
  The exchange is complete by then, and calling a session server first would
  hold the connection in plaintext for the length of an outbound HTTP request.
- Offline logins need a UUID and the acceptor is the only thing holding the
  name at that point, so `login.OfflineUUID` derives the vanilla identity
  (version 3 over `OfflinePlayer:<name>`). It is byte-identical to the server's
  existing `offlineUUID`, which is what keeps Task 7's UUID-formatting
  assertion true.

---

## Stage M3.2 — Pin the server's current behavior

### Task 3: A connection harness and byte-parity fixtures

**Repository:** `server`

**Files:**
- Create: `internal/server/conn/conn_test.go`, `internal/server/conn/parity_test.go`
- Create: `internal/server/conn/testdata/*.bin`

**Interfaces:**
- Produces: no production code. This task describes what the server does today so the migration can prove it still does it.

- [x] **Step 1: Build the harness against the old code**

A test that drives a `Connection` over `net.Pipe` with a scripted client:
handshake into status, status request, ping, and a legacy-ping attempt; and
handshake into login, offline login start, through to the first play packet.
Assert the packet IDs and payload bytes the server writes.

- [x] **Step 2: Capture the fixtures**

Write the exact bytes for the status response, ping response, login success,
encryption request, and both disconnect forms into `testdata`. Record the
current legacy-ping behavior too — today that is a framing error, and the
fixture should say so, because Task 9 changes it deliberately.

- [x] **Step 3: Run and verify they pass**

```bash
devbox run -- task test -- ./internal/server/conn/...
```

Expected: green against the unmigrated server. A failure here is a bug that
predates this plan; fix or record it before continuing.

- [x] **Step 4: Commit** as `test(conn): pin the current connection behavior`.

---

## Stage M3.3 — The migration

### Task 4: Add the dependency and migrate game data

**Repository:** `server`

**Files:**
- Modify: `go.mod`, and every file importing `pkg/gamedata`
- Delete: `pkg/gamedata/` except `versions/pc_1_8/packets.go` and `versions/pc_1_8/protocol.go`; the data templates in `cmd/codegen/`; `cmd/dmd/`; `scheme/pc-1.8/`
- Modify: `Taskfile.yml`, `Taskfile.codegen.yml`

**Interfaces:**
- Produces: `data.Set` in place of `gamedata.GameData`, from `v1_8.Data()`.

- [x] **Step 1: Add the dependency**

```go
require github.com/go-theft-craft/minecraft-protocol v0.0.0

replace github.com/go-theft-craft/minecraft-protocol => ../minecraft-protocol
```

- [x] **Step 2: Swap the registries**

Eighteen call sites. Rename `gamedata.GameData` to `data.Set` and source it from
`v1_8.Data()`. Where a registry accessor's name or return type differs, adapt
the caller; do not add a compatibility shim, because a shim would survive into
M6.

- [x] **Step 3: Keep the packet structs**

`pkg/gamedata/versions/pc_1_8/packets.go` and its `protocol.go` stay, with a
package comment stating that they are the last local wire types and that M6
deletes them. Reduce `cmd/codegen` to packet generation only.

- [x] **Step 4: Verify**

```bash
devbox run -- task deps
devbox run -- task lint
devbox run -- task test
devbox run -- task build
```

Expected: everything passes, gameplay tests unchanged, and
`grep -rn 'server/pkg/gamedata' --include='*.go' .` matches only the retained
packet package.

- [x] **Step 5: Commit** as `refactor(data): source game data from minecraft-protocol`.

### Task 5: The stream replaces the read loop

**Repository:** `server`

**Files:**
- Create: `internal/server/conn/stream.go`
- Modify: `internal/server/conn/connection.go`
- Delete: `internal/server/conn/cfb8.go`, `cfb8_test.go`, `encrypted_conn.go`

**Interfaces:**
- Produces: `Connection.stream *protocol.Stream`; `writePacket` with an unchanged signature; `readPacket(ctx) (protocol.Packet, error)`.

- [x] **Step 1: Write the failing test**

Extend the Task 3 harness: the connection reads and writes through a stream; a
frame above the configured limit is refused with a named error rather than an
`OOM`; two goroutines writing concurrently produce two intact frames with no
interleaving; closing the client end unblocks `Handle` and runs its deferred
save exactly once.

- [x] **Step 2: Run and verify failure**

- [x] **Step 3: Implement**

`NewConnection` builds `protocol.NewStream(session, transport)` with limits from
config. `Handle` starts it and loops on `Stream.Read`. `writePacket` becomes:

```go
func (c *Connection) writePacket(p mcnet.Packet) error {
	payload, err := java.Marshal(p, c.limits)
	if err != nil {
		return fmt.Errorf("marshal packet 0x%02X: %w", p.PacketID(), err)
	}
	return c.stream.Write(c.ctx, protocol.Packet{
		State:     c.streamState(),
		Direction: protocol.DirectionClientbound,
		ID:        p.PacketID(),
		Payload:   payload,
	})
}
```

Delete the mutex: `Stream.Write` serializes through the write pump. Delete
`enableEncryption` and both cipher files.

- [x] **Step 4: Verify the fixtures still match**

```bash
devbox run -- task test -- ./internal/server/conn/...
```

Expected: every byte-parity fixture from Task 3 still passes. The state machine
still routes by `c.state` at this point; only the transport changed.

- [x] **Step 5: Commit** as `refactor(conn): run connections on the managed stream`.

### Task 6: Handshake, status, and ping on generated packets

**Repository:** `server`

**Files:**
- Modify: `internal/server/conn/handler_handshake.go`, `internal/server/conn/handler_status.go`, `internal/server/conn/connection.go`

**Interfaces:**
- Produces: handlers that switch on generated values and let the session own state transitions.

- [x] **Step 1: Write the failing test**

The handshake's next-state field drives the session transition rather than
`c.state`; an invalid next state disconnects with a reason instead of returning
a bare error; the status response JSON is byte-identical to the fixture; a ping
echoes its payload; a status request in the wrong state is refused.

- [x] **Step 2: Run and verify failure**

- [x] **Step 3: Implement**

Use `HandshakingServerboundSetProtocol`, `StatusClientboundServerInfo`,
`StatusServerboundPing`, and `StatusClientboundPing`. Let the session propose
the handshake transition and the stream commit it; delete the local `State` enum
once nothing reads it.

- [x] **Step 4: Commit** as `feat(conn): serve handshake and status from generated packets`.

### Task 7: Login through the acceptor

**Repository:** `server`

**Files:**
- Modify: `internal/server/conn/handler_login.go`, `internal/server/conn/crypto.go`, `internal/server/config/config.go`
- Create: `internal/server/conn/verifier.go`

**Interfaces:**
- Produces: `mojangVerifier` implementing `login.Verifier`; `config.CompressionThreshold`; a login handler that delegates to `login.Acceptor`.

- [x] **Step 1: Write the failing test**

Offline login reaches play and writes the fixture's login-success bytes; online
login with a stub verifier reaches play with the cipher installed; a verifier
error disconnects with a readable reason; the skin fetch failing does not fail
the login; the profile's UUID formatting is unchanged from the old path,
asserted against a known username.

- [x] **Step 2: Run and verify failure**

- [x] **Step 3: Implement**

`handleLogin` calls `Accept` and then `startPlay` with the returned profile.
Move the Mojang call into `verifier.go` behind `login.Verifier`, keeping its
existing HTTP behavior and error text. Delete `minecraftSHA1HexDigest` and the
local RSA handling; both now live in `wire/java`.

Add `CompressionThreshold` to config, defaulting to 256, and pass it to the
acceptor. `-1` disables compression.

- [x] **Step 4: Verify compression end to end**

Add a test that logs in with the threshold at 16, then exchanges a packet above
and a packet below it, asserting both arrive intact.

- [x] **Step 5: Commit** as `feat(conn): accept logins through the shared acceptor`.

### Task 8: Play on raw payloads

**Repository:** `server`

**Files:**
- Modify: `internal/server/conn/handler_play.go`, `commands.go`, `inventory.go`, `mining.go`, `crafting.go`, `slot.go`, `tab_complete.go`, and the `player` files using `mcnet`
- Delete: `pkg/protocol/`

**Interfaces:**
- Produces: play handlers reading `Packet.Payload` through `java.Unmarshal`, with their local structs unchanged.

- [x] **Step 1: Write the failing test**

A play packet decodes into the same local struct as before, from the same bytes;
an unknown play packet ID is ignored exactly as it is today; a truncated play
packet produces an error naming the packet rather than a panic.

- [x] **Step 2: Run and verify failure**

- [x] **Step 3: Migrate the call sites**

`mcnet.Unmarshal(data, &p)` becomes `java.Unmarshal(packet.Payload, &p, limits)`
and `mcnet.Marshal` becomes `java.Marshal`. The structs and the `mc` tags do not
change, because the shared reflect codec reads the same tag.

- [x] **Step 4: Delete the local wire package**

```bash
rg -n 'server/pkg/protocol' --glob '*.go'
```

Expected: no matches. Then delete `pkg/protocol/`.

- [x] **Step 5: Verify**

```bash
devbox run -- task lint
devbox run -- task test
devbox run -- task build
```

- [x] **Step 6: Commit** as `refactor(conn): decode play packets with the shared codec`.

### Task 9: Legacy ping and graceful disconnect

**Repository:** `server`

**Files:**
- Create: `internal/server/conn/legacy_ping.go`
- Modify: `internal/server/conn/connection.go`, `internal/server/server.go`

**Interfaces:**
- Produces: a `PreFrameHook` answering `FE 01`; `disconnect` built on `Stream.Shutdown`.

- [x] **Step 1: Write the failing test**

A legacy `FE 01` probe receives a well-formed legacy response and the connection
closes cleanly — replacing the Task 3 fixture that recorded a framing error, with
the fixture updated in the same commit so the change is visible in review. A
kicked player receives a login-state or play-state disconnect packet, matching
the current state, before the socket closes. Server shutdown disconnects every
connected player with a reason rather than dropping sockets.

- [x] **Step 2: Run and verify failure**

- [x] **Step 3: Implement**

Install the pre-frame hook with the legacy response format. Replace
`disconnect`'s body with `Stream.Shutdown(ctx, reason)`. In `server.go`, shut
connections down on context cancellation before closing the listener.

- [x] **Step 4: Commit** as `feat(conn): answer legacy pings and disconnect gracefully`.

---

## Stage M3.4 — Proof against real clients

### Task 10: The Node client lane

**Repository:** `server`

**Files:**
- Create: `interop/node/client.mjs`, `interop/node/package.json`, `interop/node_client_test.go`
- Modify: `Taskfile.yml`

**Interfaces:**
- Produces: `task test:interop`, a loopback-only Node client harness.

- [x] **Step 1: Write the failing test**

Start the Go server on an ephemeral loopback port, run the pinned Node client at
1.8.8 in offline mode, and assert it reaches play and receives a chunk. Run it
twice: once with compression disabled and once at threshold 256. The harness
binds and dials loopback only, and never contacts a session server.

- [x] **Step 2: Run and verify failure**

```bash
devbox run -- task test:interop
```

- [x] **Step 3: Implement and pin**

Pin `minecraft-protocol` at 1.66.2, the same version `minecraft-protocol`'s own
lane uses, so the two repositories agree on what they are testing against.

- [x] **Step 4: Commit** as `test(interop): verify the server against the pinned Node client`.

### Task 11: Real clients, recorded

**Repository:** `server`

**Files:**
- Create: `docs/verification/2026-08-15-m3-client-checks.md`

- [x] **Step 1: Vanilla client, offline mode**

Connect a real 1.8.9 client and run a full session: join, move, break a block,
place a block, open a chest, move an item, chat, take damage, die, respawn,
disconnect. Record the client build and every decode error observed.

Any generated-codec decode failure is a bug in `minecraft-protocol`: record the
packet, fix the codec there, add a byte fixture, and re-run. Do not relax the
check in `server`.

- [x] **Step 2: Vanilla client, online mode**

One authenticated login against the real session server, with compression on.
Record the result. This is the only proof the acceptor's server hash and
verify-token handling are right, and it cannot run in CI.

- [x] **Step 3: Compression on and off**

Repeat the offline join at thresholds `-1`, `256`, and `1`, confirming a client
joins in all three.

Done. `-1`, `256`, and `1` all reach play: `-1` sends no `set_compression` at
all, `1` compresses nearly every packet. `256` was confirmed with the vanilla
client, the other two with the pinned Node client.

- [x] **Step 4: Commit** as `docs: record M3 client verification`.

### Task 12: Documentation and milestone records

**Repositories:** `server`, `headless-minecraft`

**Files:**
- Modify: `server/README.md`, `server/CLAUDE.md`, `../headless-minecraft/MASTER_PLAN.md`, `../minecraft-protocol/CHANGELOG.md`

- [x] **Step 1: Fix the stale guidance**

`server/CLAUDE.md` currently claims `internal/` is empty and the build is
vendored. Neither is true. Rewrite the architecture and commands sections to
match the repository, and document that the protocol and game data come from
`minecraft-protocol`.

- [x] **Step 2: Record the milestone**

`MASTER_PLAN.md`: M3 complete, with what the real-client sessions found — every
mis-modelled packet fixed in `minecraft-protocol`, and the online-mode login
result.

- [x] **Step 3: Run every gate**

```bash
cd ../minecraft-protocol && devbox run -- task verify
cd ../server && devbox run -- task lint && devbox run -- task test && devbox run -- task test:interop && devbox run -- task build
```

- [x] **Step 4: Inspect final scope**

`git status --short` in both repositories. Confirm: `pkg/protocol` and the
server's cipher files are gone; `pkg/gamedata` retains only the packet structs;
no play packet uses a generated type; `minecraft-protocol`'s `go.mod` still has
no `require` block.

- [x] **Step 5: Commit** as `docs: record the shared protocol migration`.

---

## Outcome

Tasks 1 through 12 are implemented. Both vanilla client sessions ran on
2026-08-15 with **zero decode errors** — no generated codec rejected a packet
the real 1.8.9 client sent, in offline or online mode. The online login proved
the server hash and verify-token handling against the real session server,
which no automated test can do.

Open before M3 is called complete:

- The 2x2 crafting question in
  [the session findings](../../verification/2026-08-15-m3-session-findings.md),
  which is the one gameplay problem that could have been caused by Task 4.

Two defects were found by running the server rather than by testing it: normal
disconnects logged at ERROR, fixed in this milestone; and a survival block
duplication whose cause is not the migrated drop data, recorded for later.
