# Packet type mapping: server `pkt.` types onto generated protocol 47

This is the reference for the M6.1 play-state migration. It pairs every
`pkt.`-qualified identifier the server references with its counterpart in
`minecraft-protocol`'s generated `generated/java/v1_8` package (Go package
`v1_8`), and records every field difference between the paired structs.

Tasks 2-8 rename by this table. Do not re-derive pairings by name similarity —
use the `(state, direction, packet ID)` recorded here.

## How this was produced

- **Step 1 — what the server uses.** `grep -rho 'pkt\.[A-Z][A-Za-z0-9_]*'`
  over all `*.go` (tests included), sorted unique, yielded **74** referenced
  identifiers. Of those, **71 are packet structs** (each has a
  `PacketID() int32` method) and **3 are constants** (`ProtocolVersion`,
  `VersionName`, `MetadataEnd`).
- **Step 2 — what the generated package offers.** The `PacketID()` methods in
  `../minecraft-protocol/generated/java/v1_8/packets.go` define **111** packet
  types. The authoritative `(state, direction, id) -> type` table lives in
  that package's `descriptor.go`; it has the same 111 entries.
- **Step 3 — pairing by `(state, direction, packet ID)`.** Packet IDs collide
  across states and directions (play clientbound `0x00` and play serverbound
  `0x00` are different packets), so name and ID alone are insufficient; the
  triple is the key. Each server type's ID comes from its own `PacketID()`
  method; its state and direction are fixed by role (handshaking/status/login
  types) and by the `CB`/`SB` suffix or, for unsuffixed play types, by which
  direction's ID space the packet lives in. Every pairing below matches the
  generated `descriptor.go` entry at the same triple.
- **Step 4 — field diff.** A throwaway module (`/tmp/mapping`, its own `go.mod`
  with `replace` directives onto both real modules) constructed a value of each
  paired type and compared `reflect.TypeOf(...)` field-by-field: name, Go type,
  and order. Differences are recorded per type below.

### Counts observed vs. predicted

The brief predicted 74 referenced identifiers; that came out **exactly 74**
(71 packet structs + 3 constants), and every one of the 71 packet structs
paired with a generated type. Verification totals from the reflection pass:

```
pairs=71  idMismatch=0  identical=49  fieldDiff=22
```

Zero ID mismatches means no pairing crosses a `(state, direction, id)`
boundary. Zero field **re-orderings** were found: every difference is either a
type-representation change (same field name and position) or a body the server
modelled as one opaque blob that the generated type splits into named fields.
There is no case where two same-named fields swap positions — the silent-wire
hazard the brief warns about does not occur in this set.

## Severity legend for the field-difference column

- **identical** — same field names, Go types, and order. A pure rename; safe.
- **repr** — same field name and position, different Go representation of the
  same wire bytes (`int64`+`mc:"position"` vs the generated `Position` struct;
  `[16]byte`+`mc:"uuid"` vs `java.UUID`). Wire-compatible, but call sites must
  construct/read the new Go type. Moderate: check each call site.
- **blob** — the server modelled the packet body as a single opaque
  `Data []byte` with `mc:"rest"` and never decoded it; the generated type
  decodes it into named fields. No transposition risk, but any code that built
  or read these bytes by hand must move to the structured fields. Hazard: the
  behaviour genuinely expands here.

## Mapping table

State/dir is abbreviated: `P/CB` = play clientbound, `P/SB` = play serverbound,
`S/CB` status clientbound, `S/SB` status serverbound, `L/CB` login clientbound,
`L/SB` login serverbound, `H/SB` handshaking serverbound.

