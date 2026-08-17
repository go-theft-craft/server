# M11.7 commands design

- Status: Draft for review
- Date: 2026-08-17
- Repository: `server`
- Milestone: M11.7, the seventh sub-milestone of
  [the server framework track](2026-08-16-server-framework-design.md)

## Context

Commands are the one seam in the track that is currently not a seam at all.

`internal/server/conn/commands.go` holds a package-level `commands` slice
populated in `init()`, ten entries, each a struct with a name, a usage string,
a description, and `handler func(c *Connection, args []string)`. Dispatch is a
linear scan in `handleCommand`. A command therefore has the whole connection —
its stream, its player, its world — and an application outside this repository
cannot add one, because `Connection` is internal and `commands` is unexported.

Argument completion is a second, parallel encoding of the same knowledge.
`tab_complete.go` has a `switch cmdName` with a case per command and a case per
argument index: `/tp` argument 1 is a player name, arguments 2 and 3 are
coordinates, `/gamemode` argument 1 is one of four words, `/time` argument 1 is
`set` and argument 2 is one of four words. Every one of those facts is also
implied by the handler that parses them, and nothing keeps the two in step.

Two protocol facts bound the design, both checked against
`minecraft-protocol` v0.2.0:

- Java 1.8 ships **no** command tree. There is no `commands.go` in
  `generated/java/v1_8`, and protocol 47 has no `DeclareCommands` packet. The
  only suggestion channel is `TabComplete`, request and response.
- Java 26.1 ships one: `generated/java/v26_1/commands.go` is 198 KB of
  brigadier tree, `data.Set.Commands()` returns it, and
  `PlayClientboundDeclareCommands` is packet 0x10.

So the same command set has to produce a brigadier tree on one version and a
list of strings on the other, and only one of those two has a canonical source
to check against.

## Goals

- A command as a value an application can write, register, and test without a
  connection.
- One declaration of a command's arguments that drives execution, completion,
  and — on 775 — the brigadier tree.
- `vanilla.Stubs()`: every vanilla command present and honestly reporting
  itself unimplemented.
- Callers that are not players.

## Non-goals

- Implementing vanilla command behavior. Stubs are the deliverable; parent
  Decision 10 and the track's non-goals both say so.
- A permission system with groups, inheritance, or persistence. Decision 5
  ships an integer level and an interface, which is the minimum that lets an
  application bring its own.
- A console. `Caller` makes a non-player caller expressible; reading stdin is
  an application's business, and `examples/vanilla` is where one would go.
- Command blocks, `/execute` rewriting, or selector evaluation beyond what
  Decision 3 defines.

## Decision 1: a command is a value with a declared signature

```go
type Command interface {
    Name() string
    Aliases() []string
    // Signature declares the argument shape once. Execution, completion, and
    // brigadier rendering all read it, so they cannot disagree.
    Signature() Signature
    Run(ctx context.Context, caller Caller, args Args) error
}

type Set interface {
    Lookup(name string) (Command, bool)  // by name or alias
    All() []Command
}
```

`Signature` is a list of overloads, each a list of parameters:

```go
type Signature struct {
    Overloads []Overload
    Level     PermissionLevel
}

type Overload struct{ Params []Param }

type Param struct {
    Name     string
    Type     ParamType   // word, message, int, float, coordinates, player, ...
    Optional bool
    // Choices fixes the accepted values, which is what turns
    // /gamemode into four suggestions rather than free text.
    Choices  []string
}
```

Overloads are how `/tp <player>` and `/tp <x> <y> <z>` stop being a length
check inside a handler and start being two declared shapes. That is the same
distinction brigadier makes, so the 775 rendering falls out rather than being
invented.

`Args` is the parsed result, not `[]string`: `args.Player(0)`, `args.Int(1)`,
`args.Coordinates(1)`. Parsing happens once, in the dispatcher, against the
signature. A handler that re-parses is a handler that disagrees with
completion.

## Decision 2: the caller is an interface, and it is not a connection

```go
type Caller interface {
    Name() string
    UUID() (uuid.UUID, bool)          // false for a non-player caller
    Level() PermissionLevel
    Position() (Position, bool)       // false when the caller is not in a world
    World() string
    // Reply sends a message back to whoever called. A player sees chat; a
    // console sees a line; a script collects it.
    Reply(msg Message)
}
```

