# Minecraft 1.8 Protocol Reference (Protocol Version 47)

## When to Use
Use this skill when implementing or debugging Minecraft protocol features — packet encoding/decoding, connection flow, authentication, chunk data, or any network-level server logic.

## Protocol Basics

**Protocol version:** 47 (Minecraft 1.8 through 1.8.9)

### Data Types (all big-endian except VarInt/VarLong)
| Type | Size | Notes |
|------|------|-------|
| VarInt | 1-5 bytes | 7 bits/byte, MSB = continuation, NOT zigzag |
| VarLong | 1-10 bytes | Same for 64-bit |
| String | variable | VarInt length prefix + UTF-8 |
| Position | 8 bytes | Packed: x(26 signed) << 38 | y(12 signed) << 26 | z(26 signed) |
| UUID | 16 bytes | Two unsigned 64-bit integers |
| Angle | 1 byte | Steps of 1/256 of full turn |

### Packet Framing
**Uncompressed:** `[Length VarInt] [PacketID VarInt] [Data bytes]`
**Compressed:** `[PacketLength VarInt] [DataLength VarInt] [zlib(PacketID + Data)]` — DataLength=0 means uncompressed

### Connection States
Handshaking → Status (server list) OR Login (auth) → Play

## Connection Flow (Online Mode)

```
Client → Handshake(protocolVersion=47, nextState=2)
Client → Login Start(username)
Server → Encryption Request(serverId="", publicKey, verifyToken)
Client → Encryption Response(encryptedSecret, encryptedToken)
  [Server verifies with Mojang, both enable AES/CFB8]
Server → Set Compression(threshold)  [optional]
Server → Login Success(uuid, username)
  [State → PLAY]
Server → Join Game(0x01)
Server → Spawn Position(0x05)
Server → Player Abilities(0x39)
Server → Player Position And Look(0x08)  ← exits "Loading terrain"
Server → Chunk Data(0x21) × N
Server → Player Info(0x38)
Server → Chat Message(0x02) "Hello World"
Server → Keep Alive(0x00)  every ~15s
```

## Authentication (Online Mode)

### Server Hash (NON-STANDARD)
```
sha1 = SHA1(ASCII(serverId) + sharedSecret + publicKeyDER)
hash = twos_complement_hex_digest(sha1)  // negative = prepend "-", no zero-pad
```

### Mojang API
**Client → Mojang:** `POST https://sessionserver.mojang.com/session/minecraft/join`
```json
{"accessToken": "...", "selectedProfile": "uuid-no-dashes", "serverId": "hash"}
```

**Server → Mojang:** `GET https://sessionserver.mojang.com/session/minecraft/hasJoined?username=X&serverId=hash`
Returns: `{id, name, properties[{name:"textures", value, signature}]}`

### Encryption
- RSA 1024-bit keypair generated at startup
- Client generates 16-byte shared secret
- Both encrypted with server's public key (PKCS#1 v1.5)
- AES/CFB8 stream cipher with secret as both key and IV
- Continuous across packets (not per-packet)

## Key Play Packets

### Join Game (0x01 Clientbound)
| Field | Type | Notes |
|-------|------|-------|
| entityId | i32 | Player entity ID |
| gameMode | u8 | 0=Survival, 1=Creative, 2=Adventure, 3=Spectator. Bit 3=Hardcore |
| dimension | i8 | -1=Nether, 0=Overworld, 1=End |
| difficulty | u8 | 0=Peaceful..3=Hard |
| maxPlayers | u8 | Tab list hint |
| levelType | string | "default", "flat", etc. |
| reducedDebugInfo | bool | |

### Player Abilities (0x39 Clientbound)
| Field | Type | Notes |
|-------|------|-------|
| flags | i8 | 0x01=Invulnerable, 0x02=Flying, 0x04=AllowFlight, 0x08=CreativeMode |
| flyingSpeed | f32 | Default 0.05 |
| walkingSpeed | f32 | Default 0.1 |

**Creative mode flags:** 0x0D (Invulnerable + AllowFlight + CreativeMode)

### Chat Message (0x02 Clientbound)
| Field | Type |
|-------|------|
| jsonData | string | JSON chat component |
| position | i8 | 0=chat, 1=system, 2=action bar |

### Chunk Data (0x21 Clientbound)
| Field | Type |
|-------|------|
| chunkX | i32 |
| chunkZ | i32 |
| groundUp | bool |
| primaryBitMask | u16 | Bit per 16-high section |
| data | ByteArray | Sections + biomes |

**Section layout per section:** blocks(8192B, 2 bytes/block LE: blockId<<4|meta) + blockLight(2048B nibble) + skyLight(2048B nibble)
**Biome data:** 256 bytes at end if groundUp=true

### Position Encoding
```
encode: ((x & 0x3FFFFFF) << 38) | ((y & 0xFFF) << 26) | (z & 0x3FFFFFF)
```

## Timing
- Keep Alive: send every ~15s, client disconnects after ~20s without one
- Client must echo Keep Alive ID back

## Reference Implementations
- [wiki.vg Protocol](https://c4k3.github.io/wiki.vg/Protocol.html) — Canonical docs
- [wiki.vg Encryption](https://c4k3.github.io/wiki.vg/Protocol_Encryption.html)
- [GoLangMc/minecraft-server](https://github.com/GoLangMc/minecraft-server) — Go server
- [Tnze/go-mc](https://github.com/Tnze/go-mc) — Go MC library
- [szerookii/gocrafty](https://github.com/szerookii/gocrafty) — Go 1.8.9 server
- [minekube/gate](https://github.com/minekube/gate) — Go proxy (1.8-1.21)
- [Attano/Spigot-1.8](https://github.com/Attano/Spigot-1.8) — NMS source reference
- [PrismarineJS/minecraft-data](https://github.com/PrismarineJS/minecraft-data) — Machine-readable protocol definitions

## Local Data
- Protocol definitions: `scheme/pc-1.8/protocol.json`
- Generated packet data: `internal/gamedata/versions/pc_1_8/protocol.go`
- Domain types: `internal/gamedata/protocol.go`
