# Server protocol migration design

- Status: Draft for review
- Date: 2026-08-15
- Repositories: `server`, `minecraft-protocol`
- Milestone: M3

## Context

M0 produced reflection-free protocol 47 codecs and immutable game data. M1
produced the managed stream: bounded framing, compression, ordered runtime
transitions, graceful disconnect, and lossless observation points. M2 adds
AES-CFB8 below framing and the client login sequence.

Nothing outside `minecraft-protocol` uses any of it. The `server` repository
still carries a complete parallel implementation: its own framing in
`pkg/protocol`, its own AES-CFB8 in `internal/server/conn/cfb8.go`, its own
`net.Conn` wrapper in `encrypted_conn.go`, and its own generated packet structs
in `pkg/gamedata/versions/pc_1_8/packets.go` produced by `cmd/codegen`.

M3 is the milestone that proves the shared library against a real connection.
Until one consumer runs on it, the library's contracts are untested claims and
every defect found later is found in three places at once.

The server is also further behind than the tracker implied. It has no
compression at any threshold, and it does not answer the legacy `FE 01` server
list ping. Both arrive with the migration rather than as separate features.

## Scope change to record

`MASTER_PLAN.md` scoped M3 to handshake, status, ping, login, disconnect,
compression, and online/offline mode, and deliberately left play-state
migration to M6. That boundary is not taken.

The reason is that the managed stream owns the socket. Once it does, it owns
play frames too, and keeping the existing play handlers on the old packet types
would require either a passthrough session that hides play packets from the
session, or a translation layer between the two generated packages. Both are
built in M3 and deleted in M6.

M3 therefore migrates the whole server. M6 keeps the proxy wire migration and
connecting `headless-minecraft` to the current Java profile. `MASTER_PLAN.md`
records the move rather than silently reinterpreting either milestone.

## Goals

- Replace the server's framing, encryption, and packet types with
  `minecraft-protocol`.
- Add a reusable server-side login negotiator upstream, so `server` and later
  `proxy` share one tested sequence.
- Add compression and the legacy `FE 01` ping, which the server lacks.
- Delete `pkg/protocol`, `conn/cfb8.go`, `conn/encrypted_conn.go`,
  `conn/crypto.go`'s cipher half, and packet generation from `cmd/codegen`.
- Prove byte-level parity before any of that is deleted.

## Non-goals

- Registry unification. `pkg/gamedata` and its `cmd/codegen` registry output
  stay. `pkg/world`, `pkg/world/gen`, and `internal/server/player` depend on
  `gamedata.GameData`, and moving them to `data.Set` is a separate milestone
  with its own risk.
- The `proxy` repository.
- `headless-minecraft`.
- Protocol 775 and the configuration state.
- Gameplay behavior change. A migrated server does what the current one does.
  Four externally visible differences are intended and each is called out
  below: compression, the legacy `FE 01` ping, a disconnect packet that is
  actually sent, and tolerance of unrecognized packet IDs.

## Prerequisites

M2 must be implemented **and pushed**. The server pins a pseudo-version the way
`headless-minecraft` already does:

```
require github.com/go-theft-craft/minecraft-protocol v0.0.0-<commit>
```

`minecraft-protocol` declares `go 1.26.5` and `server` declares `go 1.25.2`, so
M3 bumps the server toolchain. The server vendors its dependencies;
`minecraft-protocol` has no external dependencies of its own, so
`go mod vendor` adds one tree and nothing else.

## Stages

| Stage | Deliverable | Gate |
| --- | --- | --- |
| M3.1 | `login.ServerNegotiator`, `Verifier`, and an outbound envelope helper in `minecraft-protocol` | Server-role login passes the Node interoperability lane, encrypted and plain |
| M3.2 | Dependency pin, toolchain bump, parity fixtures captured from the current code | Golden bytes exist for every path before anything is deleted |
| M3.3 | `Connection` owns a `protocol.Stream`; handshake, status, ping, legacy `FE 01` | A real client sees an unchanged server list entry |
| M3.4 | Login: offline, online, encryption, compression, disconnect | Both modes reach play, compression on and off |
| M3.5 | Play migration across `conn/`, `player/`, `world/` | Full play parity against the M3.2 fixtures |
| M3.6 | Deletions and release gate | All three verification lanes green, worktree clean |

M3.2 before M3.3 is a hard ordering constraint. The golden bytes can only be
captured from code that still exists, so fixtures are recorded before the first
line of the old encoder is removed.

## Upstream additions (M3.1)

### Server negotiator

M2 declares `login.Verifier` and implements only the client `Negotiator`. Its
tests drive the server half by hand. M3.1 promotes that hand-rolled sequence
into `login/server.go`:

```go
// ServerNegotiator runs the server half of the protocol 47 login sequence.
// It owns inbound delivery for the duration of the login, exactly as the
// client Negotiator does.
type ServerNegotiator struct { ... }

func NewServerNegotiator(key ServerKey, verifier Verifier, options ...ServerOption) (*ServerNegotiator, error)

func (n *ServerNegotiator) Negotiate(ctx context.Context, stream *protocol.Stream) (Profile, error)
```

