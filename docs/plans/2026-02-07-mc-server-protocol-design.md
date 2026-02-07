# Minecraft Server Protocol Implementation Design

## Goal

Implement a Minecraft 1.8 (protocol 47) server that:
- Accepts client connections with both online-mode and offline-mode auth (configurable)
- Spawns the player in a flat stone world in Creative mode
- Sends a "Hello, world!" chat message on join
- Maintains the connection with KeepAlive packets

## Package Structure

```
cmd/server/
    main.go                     // CLI entry: parse config, start server, graceful shutdown

internal/server/
    server.go                   // Server: TCP listener, accept loop, connection dispatch
    config.go                   // Config: port, online-mode, motd, max-players
    conn/
        connection.go           // Connection: state machine, read/write loop
        handler_handshake.go    // Handshake state handler
        handler_status.go       // Status state handler (server list ping)
        handler_login.go        // Login state handler (offline + online auth)
        handler_play.go         // Play state handler (join sequence, packet dispatch)
        crypto.go               // RSA keypair, AES/CFB8, Mojang session API, server hash
    net/
        buffer.go               // Buffer: ReadVarInt, WriteString, etc.
        varint.go               // VarInt/VarLong encoding/decoding
        packet.go               // Packet interface, ID registry
        marshal.go              // Struct-tag reflection marshal/unmarshal
    packet/
        handshake.go            // Handshake packet struct
        login.go                // LoginStart, EncryptionRequest/Response, LoginSuccess, SetCompression, Disconnect
        play.go                 // JoinGame, SpawnPosition, PlayerAbilities, PlayerPositionLook, ChunkData,
                                // PlayerInfo, ChatMessage, KeepAlive, HeldItemSlot, PluginMessage
        serverbound.go          // Serverbound play packets: KeepAlive, Chat, Position, Look, ClientSettings
    world/
        chunk.go                // Flat stone chunk generation, chunk data encoding
```

## Cross-Cutting Concerns

### Logging
Use `log/slog` (Go stdlib). Each component receives a `*slog.Logger`. Connection-scoped loggers include `addr` and `player` attributes.

### Context Propagation
`context.Context` flows from `main → Server.Start → Connection.Handle`. Context cancellation triggers graceful shutdown of all connections.

### Graceful Shutdown
```go
ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
// Server.Start blocks until ctx cancelled
// All connections drain and close
```

### Error Handling
- Errors wrapped with `fmt.Errorf("context: %w", err)` at each layer
- Network errors on individual connections are logged + connection closed, never crash the server
- Protocol violations (bad packet, wrong state) → disconnect with reason

## Struct-Tag Packet System

### Tag Format
```go
type JoinGame struct {
    EntityID         int32  `mc:"i32"`
    GameMode         uint8  `mc:"u8"`
    Dimension        int8   `mc:"i8"`
    Difficulty       uint8  `mc:"u8"`
    MaxPlayers       uint8  `mc:"u8"`
    LevelType        string `mc:"string"`
    ReducedDebugInfo bool   `mc:"bool"`
}

func (JoinGame) PacketID() int32 { return 0x01 }
```

### Supported Tags
| Tag | Go Type | MC Type |
|-----|---------|---------|
| `varint` | int32 | VarInt |
| `varlong` | int64 | VarLong |
| `i8` | int8 | Byte |
| `u8` | uint8 | Unsigned Byte |
| `i16` | int16 | Short |
| `u16` | uint16 | Unsigned Short |
| `i32` | int32 | Int |
| `i64` | int64 | Long |
| `f32` | float32 | Float |
| `f64` | float64 | Double |
| `bool` | bool | Boolean |
| `string` | string | String (VarInt-prefixed UTF-8) |
| `position` | int64 | Position (packed x/y/z) |
| `uuid` | [16]byte | UUID |
| `bytearray` | []byte | ByteArray (VarInt-prefixed) |
| `rest` | []byte | Remaining bytes |

### Packet Interface
```go
type Packet interface {
    PacketID() int32
}
```

### Marshal/Unmarshal
```go
func Marshal(p Packet) ([]byte, error)   // struct → bytes via reflection + tags
func Unmarshal(data []byte, p Packet) error  // bytes → struct via reflection + tags
```

## Connection Lifecycle

### State Machine
```
HANDSHAKE → LOGIN → PLAY
              ↓
            STATUS
```

### Handshake Phase
1. Read `Handshake` packet (0x00)
2. Validate protocol version (47)
3. Switch to Login or Status based on `NextState`