| Server `pkt.` type | Generated `v1_8` type | State/dir | ID | Field differences |
|---|---|---|---|---|
| ServerInfo | StatusClientboundServerInfo | S/CB | 0x00 | identical (`Response string`) |
| PingStart | StatusServerboundPingStart | S/SB | 0x00 | identical (no fields) |
| PingCB | StatusClientboundPing | S/CB | 0x01 | identical (`Time int64`) |
| PingSB | StatusServerboundPing | S/SB | 0x01 | identical (`Time int64`) |
| SetProtocol | HandshakingServerboundSetProtocol | H/SB | 0x00 | identical (4 fields) |
| LoginStart | LoginServerboundLoginStart | L/SB | 0x00 | identical (`Username string`) |
| EncryptionBeginCB | LoginClientboundEncryptionBegin | L/CB | 0x01 | identical (3 fields) |
| Success | LoginClientboundSuccess | L/CB | 0x02 | identical (2 fields) |
| Disconnect | LoginClientboundDisconnect | L/CB | 0x00 | identical (`Reason string`) |
| AbilitiesCB | PlayClientboundAbilities | P/CB | 0x39 | identical (3 fields) |
| Animation | PlayClientboundAnimation | P/CB | 0x0B | identical (2 fields) |
| BlockBreakAnimation | PlayClientboundBlockBreakAnimation | P/CB | 0x25 | **repr** — `Location`: `int64 mc:"position"` -> `Position` |
| BlockChange | PlayClientboundBlockChange | P/CB | 0x23 | **repr** — `Location`: `int64 mc:"position"` -> `Position` |
| ChatCB | PlayClientboundChat | P/CB | 0x02 | identical (2 fields) |
| Collect | PlayClientboundCollect | P/CB | 0x0D | identical (2 fields) |
| CustomPayloadCB | PlayClientboundCustomPayload | P/CB | 0x3F | identical (2 fields) |
| EntityDestroy | PlayClientboundEntityDestroy | P/CB | 0x13 | **blob** — `Data []byte` -> `EntityIds []int32` |
| EntityEquipment | PlayClientboundEntityEquipment | P/CB | 0x04 | **blob** — `Data []byte` -> `EntityID int32`, `Slot int16`, `Item Slot` |
| EntityHeadRotation | PlayClientboundEntityHeadRotation | P/CB | 0x19 | identical (2 fields) |
| EntityLook | PlayClientboundEntityLook | P/CB | 0x16 | identical (4 fields) |
| EntityMetadata | PlayClientboundEntityMetadata | P/CB | 0x1C | **blob** — `Data []byte` -> `EntityID int32`, `Metadata EntityMetadata` |
| EntityMoveLook | PlayClientboundEntityMoveLook | P/CB | 0x17 | identical (7 fields) |
| EntityStatus | PlayClientboundEntityStatus | P/CB | 0x1A | identical (2 fields) |
| EntityTeleport | PlayClientboundEntityTeleport | P/CB | 0x18 | identical (7 fields) |
| EntityVelocity | PlayClientboundEntityVelocity | P/CB | 0x12 | identical (4 fields) |
| GameStateChange | PlayClientboundGameStateChange | P/CB | 0x2B | identical (2 fields) |
| KeepAliveCB | PlayClientboundKeepAlive | P/CB | 0x00 | identical (`KeepAliveID int32`) |
| KickDisconnect | PlayClientboundKickDisconnect | P/CB | 0x40 | identical (`Reason string`) |
| Login | PlayClientboundLogin | P/CB | 0x01 | identical (7 fields) |
| MapChunk | PlayClientboundMapChunk | P/CB | 0x21 | identical (5 fields) |
| NamedEntitySpawn | PlayClientboundNamedEntitySpawn | P/CB | 0x0C | **blob** — `Data []byte` -> 9 fields (`EntityID`, `PlayerUUID`, `X/Y/Z`, `Yaw`, `Pitch`, `CurrentItem`, `Metadata`) |
| PlayerInfo | PlayClientboundPlayerInfo | P/CB | 0x38 | **blob** — `Data []byte` -> `Action string`, `Data []PlayClientboundPlayerInfoDataItem` |
| PositionCB | PlayClientboundPosition | P/CB | 0x08 | identical (6 fields) |
| RelEntityMove | PlayClientboundRelEntityMove | P/CB | 0x15 | identical (5 fields) |
| Respawn | PlayClientboundRespawn | P/CB | 0x07 | identical (4 fields) |
| SetSlot | PlayClientboundSetSlot | P/CB | 0x2F | **blob** — `Data []byte` -> `WindowID int8`, `Slot int16`, `Item Slot` |
| SpawnEntity | PlayClientboundSpawnEntity | P/CB | 0x0E | **blob** — `Data []byte` -> 9 fields (`EntityID`, `Type`, `X/Y/Z`, `Pitch`, `Yaw`, `IntField`, `ObjectData` switch) |
| SpawnPosition | PlayClientboundSpawnPosition | P/CB | 0x05 | **repr** — `Location`: `int64 mc:"position"` -> `Position` |
| TabCompleteCB | PlayClientboundTabComplete | P/CB | 0x3A | **blob** — `Data []byte` -> `Matches []string` |
| TransactionCB | PlayClientboundTransaction | P/CB | 0x32 | identical (3 fields) |
| UpdateHealth | PlayClientboundUpdateHealth | P/CB | 0x06 | identical (3 fields) |
| UpdateTime | PlayClientboundUpdateTime | P/CB | 0x03 | identical (2 fields) |
| WindowItems | PlayClientboundWindowItems | P/CB | 0x30 | **blob** — `Data []byte` -> `WindowID uint8`, `Items []Slot` |
| WorldEvent | PlayClientboundWorldEvent | P/CB | 0x28 | **repr** — `Location`: `int64 mc:"position"` -> `Position` (fields `EffectID`, `Data`, `Global` identical) |
| WorldParticles | PlayClientboundWorldParticles | P/CB | 0x2A | **blob** — `Data []byte` -> 11 fields (`ParticleID`, `LongDistance`, `X/Y/Z`, `OffsetX/Y/Z`, `ParticleData`, `Particles`, `Data` switch) |
| AbilitiesSB | PlayServerboundAbilities | P/SB | 0x13 | identical (3 fields) |
| ArmAnimation | PlayServerboundArmAnimation | P/SB | 0x0A | identical (no fields) |
| BlockDig | PlayServerboundBlockDig | P/SB | 0x07 | **repr** — `Location`: `int64 mc:"position"` -> `Position` (`Status`, `Face` identical) |
| BlockPlace | PlayServerboundBlockPlace | P/SB | 0x08 | **blob** — `Data []byte` -> `Location Position`, `Direction int8`, `HeldItem Slot`, `CursorX/Y/Z int8` |
| ChatSB | PlayServerboundChat | P/SB | 0x01 | identical (`Message string`) |
| ClientCommand | PlayServerboundClientCommand | P/SB | 0x16 | identical (`Payload int32`) |
| CloseWindowSB | PlayServerboundCloseWindow | P/SB | 0x0D | identical (`WindowID uint8`) |
| CustomPayloadSB | PlayServerboundCustomPayload | P/SB | 0x17 | identical (2 fields) |
| EnchantItem | PlayServerboundEnchantItem | P/SB | 0x11 | identical (2 fields) |
| EntityAction | PlayServerboundEntityAction | P/SB | 0x0B | identical (3 fields) |
| Flying | PlayServerboundFlying | P/SB | 0x03 | identical (`OnGround bool`) |
| HeldItemSlotSB | PlayServerboundHeldItemSlot | P/SB | 0x09 | identical (`SlotID int16`) |
| KeepAliveSB | PlayServerboundKeepAlive | P/SB | 0x00 | identical (`KeepAliveID int32`) |
| Look | PlayServerboundLook | P/SB | 0x05 | identical (3 fields) |
| PositionSB | PlayServerboundPosition | P/SB | 0x04 | identical (4 fields) |
| PositionLook | PlayServerboundPositionLook | P/SB | 0x06 | identical (6 fields) |
| ResourcePackReceive | PlayServerboundResourcePackReceive | P/SB | 0x19 | identical (2 fields) |
| SetCreativeSlot | PlayServerboundSetCreativeSlot | P/SB | 0x10 | **blob** — `Data []byte` -> `Slot int16`, `Item Slot` |
| Settings | PlayServerboundSettings | P/SB | 0x15 | identical (5 fields) |
| Spectate | PlayServerboundSpectate | P/SB | 0x18 | **repr** — `Target`: `[16]byte mc:"uuid"` -> `java.UUID` |
| SteerVehicle | PlayServerboundSteerVehicle | P/SB | 0x0C | identical (3 fields) |
| TabCompleteSB | PlayServerboundTabComplete | P/SB | 0x14 | **blob** — `Data []byte` -> `Text string`, `Block *Position` |
| TransactionSB | PlayServerboundTransaction | P/SB | 0x0F | identical (3 fields) |
| UpdateSignSB | PlayServerboundUpdateSign | P/SB | 0x12 | **repr** — `Location`: `int64 mc:"position"` -> `Position` (`Text1..4` identical) |
| UseEntity | PlayServerboundUseEntity | P/SB | 0x02 | **blob** — `Data []byte` -> `Target int32`, `Mouse int32`, `X/Y/Z` switch fields |
| WindowClick | PlayServerboundWindowClick | P/SB | 0x0E | **blob** — `Data []byte` -> `WindowID uint8`, `Slot int16`, `MouseButton int8`, `Action int16`, `Mode int8`, `Item Slot` |