Given a running stream it reads `LoginStart`; in offline mode it derives the
version 3 UUID from `OfflinePlayer:<name>` and writes `Success`; in online mode
it generates a verify token, writes `EncryptionBegin`, reads the response,
decrypts the secret and token with PKCS#1 v1.5, compares the token in constant
time, installs the cipher through `Stream.Control`, computes the server hash,
calls `Verifier.Verify`, and writes `Success`.

Compression is a `ServerOption` holding a threshold. When it is non-negative
the negotiator writes `LoginClientboundCompress` before `Success`. It calls no
control for either step: the generated session already proposes the compression
control from the outbound `Compress` packet and the login-to-play state change
from the outbound `Success`, and the stream commits both.

The negotiator makes no network call. `Verifier` is the seam, and the server
supplies the Mojang `hasJoined` implementation that lives in `conn/crypto.go`
today.

The trade this accepts: an upstream API designed against one real caller. The
proxy is the intended second caller but is several milestones away and its
shape is unproven, so the risk is a `ServerOption` set that fits the server and
has to grow later. The alternative, writing the sequence in `server` and
extracting it when the proxy arrives, was considered and rejected in favor of
one tested implementation from the start.

### Outbound envelope helper

`protocol.Packet` is an envelope, not an interface:

```go
type Packet struct {
	State     State
	Direction Direction
	ID        int32
	Name      string
	Value     any
	Payload   []byte
}
```

`EncodeFrame` rejects a mismatch between the envelope's state, direction, and
ID and the value's own registration. Every server write must therefore build a
correct envelope. Doing that at each call site means either repeating three
fields or reading `Stream.Snapshot` per write, which is a coordinator round
trip on the play hot path.

The generated package already holds the registry that answers this. M3.1 adds
a generated helper, which means a change to
`internal/codegen/generator/templates/protocol.go.tmpl` and regenerated output:

```go
// Envelope returns the packet envelope for a generated value, taking state,
// direction, and ID from the value's registration.
func Envelope(value any) (protocol.Packet, error)
```

A one-line `Connection.send(ctx, value)` wrapper calls `Envelope` and then
`Stream.Write`, propagating either error. A wrong state or direction becomes a
registration lookup failure rather than an encoder error at run time.

## Connection architecture

`Connection` keeps its lifecycle role and loses its transport.

### Reading

Today `handleNextPacket` reads `(packetID int32, data []byte)` from
`mcnet.ReadRawPacket`, switches on a hand-rolled `State` enum, and each handler
switches again on the raw ID before unmarshalling. That collapses into one
loop over `stream.Read(ctx)` and a type switch on `packet.Value`, because the
session has already decoded the body against the correct state and direction.

`Connection.state` is deleted. The session owns protocol state. Where the
server needs to know it, it reads `stream.Snapshot`.

An unrecognized ID no longer terminates the connection with
`unexpected packet 0x%02X`. It decodes to `protocol.UnknownPacket`, and the
default branch of the type switch logs and continues. This is a deliberate
behavior change, and it is the one exception to the no-behavior-change rule.

### Writing

`writePacket` takes `c.mu` and writes to `c.rw`. It becomes
`stream.Write(ctx, envelope)`. The stream's coordinator serializes writes
already, so `Connection.mu` and the whole write-lock discipline are deleted.
`player.Manager` broadcasts, which today reach `writePacket` from other
goroutines, stay correct without change.

### Encryption

`enableEncryption` builds an `encryptedConn` and swaps `c.rw` in place. That
only works because reads are synchronous and no read is in flight. Under
asynchronous pumps it is a data race and the switch point is undefined.

The migrated path installs the cipher through `Stream.Control` with M2's
`TransportControl`, which the conduit applies at a frame boundary while the
read pump is blocked. The server does not call it directly; `ServerNegotiator`
does, at the one correct moment between reading `EncryptionResponse` and
writing `Success`.

`conn/cfb8.go`, `conn/cfb8_test.go`, and `conn/encrypted_conn.go` are deleted.
`conn/crypto.go` keeps the Mojang `hasJoined` call and the skin fetch, and
loses `minecraftSHA1HexDigest` to `wire/java.ComputeServerHash`.

### Compression

New capability. `config.Config` gains:

```go
CompressionThreshold int `json:"compression_threshold"` // -1 disables
```

The default is 256, matching vanilla. The value is passed to
`ServerNegotiator` as an option, and a negative value means the negotiator
writes no `Compress` packet at all rather than writing a disabling one.

### Legacy ping

New capability for the server, but not new code: M1 already shipped
`java.NewLegacyPingHook`, which declines a non-legacy connection without
consuming a byte and answers a matching one with a UTF-16BE `FF` response. The
server supplies a `java.LegacyStatusHandler` reading its own MOTD, player
count, and maximum, and installs the result through
`protocol.WithPreFrameHook`.