### Login Phase (Offline Mode)
1. Read `LoginStart` (username)
2. Generate offline UUID from username: `UUID v3("OfflinePlayer:" + username)`
3. Send `LoginSuccess` (uuid, username)
4. Transition to Play

### Login Phase (Online Mode)
1. Read `LoginStart` (username)
2. Generate RSA 1024-bit keypair (once at server startup, reused)
3. Generate random 4-byte verify token
4. Send `EncryptionRequest` (serverId="", publicKey, verifyToken)
5. Read `EncryptionResponse` (encrypted sharedSecret, encrypted verifyToken)
6. Decrypt both using RSA private key
7. Verify decrypted token matches original
8. Compute server hash: `SHA1(serverId + sharedSecret + publicKeyDER)` with Minecraft's non-standard hex digest
9. Verify with Mojang: `GET sessionserver.mojang.com/session/minecraft/hasJoined?username=X&serverId=hash`
10. Enable AES/CFB8 encryption on connection (key=IV=sharedSecret)
11. Send `LoginSuccess` (uuid from Mojang, username)
12. Transition to Play

### Play Phase — Join Sequence
After Login Success, send in order:
1. **Join Game** (0x01) — entityId=1, gameMode=1 (Creative), dimension=0 (Overworld), difficulty=1 (Easy), maxPlayers=20, levelType="flat", reducedDebugInfo=false
2. **Spawn Position** (0x05) — Position(0, 4, 0)
3. **Player Abilities** (0x39) — flags=0x0D (Invulnerable+AllowFlight+CreativeMode), flySpeed=0.05, walkSpeed=0.1
4. **Player Position And Look** (0x08) — x=0.5, y=4.0, z=0.5, yaw=0, pitch=0, flags=0x00 (all absolute)
5. **Chunk Data** (0x21) × 49 — 7×7 grid centered on chunk(0,0), flat stone world
6. **Player Info** (0x38) — action=0 (AddPlayer), self entry with gamemode=1
7. **Chat Message** (0x02) — `{"text":"Hello, world!","color":"gold"}`, position=0
8. Start **KeepAlive** goroutine

### Play Phase — Packet Dispatch Loop
Read packets in a loop, dispatch by ID:
- `0x00 KeepAlive` — validate ID matches last sent
- `0x01 Chat` — log the message
- `0x04/0x05/0x06 Position/Look` — track player position
- `0x15 ClientSettings` — acknowledge
- Other — ignore silently

### KeepAlive
- Goroutine per connection, sends KeepAlive every 15 seconds
- Tracks last sent ID and timestamp
- If no response within 30 seconds, disconnect
- Cancelled via connection context

## Flat Stone World

### Chunk Generation
For each chunk in the 7×7 grid:
- Section 0 (y=0..15): Stone at y=0, air above
- Primary bit mask: 0x0001 (only section 0)
- Ground-up continuous: true
- Block data: `blockId=1 (stone), meta=0` → LE bytes `0x10, 0x00` for y=0, `0x00, 0x00` for y>0
- Block light: all 0xFF (full light)
- Sky light: all 0xFF
- Biome: all 0x01 (Plains)

Block encoding: `(blockId << 4) | metadata`, stored as little-endian u16
Block index: `y*256 + z*16 + x`

## Config

```go
type Config struct {
    Port       int    // default: 25565
    OnlineMode bool   // default: false
    MOTD       string // default: "A Minecraft Server"
    MaxPlayers int    // default: 20
}
```

Parsed from CLI flags in `cmd/server/main.go`.

## Taskfile Integration

```yaml
# Taskfile.yml
server:
  desc: Run the Minecraft server
  cmds:
    - go run ./cmd/server
```

## Implementation Order

1. `internal/server/net/` — VarInt, Buffer, struct-tag marshal/unmarshal
2. `internal/server/packet/` — All packet structs with tags
3. `internal/server/conn/` — Connection state machine, handshake + login (offline-mode)
4. `internal/server/world/` — Flat stone chunk generation
5. `internal/server/` — Server listener, config
6. `cmd/server/main.go` — CLI entry, graceful shutdown
7. Test: connect with Minecraft 1.8 client in offline mode
8. `internal/server/conn/crypto.go` — RSA, AES/CFB8, Mojang auth
9. Test: connect with online-mode client
10. Tests for marshal/unmarshal, VarInt, chunk encoding
