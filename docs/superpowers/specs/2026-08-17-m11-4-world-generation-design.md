# M11.4 world generation design

- Status: Draft for review
- Date: 2026-08-17
- Repository: `server`
- Milestone: M11.4, the fourth sub-milestone of
  [the server framework track](2026-08-16-server-framework-design.md)

## Context

`gen.Generator` is already a seam, and M11.1 made it a constructor option
(`server.WithGenerator`). What it is not is configurable, describable, or
version-neutral.

What exists today in `pkg/world/gen`:

- `DefaultGenerator`, built from a seed alone, running four passes: terrain
  and biomes, caves, ores, trees (`default.go:25`).
- `FlatGenerator`, four hardcoded layers, ignoring its seed argument entirely
  (`flat.go:48`).
- Every tunable is a package constant or a package variable: `seaLevel = 62`,
  the eleven biome IDs, and the six-entry `ores` table with per-ore Y range,
  vein size, and attempts per chunk (`ores.go:21`).
- Every block is written as `blockStone<<4`, the protocol 47 encoding, which
  M11.2 replaces with interned handles.
- The application picks a generator by string: `config.GeneratorType` is
  `"default"` or `"flat"`, resolved in `server.New` with a two-case switch.

The immediate consequence is that a world cannot record how it was generated.
`level.dat` in M11.3 has a field for the generator's name and parameters, and
today there would be nothing to put in it beyond a seed and one of two words.

## Goals

- A generator described by typed parameters that round-trip through the world's
  own metadata, so a world regenerates the terrain it had.
- Named world types resolved through a registry rather than a switch, so an
  application adds one without editing the framework.
- Generators that emit version-neutral state handles.
- A determinism contract that a test enforces.

## Non-goals

- Vanilla parity. This generator has never claimed to reproduce Mojang's
  terrain and this milestone does not start. `minecraft-reference` and
  `minecraft-simulation` are where vanilla ground truth lives, and none of it
  is terrain.
- Structures — villages, mineshafts, strongholds. The parameter surface leaves
  room for a structure pass; building one is a later milestone.
- A separate generator repository, settled by parent Decision 9.
- 3D biomes. M11.2's dimension descriptor is where that changes, and no
  generator wants it yet.

## Decision 1: a generator is a named type plus typed parameters

```go
// Params is a generator's configuration. Each named type defines its own
// concrete type; the interface exists so level metadata can carry any of them.
type Params interface {
    // Type is the registered name, e.g. "default" or "flat".
    Type() string
}

// Factory builds a generator from parameters and a seed.
type Factory interface {
    Name() string
    // Defaults returns freshly allocated default parameters, which is also
    // what documents the surface: a caller marshals them to see every knob.
    Defaults() Params
    // Parse decodes parameters from the form level.dat stored them in.
    Parse(raw json.RawMessage) (Params, error)
    New(seed int64, p Params) (Generator, error)
}
```

Parameters are typed structs with JSON tags rather than `map[string]any`. A
typed struct is checked at compile time in the examples, documents itself
through `Defaults`, and gives `Parse` somewhere to reject an unknown key
instead of ignoring it.

`DefaultParams` starts as exactly what is hardcoded today, promoted field by
field:

```go
type DefaultParams struct {
    SeaLevel     int         `json:"sea_level"`      // 62
    TerrainScale float64     `json:"terrain_scale"`  // the noise divisor
    Caves        CaveParams  `json:"caves"`          // density, radius, Y range
    Ores         []OreParams `json:"ores"`           // the six-entry table, by block name
    Trees        TreeParams  `json:"trees"`          // density per biome
    Biomes       BiomeParams `json:"biomes"`         // temperature and rainfall scales
}
```

`OreParams.Block` is a block **name** — `"minecraft:diamond_ore"` — not an ID.
That is what makes the parameter file survive a version change, and it is the
same identity M11.2's registry interns.