This is what makes a command testable without a socket. Every one of today's
ten handlers reaches into `*Connection` for exactly these five things — the
player's name, position, and gamemode, the player manager, and a chat send —
and four of the five are already available on `player.Player`.

`Message` is structured rather than a preformatted JSON string. Today
`sendSystemMsg` formats `{"text":...,"color":...}` by hand at the call site,
which means every command hardcodes protocol 47's chat encoding. A structured
message rendered by the version adapter is the same boundary M11.2 draws for
blocks, applied to text.

The services a command needs beyond the caller — the world, the player list, a
save trigger — arrive in the context through a narrow accessor rather than as
a fat struct:

```go
type Services interface {
    World() *world.World
    Players() PlayerList
    Save(ctx context.Context) error
}

func ServicesFrom(ctx context.Context) Services
```

Narrow because `/save` needs a save trigger and nothing else, and a command
that receives the whole server acquires reasons to reach further.

## Decision 3: completion is derived from the signature, and the version renders it

The `switch cmdName` in `tab_complete.go` is deleted. Completion becomes:

1. Split the input, find the command by name, and pick the overloads that
   still match.
2. For the argument position under the cursor, ask its `ParamType` for
   candidates.
3. Filter by the partial the player has typed.

Candidate sources per type: `Choices` for a fixed set, the online player list
for `player`, the caller's own position for `coordinates` — which is what the
current code does and what its comment defends — and nothing for free text.

Two behaviors from the current implementation are kept deliberately, because
they were chosen against the protocol's constraints and the comments in
`tab_complete.go` record why:

- Coordinates complete to the block the caller stands in, not to the block
  they are looking at, because a ray cast is not available and the standing
  position is the more useful of the two.
- A candidate that does not extend what was typed is dropped rather than sent
  empty, because an empty candidate takes a line in the client's suggestion
  list.

On protocol 775 the same signature renders to a brigadier node tree, with each
`ParamType` mapped to a `data.CommandParser` name. The mapping table is the
whole version boundary for commands, and it is checked against the tree
`v26_1.Data().Commands()` ships: for every command name present in both, the
parser names this repository emits for its arguments must be parsers vanilla
uses for arguments of that kind. That is a weaker check than equality — the
stubs' signatures are not vanilla's — and it is the strongest one available
without implementing the commands.

## Decision 4: `vanilla.Stubs()` is a curated list here, because 1.8 has no tree to read

For 775 the stub set could be generated from `data.Set.Commands()`. For 47 it
cannot: upstream ships no command data for 1.8, so there is nothing to read.

The list is therefore a table in this repository, checked in as data with a
source comment, covering the vanilla 1.8 command set — roughly sixty commands,
from `/achievement` through `/xp`. The exact list is pinned during
implementation from the vanilla server's own `/help` output rather than from
memory, and the plan should say that explicitly, because a stub list that is
quietly wrong is worse than a short one that is right.

```go
package vanilla

// Stubs returns every vanilla command, each returning ErrNotImplemented.
func Stubs() server.Set

// Implemented returns the subset this repository actually implements today:
// help, list, tp, gamemode, time, say, me, kill, seed, save.
func Implemented() server.Set

// Set composes sets, later entries overriding earlier ones by name, which is
// how an application replaces stubs one at a time.
func Merge(sets ...server.Set) server.Set
```

`ErrNotImplemented` carries the command name and produces a message that says
the server does not implement it, rather than "unknown command". The difference
matters to whoever is building the server: unknown means a typo, unimplemented
means a to-do.

## Decision 5: permissions are one integer and one hook

```go
type PermissionLevel int

const (
    LevelAll      PermissionLevel = 0 // anyone
    LevelModerate PermissionLevel = 2 // vanilla's op level for most commands
    LevelAdmin    PermissionLevel = 4 // stop, op, ban
)

// Authorizer decides whether a caller may run a command. The default grants
// everything, which is what the server does today.
type Authorizer interface {
    Allow(caller Caller, cmd Command) bool
}
```

The default authorizer grants everything, matching current behavior exactly:
this server has no op list, and adding one silently in a framework milestone
would lock people out of their own worlds. The vanilla levels are on the stub
signatures so an application that installs an `Authorizer` gets the vanilla
answer for free.

An unauthorized command replies with a refusal and is **not** completed:
suggestion is an information channel, and completing `/ban` for a player who
cannot run it tells them the server has one.

