# M3 client verification

Status: **incomplete — the vanilla-client checks have not been run.**

M3 migrated the server's connection onto `minecraft-protocol`. Every automated
gate passes, including a pinned Node client that reaches play. The checks below
need a real Minecraft client, and two of them need a real Mojang account, so
they cannot run in CI and have not been run yet.

Until they are, M3 is implemented but not verified. The plan is explicit about
why they matter: every play packet is now decoded twice, and the generated
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

Not run.

Connect a real 1.8.9 client to a server started with `-online-mode=false` and
run a full session: join, move, break a block, place a block, open a chest,
move an item, chat, take damage, die, respawn, disconnect.

Record the client build and every decode error observed. **Any generated-codec
decode failure is a bug in `minecraft-protocol`**: record the packet, fix the
codec there, add a byte fixture, and re-run. Do not relax the check in this
repository.

| Action | Result | Notes |
| --- | --- | --- |
| Join | | |
| Move | | |
| Break a block | | |
| Place a block | | |
| Open a chest | | |
| Move an item | | |
| Chat | | |
| Take damage | | |
| Die | | |
| Respawn | | |
| Disconnect | | |

## Step 2 — Vanilla client, online mode

Not run. Needs an account that owns the game.

One authenticated login against the real session server, with compression on.
This is the only proof that the acceptor's server hash and verify-token
handling are right: every automated test stubs the session server, and a hash
that is wrong in the same way on both sides of a loopback test still passes it.

| Field | Value |
| --- | --- |
| Client build | |
| Result | |
| Compression threshold | |

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