`FlatParams` is a layer list, which is what superflat is in vanilla:

```go
type FlatParams struct {
    Layers []FlatLayer `json:"layers"` // block name and thickness, bottom up
    Biome  string      `json:"biome"`  // "minecraft:plains"
}
```

That turns `FlatGenerator`'s four hardcoded `SetBlock` calls into data, and it
gives the `flat` example something to demonstrate beyond "a generator can be
substituted".

## Decision 2: named types live in a registry the application can extend

```go
// Registry resolves a generator name to its factory. The framework registers
// "default" and "flat"; an application registers its own before New.
type Registry interface {
    Register(f Factory) error   // errors on a duplicate name
    Lookup(name string) (Factory, bool)
    Names() []string
}

func DefaultRegistry() Registry   // "default" and "flat", freshly built
```

A registry is a mutable global in most Go projects and is not one here: a
`Registry` value is passed through `server.WithGeneratorRegistry`, defaulting
to `DefaultRegistry()`. A package-level `init()`-populated map would make two
servers in one process share generator names, and the interoperability lane
already runs servers side by side in one test binary.

The switch in `server.New` becomes a lookup, and an unknown name is an error at
construction rather than a silent fall-through to `default` — which is what the
current `switch` does, so a typo in `generator` today gives you noise-terrain
without saying so.

`server.WithGenerator(g)` stays, and stays the escape hatch: an application
with a generator it does not want to name or parameterize passes the value
directly, as `examples/flat` does today.

## Decision 3: generators resolve block names once, at construction

M11.2 gives every generator a `StateRegistry`. `New(seed, params)` gains it,
and each generator resolves the names it will use into handles once:

```go
type surfacePalette struct {
    air, stone, dirt, grass, sand, gravel, water, bedrock world.State
}

func newSurfacePalette(reg world.StateRegistry) surfacePalette { ... }
```

Then `fillColumn` writes handles. The 20-odd `block*` constants in `flat.go`
disappear, and with them the `<<4` that encodes protocol 47 into terrain.

A name a registry does not know is an error at construction, not at chunk
generation. A generator that fails on the ten-thousandth chunk because a block
name was wrong is a generator that fails in production rather than in a test.

## Decision 4: determinism is a contract with a golden test

Same version, same seed, same parameters, same chunk bytes. Every generator
must satisfy it and one test enforces it: generate a fixed set of chunks at a
fixed seed and compare a hash per chunk against a checked-in table.

The contract is deliberately scoped to a version rather than forever. Changing
the terrain algorithm changes the hash, and that is allowed — with the same
rule the byte-parity fixtures already carry: the table is regenerated
deliberately, in the commit that changes the algorithm, and the commit message
says the terrain moved. What is not allowed is discovering it later, on a
player's world.

This is also what makes `level.dat` meaningful. It stores the generator name,
its parameters, and a **generator version** integer the factory owns. A world
generated with `default` v1 and opened by a server shipping `default` v2 gets
a loud warning and keeps generating with v2, because regenerating old chunks
silently would rewrite terrain a player has already built on. The alternative,
refusing to open the world, was rejected as worse: nobody thanks a server that
will not start.

## Decision 5: the generator produces a builder, not a chunk

M11.2 defines `Generate(pos ChunkPos, into *Builder) error`. Generation is the
one place mutation is right — nothing else can see the chunk yet — and a
generator that wrote 65,536 blocks through the immutable path would copy a
section per block.

Two consequences worth stating:

- `Builder` bounds writes by the dimension descriptor, so a generator writing
  outside `MinY..MinY+Height` gets an error rather than a silent drop. The
  current `SetBlock` silently ignores `y >= 256` through Go's array bounds
  only because the array is exactly 16 sections.
- `Generate` returns an error, which today's `Generate` cannot. A generator
  that cannot produce a chunk should not return an empty one that looks like a
  void world.