## Interfaces

```go
package server

func WithCommands(s Set) Option
func WithAuthorizer(a Authorizer) Option

// Dispatch runs a command line for a caller. Chat handling calls it; so can
// an application, which is what makes a console or an RCON adapter possible
// without a connection.
func (s *Server) Dispatch(ctx context.Context, caller Caller, line string) error

// Complete returns suggestions for a partial line.
func (s *Server) Complete(ctx context.Context, caller Caller, line string) []string
```

`conn` keeps the chat path and the `TabComplete` handler, and both become
three lines that build a `Caller` and call the server. The ten built-in
commands move to a `commands/builtin` package as `Command` values.

## Migration

1. `Command`, `Set`, `Signature`, `Args`, `Caller`, and `Message`, with the
   dispatcher and the parser, plus tests that run commands with no connection.
2. The ten existing commands move to `builtin`, one per commit or in small
   groups, with the existing command tests
   (`internal/server/conn/commands_test.go`, 440 lines) kept passing against
   the new path.
3. Completion derives from signatures; `tab_complete.go`'s switch is deleted
   and its tests (`tab_complete_test.go`, 157 lines) keep asserting the same
   suggestions.
4. `Message` rendering moves behind the version adapter; the hand-built chat
   JSON in `commands.go` goes away.
5. `vanilla.Stubs()`, `Merge`, and the permission levels.
6. Brigadier rendering and the parser-name table, checked against
   `v26_1.Data().Commands()`.

Steps 2 and 3 are the ones with a behavior risk: the existing tests are the
contract, and any suggestion that changes is a regression until someone argues
otherwise in the commit message.

## Testing

- Every command runs against a fake `Caller` with no connection, and asserts
  on `Reply` rather than on bytes.
- The existing command and tab-complete tests pass unchanged in behavior:
  same commands, same messages, same suggestions.
- Signature-driven completion agrees with the current `switch` for all ten
  commands at every argument index, which is a table test written from the
  current implementation before it is deleted.
- `/tp` overload resolution: one argument is a player, three are coordinates,
  two is an error naming both shapes.
- Every stub reports itself unimplemented, and no stub name collides with an
  implemented command.
- Every vanilla name in the curated list resolves through `Lookup`, including
  by alias (`/msg`, `/tell`, `/w`).
- An unauthorized caller gets a refusal and no suggestions.
- Parser mapping: for every command name present in both this repository's set
  and `v26_1.Data().Commands()`, the emitted parser names are ones the vanilla
  tree also uses.
- A rendered brigadier tree round-trips through
  `PlayClientboundDeclareCommands`'s codec, so the version boundary is proved
  against the generated codec rather than against a hand-written expectation.

## Risks

**The curated 1.8 list is the one piece of data with no upstream source.** It
is written by hand, and a wrong name is a stub that never fires or a command
that reports itself missing when vanilla has none. Pinning it from a vanilla
server's `/help` output during implementation is the mitigation, and it is a
manual step that the plan has to name rather than assume.

**Deleting the completion switch loses behavior nobody wrote down.** The
comments in `tab_complete.go` record two decisions and the tests record the
rest, which is why step 3 writes the table test from the current
implementation *before* deleting it.

**Brigadier rendering is unexercised.** Nothing in this repository speaks 775,
so the tree is checked against a codec and a data set, never against a client.
That limit is the same one M11.2 states for its 775 registry round-trip, and
it should be recorded in the milestone rather than discovered by whoever first
connects a modern client.

**A command API is a compatibility surface from the day it ships**, more than
the other seams, because it is the one an application writes code against
rather than plugs a value into. Overloads and `ParamType` are the parts most
likely to need extension; both are open lists, and adding to them is additive.

## Exit criteria

| | Criterion |
| --- | --- |
| 1 | Every vanilla command name resolves, and every unimplemented one says so rather than reporting itself unknown |
| 2 | The ten built-in commands run with no connection, against a fake caller |
| 3 | Completion is derived from signatures and matches today's suggestions for all ten |
| 4 | An application registers a command and replaces a stub without editing the framework |
| 5 | A command reply names no protocol 47 chat encoding at the call site |
| 6 | A brigadier tree rendered from the set round-trips through the 775 codec |
| 7 | Permissions default to granting everything, and an installed authorizer withholds both execution and suggestion |