## Field differences grouped by severity

### repr — representation change, same name and position (7)

Same field name, same order, wire-compatible; only the Go type of one field
changes. Every call site that sets or reads the field must switch to the new
type.

- `Location`: `int64` with `mc:"position"` -> generated `Position{X int32; Y
  int16; Z int32}` — **BlockBreakAnimation, BlockChange, SpawnPosition,
  WorldEvent, BlockDig, UpdateSignSB**.
- `Target`: `[16]byte` with `mc:"uuid"` -> `java.UUID` — **Spectate**.

### blob — opaque body now structured (15)

The server type is a single `Data []byte` with `mc:"rest"`; the generated type
decodes the body into named fields. Any code that hand-built or hand-parsed
these bytes must move to the structured fields. No field is transposed — the
server simply never modelled the body.

EntityDestroy, EntityEquipment, EntityMetadata, NamedEntitySpawn, PlayerInfo,
SetSlot, SpawnEntity, TabCompleteCB, WindowItems, WorldParticles, BlockPlace,
SetCreativeSlot, TabCompleteSB, UseEntity, WindowClick.

### identical (49)

The remaining 49 pairs match field-for-field on name, Go type, and order. A
pure rename with no behavioural change.

## Unpaired types and their disposition

### Server constants — `ProtocolVersion`, `VersionName`, `MetadataEnd`