`HeightAt` stays, because spawn placement and item-drop landing both use it
(`World.SpawnHeight`, and the `groundAt` callback in
`player.SpawnItemEntity`). It is a query about terrain rather than a chunk, and
generating a full chunk to answer it is what the callback was written to
avoid.

## Interfaces

```go
package gen

type Generator interface {
    Generate(pos world.ChunkPos, into *world.Builder) error
    HeightAt(x, z int) int
}

package server

func WithGeneratorRegistry(r gen.Registry) Option
func WithGeneratorNamed(name string, params gen.Params) Option // resolved at New
func WithGenerator(g gen.Generator) Option                     // unchanged
func WithSeed(seed int64) Option                               // unchanged
```

`config.Config` grows a parameters field beside its generator name:

```go
GeneratorType   string          `json:"generator_type"`
GeneratorParams json.RawMessage `json:"generator_params,omitempty"`
```

Raw JSON in config rather than a typed field, because the config struct cannot
name a type the application registered. It is parsed by the factory the name
resolves to, and a config with parameters for a generator that is not
registered is a startup error.

## Migration

1. Parameters and factories for `default` and `flat`, with the current
   constants as defaults, and a test asserting the defaults produce
   byte-identical chunks to today's generators.
2. The registry, the `server` options, and the config field. The two-case
   switch is deleted.
3. Block names replace block constants, on top of M11.2's registry.
4. `Builder` output, bounds checking, and the error return.
5. The determinism table, generated once and checked in.
6. `level.dat` carries name, parameters, and generator version; the mismatch
   warning lands with it.

Step 1 is the one that has to be exactly right. The current world of anyone
running this server was generated by the current constants, and a default that
differs by one changes their terrain at the next chunk they walk into.

## Testing

- Defaults produce byte-identical chunks to the pre-M11.4 generators, for a
  fixed seed across a fixed chunk set. This is the migration's whole safety
  argument.
- The determinism table: same seed and parameters, same hashes, run twice in
  one process and once in a fresh one.
- Parameters round-trip: `Defaults()` → JSON → `Parse` → `New` produces a
  generator whose output matches the one built from `Defaults()` directly.
- An unknown key in a parameter document is rejected, not ignored.
- An unknown block name fails at `New`, not at `Generate`.
- A flat generator built from a layer list matches the current hardcoded
  bedrock/stone/stone/dirt/grass column.
- A generator writing outside the dimension's Y range gets an error.
- `HeightAt` agrees with the top solid block of the generated column, checked
  across biomes, because item pickup depends on it
  (`internal/server/player/item_entity.go`).

## Risks

**Promoting constants to parameters changes terrain if a default is
mistranscribed.** Six ore entries, eleven biome thresholds, and a handful of
noise scales are all copied by hand. The byte-identical test in migration step
1 is the only thing that catches a wrong digit, and it has to run before any
other change in this milestone.

**Parameters are a compatibility surface.** Once a world stores them, renaming
a JSON field breaks it. They are versioned with the generator version from
Decision 4, and the plan should say plainly that adding a field is cheap and
renaming one is not.

**`HeightAt` and `Generate` can disagree.** They already can — `HeightAt`
recomputes terrain height rather than reading the generated chunk — and caves
carved after the height pass make the answer wrong at the surface of a cave
mouth. This milestone does not fix it, and the test above will document the
gap rather than hide it.

## Exit criteria

| | Criterion |
| --- | --- |
| 1 | `default` and `flat` are built from typed parameters whose defaults reproduce today's terrain byte for byte |
| 2 | A generator is resolved by name through a registry, and an unknown name is a startup error |
| 3 | An application registers its own named generator without editing the framework |
| 4 | No block ID or `<<4` remains in `pkg/world/gen` |
| 5 | The determinism table passes, and regenerating it requires an explicit flag |
| 6 | `level.dat` records name, parameters, and generator version, and a version mismatch warns without refusing to start |
