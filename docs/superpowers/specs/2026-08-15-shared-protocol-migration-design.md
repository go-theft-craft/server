# Shared protocol migration design

- Status: Draft for review
- Date: 2026-08-15
- Repositories: `server`, `minecraft-protocol`
- Milestone: M3

## Context

`minecraft-protocol` has a managed stream, bounded framing, compression,
AES-CFB8 encryption, a client login negotiator, and reflection-free generated
protocol 47 codecs. Nothing outside its own tests uses any of it.

`server` has its own version of most of that, written earlier and by hand:

| Concern | `server` today | `minecraft-protocol` |
| --- | --- | --- |
| Framing | `pkg/protocol.ReadRawPacket`, one 2 MiB cap | `Framer`, full `Limits` |
| Compression | **none** | Bounded envelope, runtime transition |
| Encryption | `conn/cfb8.go`, `conn/encrypted_conn.go` | Conduit below framing |
| Packet coding | `reflect` over `mc` tags | Generated codecs, no reflection |
| Login | `conn/handler_login.go` | Client negotiator, `Verifier` interface |
| Server hash | `conn/crypto.go` | `java.ComputeServerHash` |
| Read loop | Blocking, one goroutine, `c.rw` swapped on encrypt | Read and write pumps, ordered transitions |
| Disconnect | Cancel the context | State-appropriate packet, then drain |

M3 makes the server the first real consumer. It is the milestone that finds out
whether the shared library's contracts survive contact with a program that was
not written against them.

## Goals

- The server's connection runs on `protocol.Stream` from the first byte.
- Handshake, status, ping, and login use generated protocol 47 packets.
- Compression works, in both directions, and is on by default.
- Online-mode login runs through a shared server-side acceptor, with the Mojang
  session call still owned by `server`.
- Disconnects send the state-appropriate packet before the socket closes.
- The server's game-data registries come from `minecraft-protocol/data`.
- `server/pkg/protocol`, `conn/cfb8.go`, `conn/encrypted_conn.go`, and the
  hand-written server hash are deleted.
- A real vanilla 1.8.9 client and the pinned Node client both reach play.

## Non-goals

- Migrating play packets to generated types. Play keeps its local structs and
  moves to `java.Marshal`, which is a one-line change per call site. M6 replaces
  the structs.
- `proxy`. It moves in M6.
- Protocol 775. The server speaks 47 in this milestone and gains a second
  version in M6.
- Gameplay behavior of any kind. If a block breaks differently after this
  milestone, that is a bug in the migration.

## Decision 1: the stream owns the transport, immediately

`Connection` holds a `*protocol.Stream` built in `NewConnection` and started in
`Handle`. The `rw io.ReadWriter` field, `enableEncryption`, `encrypted_conn.go`,
and `cfb8.go` are deleted. Encryption arrives as an `EncryptionControl` through
`Stream.Control`, which is where M2 put it.

The alternative — using the session and framer synchronously inside the existing
blocking loop — keeps the diff smaller and was rejected. It would leave the
concurrent path with no consumer until M6, which means the first program to
depend on the read and write pumps, the transition coordinator, and graceful
shutdown would be a headless client with no existing test suite to compare
against. The server has a working world, a real client, and a body of behavior
that will notice if something is wrong. That is the better place to find out.

## Decision 2: `writePacket` keeps its signature

Eighty call sites write packets, from the connection goroutine, the keep-alive
ticker, and player broadcasts. They all go through:

```go
func (c *Connection) writePacket(p mcnet.Packet) error
```

The body is replaced; the signature is not. It marshals through
`java.Marshal(p, limits)` and hands the result to `Stream.Write` as a packet
envelope with a nil `Value` and a set `Payload`, which the generated session
encodes as-is. The mutex goes away, because `Stream.Write` is already safe for
concurrent use and serializes through the write pump.

Eighty untouched call sites is the difference between a migration that can be
reviewed and one that cannot.

## Decision 3: play reads raw payloads

Inbound, the stream decodes every packet into a generated value and also retains
the raw bytes in `Packet.Payload`. Handshake, status, and login handlers switch
on the generated types. Play handlers keep their local structs and decode from
`Payload` with `java.Unmarshal`, which reads the same `mc` tags the server's own
codec did.

This has a consequence worth stating plainly: **every play packet is now decoded
twice, and the first decode is strict.** The generated codec runs
`RequireEmpty`, so a serverbound play packet whose generated model is wrong
becomes a decode error that terminates the connection, where the old loop would
have handed the bytes to a handler that ignored the trailing garbage.

That is the correct behavior and it is also a new failure mode on day one. The
verification plan therefore includes a full play session from a real client —
movement, block break, block place, inventory, chat, respawn — with any decode
failure treated as a codec bug to fix in `minecraft-protocol`, not as a reason
to loosen the check.

## Decision 4: the server half of login is shared

M2 defines `login.Verifier` and implements only the client `Negotiator`. M3 adds
`login.Acceptor` beside it, in `minecraft-protocol`, driven by the same
descriptor login roles:

```go
// Acceptor runs the server half of the login sequence.
type Acceptor struct{ /* key pair, verifier, compression threshold */ }

func NewAcceptor(key *rsa.PrivateKey, opts ...AcceptorOption) (*Acceptor, error)
func (*Acceptor) Accept(ctx context.Context, stream *protocol.Stream) (Profile, error)
```

`Accept` reads login start, and in online mode generates a verify token, sends
the encryption request, reads the response, decrypts the secret and the token,
compares the token in constant time, installs the cipher, computes the server
hash, calls the `Verifier`, sends `set_compression` if a threshold is
configured, and sends login success.

The Mojang `hasJoined` call stays in `server`, as a `Verifier` implementation.
The library keeps doing cryptography and packet mechanics and keeps making no
HTTP requests, which is the boundary M2 drew.

Putting the acceptor in `minecraft-protocol` also means the encryption handshake
gets tested from both ends in one repository, over `net.Pipe`, with the client
negotiator on the other side. That test cannot exist if the two halves live in
different repositories.

## Decision 5: compression is on, at 256 bytes

The server sends `set_compression` during login with a configurable threshold,
defaulting to 256 to match vanilla. The generated session already proposes the
compression transition when that packet crosses the wire; the acceptor writes
the packet and the stream applies the change at the right frame boundary.

A threshold of `-1` disables it, and the configuration accepts that. The default
is on because the milestone's purpose is to prove the shared implementation
works, and a feature that ships disabled is a feature that ships untested.

## Decision 6: game data migrates, packet structs do not

`gamedata.GameData` becomes `data.Set`, repo-wide — 18 call sites across the
world, player, and inventory code. The local data registries, the data
templates in `cmd/codegen`, `cmd/dmd`, and `scheme/pc-1.8/` lose their last
consumer and are deleted.

The generated packet structs in `pkg/gamedata/versions/pc_1_8/packets.go` stay,
because play still uses them and Decision 3 keeps play on local structs. They go
in M6, together with the play migration that replaces them.

This split follows the seam in the code rather than the seam in the package
layout. Data is data; a packet struct is part of the wire path this milestone is
deliberately migrating one state at a time.

## Decision 7: disconnect sends a packet

`Connection.disconnect(reason)` cancels the context and closes the socket, so a
kicked player sees a connection reset rather than a reason. With the stream it
calls `Stream.Shutdown(ctx, reason)`, which builds the state-appropriate
disconnect packet, writes it, drains accepted writes, and then stops.

Login-state and play-state disconnects carry different packet IDs, and the
generated session already knows which. The server stops formatting disconnect
JSON in two places.

## Decision 8: the legacy ping gets handled

The server cannot answer a legacy `FE 01` ping: the read loop reads a VarInt
length first, so those bytes are a framing error. M1 added an opt-in pre-frame
hook for exactly this. M3 installs it and answers with the legacy response
format.

This is new behavior rather than a migration. It is in scope because it is
twenty lines against a hook that already exists, and because "status and ping
work" is a weaker claim if an old client gets a protocol error.

## Verification

Four gates, in order of how much they would embarrass us:

1. **Byte parity.** For status response, ping, login success, encryption
   request, and disconnect, the bytes the new path produces equal the bytes the
   old path produced, asserted against fixtures captured before the migration
   starts.
2. **Node client.** The pinned Node `minecraft-protocol` client, which supports
   1.8.8, connects to the Go server on loopback in offline mode, with and
   without compression, and reaches play.
3. **Vanilla client.** A real 1.8.9 client connects, joins, and plays: move,
   break, place, open a chest, chat, die, respawn, disconnect. Any generated
   decode failure is a codec bug, recorded and fixed in `minecraft-protocol`.
4. **Online mode.** One real Mojang-authenticated login, recorded in the
   milestone notes. It cannot run in CI and it is the only thing that proves the
   acceptor's hash and verify-token handling.

Gates 1 and 2 run in `task test`. Gates 3 and 4 are manual and recorded.

## Risks

**Two repositories, one milestone.** The acceptor lands in
`minecraft-protocol` and everything else in `server`. The plan orders the
acceptor first, with its own tests, so the server migration consumes a finished
interface rather than co-evolving with one.

**Strict decoding on a live wire.** Decision 3's consequence. The mitigation is
a real client session, and the honest expectation is that it finds at least one
mis-modelled packet. That is the milestone working, not failing.

**No existing connection tests.** `server` has tests for inventory, commands,
tab completion, and the cipher — nothing that drives a connection end to end.
The migration has to bring its own harness, and the first task builds it against
the *old* code so it describes current behavior rather than intended behavior.

**The server's `CLAUDE.md` is stale.** It describes an empty `internal/` and a
vendored build, neither of which is true. Anyone reading it while working on
this migration will be misled, so it is updated in the final task.

## Open questions

1. Should the compression threshold live in `config.Config` alongside
   `OnlineMode`, or in a new protocol-settings struct? The plan assumes
   `config.Config`, matching where `OnlineMode` and `MaxPlayers` already are.
2. Does the server want observations wired to anything in M3? The stream can
   publish them and M5 builds the consumers. The plan installs no sink and
   leaves the hook for M5.
