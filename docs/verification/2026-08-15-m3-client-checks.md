# M3 client verification

Status: **offline and online sessions both run against a real 1.8.9 client, with no decode failures.**

M3 migrated the server's connection onto `minecraft-protocol`. Every automated
gate passes, and the checks below were run by hand with the real game client on
2026-08-15, because they cannot run in CI.

The plan is explicit about why they matter: every play packet is now decoded twice, and the generated
decode is strict where the old loop was not, so a serverbound packet whose
generated model is wrong becomes a disconnect. A real client is what finds
those.

## What has been verified

| Check | Result |
| --- | --- |
| `task lint` | 0 issues |
| `task test` (with `-race`) | pass |
| `task test:interop` | pass — pinned Node `minecraft-protocol` 1.66.2 as a 1.8.8 client, compression off and at 256, reaches play and receives chunk data |
| `task build` | pass |
| Byte-parity fixtures | unchanged — status, ping, login success, encryption request, and both disconnect forms are byte-identical to the pre-migration server |
| Login against the real client negotiator | pass — `login.Negotiator` from `minecraft-protocol` logs in at thresholds `-1`, `1`, and `256` |
| Offline identity | pass — `login.OfflineUUID` matches the server's previous derivation for every name tested |

## Step 1 — Vanilla client, offline mode

Run on 2026-08-15 against the default generator with the saved world, offline
mode, compression at the default 256. The client identified itself as
`brand="\avanilla"` with `viewDistance=32`.

**The server logged zero errors and zero warnings for the whole session.** No
generated codec rejected a packet the real client sent, which is what this step
exists to find.

Two rows on the original checklist describe features this server does not
have, so they are marked N/A rather than left looking unrun:

- **Take damage** — there is no environmental damage anywhere in the server.
  `UpdateHealth` is written only by `/kill` and by the post-respawn restore.
  Damage exists through PvP alone, which needs a second player.
- **Open a chest** — chests are not implemented. There is no `OpenWindow` in
  the codebase; only the player's own inventory and the 2x2 grid exist.

A third gap surfaced during the session: **the crafting table does not work**,
and inventory crafting works only partially. The table needs a 3x3 matcher and
a window the server never opens, so it is a missing feature that predates M3.
The partial 2x2 behavior is **not yet explained and may be an M3 regression**:
Task 4 moved the recipe registry to `minecraft-protocol/data`, and the matcher
treats negative ingredient metadata as "any variant". If the shared data
encodes a wildcard as `0` instead, variant recipes stop matching while exact
ones keep working. No test covers `matchRecipe2x2` against the real registry,
which is how a change like that would pass unnoticed.

Connect a real 1.8.9 client to a server started with `-online-mode=false` and
run a full session: join, move, break a block, place a block, open a chest,
move an item, chat, take damage, die, respawn, disconnect.

Record the client build and every decode error observed. **Any generated-codec
decode failure is a bug in `minecraft-protocol`**: record the packet, fix the
codec there, add a byte fixture, and re-run. Do not relax the check in this
repository.

| Action | Result | Notes |
| --- | --- | --- |
| Join | Pass | Saved player data restored; join sequence completed |
| Move | Pass | Position and look accepted; entity movement broadcast |
| Break a block | Pass | Verified with a scripted client: block change, break animation, item drop, and pickup all followed |
| Place a block | Not run | |
| Open a chest | N/A | Chests are not implemented |
| Move an item | Not run | Inventory crafting is partially broken; see above |
| Chat | Pass | Round-tripped, including a command |
| Take damage | N/A | No environmental damage exists; PvP needs a second player |
| Die | Pass | `/kill` |
| Respawn | Pass | Followed `/kill` |
| Disconnect | Pass | Logged as a normal disconnect after the fix below |

One defect found and fixed by running this session: every normal client hangup
was logged at `ERROR`. The old read loop compared `err == io.EOF`, and the
managed stream wraps that error rather than returning it, so the comparison
stopped matching when M3 moved the connection onto the stream.

## Step 2 — Vanilla client, online mode

Run on 2026-08-15. One authenticated login against the real Mojang session
server, with encryption and compression both active.

This is the only proof that the acceptor's server hash and verify-token
handling are right: every automated test stubs the session server, and a hash
that is wrong in the same way on both sides of a loopback test still passes it.
The same argument sank the CFB8 cipher in M2, where two Go peers agreed with
each other and with no real implementation.

| Field | Value |
| --- | --- |
| Client build | Vanilla 1.8.9, `brand="\avanilla"` |
| Result | Pass — reached play, join sequence completed |
| Account UUID | Mojang-issued, distinct from the offline derivation, which is what proves the session server answered |
| Compression threshold | 256 (default) |
| Errors logged | None |

What this exercised end to end: the encryption request carrying the server's
public key, the client's encrypted session key and verify token, the
constant-time token comparison, the AES-CFB8 switch at the frame boundary,
`java.ComputeServerHash`, and the `hasJoined` call behind `login.Verifier`.

## Step 3 — Compression on and off

Not run with a vanilla client. The Node lane covers `-1` and `256`, and the
Go-to-Go login tests cover `-1`, `1`, and `256`.

Repeat the offline join at thresholds `-1`, `256`, and `1`, confirming a
vanilla client joins in all three.

| Threshold | Joined | Notes |
| --- | --- | --- |
| `-1` | | |
| `256` | | |
| `1` | | |