These three referenced identifiers are `const`, not packet structs, so they have
no `(state, direction, id)` and are not part of the struct rename. The brief
predicted them as having "no generated counterpart"; the tree has since moved —
`generated/java/v1_8/version.go` now defines all three:

| Constant | Server value | Generated value | Disposition |
|---|---|---|---|
| `ProtocolVersion int32` | `47` | `47` | Identical. May be sourced from `v1_8` or left as-is; no wire effect either way. |
| `MetadataEnd byte` | `0x7F` | `0x7F` | Identical. Only referenced as the entity-metadata terminator; the generated `EntityMetadata` codec handles termination internally, so this stays a server-side constant unless a later task removes its last use. |
| `VersionName string` | `"1.8.8"` | `"1.8.9"` | **Diverges.** `pkg/gamedata/versions/pc_1_8/version.go` deliberately keeps `"1.8.8"` so the status response advertises the same bytes; both are protocol 47. Do **not** adopt the generated `"1.8.9"` in this migration — reconciling the two names is a separate decision, not a side effect. Keep the server constant. |

None of the three blocks the struct rename.

### Server types that pair but are referenced only by tests (9)

`Disconnect`, `EncryptionBeginCB`, `LoginStart`, `PingCB`, `PingSB`,
`PingStart`, `ServerInfo`, `SetProtocol`, `Success` are still referenced, but
only from `*_test.go` — no production file outside `pc_1_8` uses them. M3
already moved the transport and the handshaking/status/login packets onto
generated types in production, leaving these nine alive only in tests. They
pair cleanly (see table), so the rename is mechanical; a later task can point
the tests at the generated types and drop the server structs.

### Generated types with no server counterpart (40)

The generated package defines 111 packet types; 71 are used by this migration,
leaving 40 the server never modelled (many because the server carried their
clientbound bodies as opaque bytes elsewhere, or never handled them):

LoginClientboundCompress, LoginServerboundEncryptionBegin,
PlayClientboundAttachEntity, PlayClientboundBed, PlayClientboundBlockAction,
PlayClientboundCamera, PlayClientboundCloseWindow, PlayClientboundCombatEvent,
PlayClientboundCraftProgressBar, PlayClientboundDifficulty, PlayClientboundEntity,
PlayClientboundEntityEffect, PlayClientboundExperience, PlayClientboundExplosion,
PlayClientboundHeldItemSlot, PlayClientboundMap, PlayClientboundMapChunkBulk,
PlayClientboundMultiBlockChange, PlayClientboundNamedSoundEffect,
PlayClientboundOpenSignEntity, PlayClientboundOpenWindow,
PlayClientboundPlayerlistHeader, PlayClientboundRemoveEntityEffect,
PlayClientboundResourcePackSend, PlayClientboundScoreboardDisplayObjective,
PlayClientboundScoreboardObjective, PlayClientboundScoreboardScore,
PlayClientboundScoreboardTeam, PlayClientboundSetCompression,
PlayClientboundSpawnEntityExperienceOrb, PlayClientboundSpawnEntityLiving,
PlayClientboundSpawnEntityPainting, PlayClientboundSpawnEntityWeather,
PlayClientboundStatistics, PlayClientboundTileEntityData, PlayClientboundTitle,
PlayClientboundUpdateAttributes, PlayClientboundUpdateEntityNBT,
PlayClientboundUpdateSign, PlayClientboundWorldBorder.

These need no action for the migration; they are extra capability the generated
package brings.

## What later tasks must carry from here

1. **The pairing is the `(state, direction, id)` triple in the table, not the
   name.** Several IDs repeat across direction (e.g. `0x0B` is
   `PlayClientboundAnimation` and `PlayServerboundEntityAction`); trust the
   table, not name resemblance.
2. **`repr` fields** (7) change one field's Go type while preserving name,
   order, and wire bytes — audit every construction and read of `Location`
   (six packets) and `Target` (Spectate).
3. **`blob` fields** (15) are where behaviour actually expands: the server
   passed these bodies as raw bytes. Tasks 4-6 must build the structured
   generated fields instead of copying a `Data []byte`.
4. **`VersionName` must stay `"1.8.8"`** — do not let a "use the generated
   constant" cleanup silently change the advertised version to `"1.8.9"`.
5. **No field re-orderings and no ID mismatches were found**, so once a pair is
   renamed the only remaining risk is the `repr`/`blob` field handling above.
