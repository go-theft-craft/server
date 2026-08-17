# M6.1 play-state client verification

Status: **RUN, and it passed.** A vanilla client played every scenario below on
2026-08-17 and the generated codec rejected nothing: **zero decode errors**
across the whole session. M6.1 is Complete.

M6.1 moved the server's play state off its own protocol 47 packet structs and
onto `minecraft-protocol`'s generated types, deleting the server's remaining
wire code and its code generation. The server now owns no wire code: every
packet is a generated `minecraft-protocol` type.

Why this check exists: the byte-parity fixtures prove the produced bytes did not
change, but they do not prove the strict generated **decode** accepts everything
a real client sends. Every serverbound play packet is now decoded by the
generated codec, which is strict where the old hand-rolled loop was not. A
serverbound packet whose generated model is wrong becomes a disconnect. M3's
client check found zero decode errors for handshake, status, and login and left
play on local structs; play is the half this check covers.

**Any generated-codec decode failure is a bug in `minecraft-protocol`**, not in
the server: record the packet, fix the codec there, add a byte fixture, and
re-run. Do not relax the check in this repository.

## Automated gates (all green)

These ran in CI / locally and are green as of this record; the manual session
below is the only outstanding gate.

| Check | Result |
| --- | --- |
| `task lint` | 0 issues |
| `task test` | pass |
| `task test:race` | pass (race detector; new this milestone) |
| `task test:interop` | pass — pinned Node `minecraft-protocol` reaches play over loopback |
| `task build` | pass |
| Byte-parity fixtures | unchanged — all six fixtures captured from the unmigrated server are byte-identical, and all five parity tests still compare produced bytes against them |

Before the session below, the strict generated decode had been proven only
against the pinned Node loopback interop lane. It is now proven against a real
client too.

## Session record

The session ran against a server started from this working tree with
`-online-mode=false` and the world in `data/`. It spanned several server builds
rather than one: gameplay defects the session surfaced were fixed as they were
found and the server was restarted, so the scenarios below were played across
nine server processes. That does not weaken what this check measures. Every
build decoded every serverbound play packet with the same generated codec, and
none of the fixes touched decoding.

| Field | Value |
| --- | --- |
| Date run | 2026-08-17 |
| Client build | vanilla Minecraft 1.8, protocol 47, client brand `vanilla`, locale `en_US`, view distance 32 |
| Server mode | offline, compression threshold 1, view distance 12, generator `default`, seed 12345 |
| Total decode errors | **0** |

The compression threshold is worth noting: 1, not the 256 default, so
practically every packet in the session travelled compressed. That exercises
the compressed path harder than the default would, and no scenario was run at
256.

### Scenario checklist

- [x] **Join** — decode errors: 0. Several joins across the session, each
  reaching `join sequence complete`.
- [x] **Move** — decode errors: 0
- [x] **Break a block** — decode errors: 0
- [x] **Place a block** — decode errors: 0
- [x] **Open a chest** — decode errors: 0. Chests were **not implemented** when
  the session began; the server opened no container window and a right-click
  fell through to the placement path. They were implemented during the session,
  and the scenario was then played for a single chest, a double chest, and a
  trapped chest.
- [x] **Craft in the 2x2 grid** — decode errors: 0
- [x] **Craft in the 3x3 grid** — decode errors: 0. A chest was crafted at a
  table, which is a 3x3-only recipe.
- [x] **Shift-click the crafting output** — decode errors: 0. Shift-click was
  also exercised in the player window and in a chest window, and the drag
  (paint) click mode carried most of the session's clicks.
- [x] **Take damage** — decode errors: 0. Contact damage did **not exist** when
  the session began: the server had no health state at all, and nothing could
  hurt a player. Cactus damage was implemented during the session and the
  scenario played after it.
- [x] **Die** — decode errors: 0. By `/kill` and by cactus.
- [x] **Respawn** — decode errors: 0
- [x] **Chat** — decode errors: 0
- [x] **Run a command with tab completion** — decode errors: 0. `/kill` and
  `/gamemode` were run, and tab completion was exercised in chat.
- [x] **Disconnect** — decode errors: 0. Clean disconnects and reconnects
  throughout.

### Decode errors observed

**None.** No `level=ERROR` line appeared in any of the nine server logs, and
the read loop never dropped a connection on a rejected packet — which is what a
decode failure looks like here, since `handleNextPacket` returning an error
ends the connection.

This is the property the check exists to establish: the strict generated decode
accepts everything a real client sends in play, not just what the pinned Node
interop lane sends. Together with M3's zero errors for handshake, status and
login, protocol 47's serverbound surface is now covered by a real client end to
end.

### Notes

Nothing was marked N/A. Every scenario was played, though three of them only
after the feature they exercise was built during the session.

The session was a decode check that turned into a gameplay bug hunt. What it
found, none of it a decode failure:

- **A placed chest vanished on the next chunk load.** The server stored chests
  as `id<<4` with metadata 0, and a chest's facing is horizontal only — so
  metadata 0 is not a chest state. A 1.8 client resolves each chunk section
  value against its registry of valid states and falls back to air when there
  is no match, so it drew air, and only the client's own placement prediction
  ever made a chest visible. Placement now follows
  `BlockChest.onBlockPlacedBy`. This is the general shape of the defect:
  **a state this server invents that the client cannot resolve is not a wrong
  block, it is no block at all.**
- **A double chest could face two ways.** `BlockChest.onBlockAdded` re-orients
  the placed chest and every neighbouring chest, which the server never did, so
  a pair could disagree and a broken pair left its survivor oriented for a
  partner it no longer had.
- **Chests could be placed in impossible arrangements**, which
  `BlockChest.canPlaceBlockAt` forbids: no chest may join a pair, and none may
  have two chest neighbours.
- **Blocks placed above the generated terrain were dropped from every chunk
  send.** `EncodeChunk` built its section bitmap from the sections the
  generator filled, so an override in an empty section was stored, broadcast,
  and then never sent again. The anvil writer already handled this; the network
  encoder did not.
- **Items could be placed as blocks.** An apple, seeds and a diamond pickaxe
  were in the world as block states. Protocol 47 numbers blocks 0-255 and items
  above, and the check now refuses anything past the block range.

Two limits carried out of the session. Vanilla-parity behaviour was read from
the deobfuscated 1.8.9 client in `minecraft-reference` rather than guessed at,
and doing that earlier would have saved most of the hunt. And
`TestDisconnectSendsAPlayDisconnectPacket` is flaky — it fails roughly a third
of full-suite runs on unmodified `main`, measured in a clean worktree, and
wants its own fix.