The server has a `LegacyServerListPing` type in its generated packets and no
code that ever handles it, so today a legacy client sees a dropped socket.

### Disconnect

`Connection.disconnect` logs a reason and cancels the context. It never sends a
packet, so a disconnecting client sees a dropped socket. It becomes
`stream.Shutdown(ctx, reason)`, which builds the state-appropriate disconnect
packet, drains accepted writes, and then terminates. The login failure path in
`handleEncryptionResponse`, which writes a `Disconnect` packet by hand before
cancelling, folds into the same call.

## What the compiler will not catch

The two generated packages disagree on naming (`AbilitiesCB` against
`PlayClientboundAbilities`) and on mechanism (`mc:"varint"` struct tags read by
reflection against generated encode and decode methods). The rename touches
`conn/handler_play.go`, `conn/inventory.go`, `conn/commands.go`,
`conn/crafting.go`, `conn/mining.go`, `conn/tab_complete.go`,
`player/metadata.go`, `player/manager.go`, `player/item_entity.go`,
`world/chunk.go`, and `internal/server/server.go`. Almost all of it is
mechanical and the compiler finds it.

The rename is not uniform, and this is the largest discovered risk in the
milestone. The server's `cmd/codegen` gave up on every packet containing a
slot, entity metadata, or a tagged union, and emitted a single opaque field
instead:

```go
type SetSlot struct{ Data []byte }              // server
type PlayClientboundSetSlot struct {            // minecraft-protocol
	WindowID int8
	Slot     int16
	Item     java.Slot
}
```

Thirty-one of the server's 112 packet types are these opaque blobs, and ten of
them are in use: `EntityDestroy`, `EntityEquipment`, `EntityMetadata`,
`NamedEntitySpawn`, `PlayerInfo`, `SetSlot`, `SpawnEntity`, `TabCompleteCB`,
`WindowItems`, and `WorldParticles`. For those ten the migration is not a
rename. Hand-written byte assembly in `player/metadata.go`,
`player/manager.go`, `player/item_entity.go`, `conn/inventory.go`, and
`conn/tab_complete.go` is replaced by typed field assignment against
`java.Slot`, `java.EntityMetadata`, and the generated item structs.

Two consequences follow. The typed encoders are stricter than the blob
writers, so latent bugs surface as compile or encode failures rather than
staying invisible: `conn/slot.go` currently reads an item's NBT tag byte and
discards the rest of the payload with `io.ReadAll`, which `java.Slot`'s
`NBT *NBT` field makes impossible to keep. And the remaining 101 packets,
including the positional `MapChunk` and `MapChunkBulk` payloads that
`world/chunk.go` builds, keep identical field names and are a true rename.

The ten blob packets get fixtures first in M3.2 and their own tasks in M3.5.

## Verification

Three lanes, all required before the M3.6 deletions.

### Byte fixture parity

M3.2 captures golden bytes from the current encoder into
`internal/server/conn/testdata/parity/`: handshake, status response, ping,
legacy ping, offline login, online login, disconnect, and a representative play
set covering join, chunk, metadata, slot, window, and command completion. The
migrated encoder must reproduce each byte for byte.

Where the fixtures disagree the current server is not automatically right. A
disagreement is investigated against the protocol 47 descriptor and the Node
implementation, and the losing side is fixed. Any place the current server was
wrong is recorded in the implementation plan rather than quietly matched.

### Node interoperability

The pinned Node `minecraft-protocol` 1.66.2 harness that `minecraft-protocol`
already runs in `interop/` is pointed at the Go server. Scenarios: status,
legacy `FE 01` ping, offline login through to play join, encrypted login with
the `yggdrasil` stub, and compression at threshold 256 and disabled.

### Vanilla 1.8.9 client

The only lane that covers what neither fixtures nor Node model. A scripted
checklist run against a real client: the server appears in the multiplayer
list, join renders chunks, walk, break a block, place a block, open a chest and
move an item, run a command, tab-complete, and disconnect with a visible
reason.

Including play in M3 means this lane cannot run end to end until M3.5. M3.3 and
M3.4 are the last stages where a real client confirms anything incrementally,
so both end with a partial run of it: server list entry at M3.3, join at M3.4.

## Completion criteria

- `pkg/protocol`, `conn/cfb8.go`, `conn/encrypted_conn.go`, and packet
  generation in `cmd/codegen` are deleted, and no server file encodes a frame.
- Every packet the server reads or writes is a `minecraft-protocol` generated
  type carried in a `protocol.Packet` envelope.
- Offline and online login both reach play, with and without compression and
  with and without encryption.
- The legacy `FE 01` ping answers, and a non-legacy connection is unaffected by
  the hook.
- A graceful shutdown sends a disconnect packet the client displays.
- All three verification lanes pass, along with format, lint, race tests, and
  build.
- `MASTER_PLAN.md` records M3 complete and moves the server half of M6 into M3.
- `ROADMAP.md` and `CHANGELOG.md` record compression and legacy ping as new
  server capabilities.
