# M6.1 play-state client verification

Status: **NOT RUN.** This is a prepared record. The manual vanilla-client play
session below has not been performed, and every scenario is unticked. Fill it
in when the session is run; until then M6.1 is "Client checks pending", not
Complete.

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

The strict generated decode has been proven only against the pinned Node
loopback interop lane, **not** against a real client. That is what the session
below exists to establish.

## Session record (to be filled in)

Run a vanilla 1.8.9 client against a server started with `-online-mode=false`
and drive a full play session. Record the client build, and for every scenario
record the result and every decode error observed (record zero if there are
none).

| Field | Value |
| --- | --- |
| Date run | _not run_ |
| Client build | _not run_ |
| Server mode | _e.g. offline, compression 256_ |
| Total decode errors | _not run_ |

### Scenario checklist

- [ ] **Join** — decode errors: _n/a, not run_
- [ ] **Move** — decode errors: _n/a, not run_
- [ ] **Break a block** — decode errors: _n/a, not run_
- [ ] **Place a block** — decode errors: _n/a, not run_
- [ ] **Open a chest** — decode errors: _n/a, not run_
- [ ] **Craft in the 2x2 grid** — decode errors: _n/a, not run_
- [ ] **Craft in the 3x3 grid** — decode errors: _n/a, not run_
- [ ] **Shift-click the crafting output** — decode errors: _n/a, not run_
- [ ] **Take damage** — decode errors: _n/a, not run_
- [ ] **Die** — decode errors: _n/a, not run_
- [ ] **Respawn** — decode errors: _n/a, not run_
- [ ] **Chat** — decode errors: _n/a, not run_
- [ ] **Run a command with tab completion** — decode errors: _n/a, not run_
- [ ] **Disconnect** — decode errors: _n/a, not run_

### Decode errors observed

_None recorded yet — the session has not been run. Record each rejected packet
here with its packet name and the codec error, then fix the codec in
`minecraft-protocol`, add a byte fixture there, and re-run._

### Notes

_Record here any scenario the server does not implement (mark N/A rather than
leaving it looking unrun, as M3's record did for chests and environmental
damage), and any gameplay defect the session surfaces._
