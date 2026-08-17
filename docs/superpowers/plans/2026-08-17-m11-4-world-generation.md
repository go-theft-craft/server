# M11.4 World Generation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **Status: complete, 2026-08-17.** The four inherited chunk hashes are
> unchanged, so the defaults are what the constants were. The milestone record
> in `MASTER_PLAN.md` has the parameter count and the four knobs that turned
> out to be entangled.

**Goal:** Turn the two hardcoded generators into named types built from typed parameters, resolved through a registry an application can extend, with a determinism contract a test enforces and a record in `level.dat` of what generated a world.

**Architecture:** A `Factory` owns a name, its default parameters, a parser, and a constructor. A `Registry` value — not a package global — maps names to factories and is passed through `server.WithGeneratorRegistry`. Parameters are typed structs with JSON tags that round-trip through the world's own metadata, and every block they name is a canonical name resolved once at construction into an M11.2 state handle.

**Tech Stack:** Go 1.26.6 via `openserbia/go-flake`, Devbox, Task, `minecraft-protocol` v0.2.0, vendored dependencies.

## Global Constraints

- Work in `/home/ocharnyshevich/pet.projects/go-theft-craft/server`.
- Run every command as `devbox run -- task <name>`. Never call `go` directly.
- This repository is public. Do not name the private proxy project, its protocol, or its codename in any file or commit message.
- **Terrain must not move in Task 1.** The golden table M11.2 checked in is the contract: defaults produce the chunks the pre-M11.4 generators produced, hash for hash. It may be regenerated only in Task 6, deliberately, in the commit that changes an algorithm.
- Never add the `Co-Authored-By` or `Claude-Session` trailer to a commit message.
- Run `devbox run -- task lint` and `devbox run -- task test` before every commit.

## Dependencies

**M11.2**, for `world.State`, `world.Builder`, and the state registry that
generators resolve names through. It also wrote the golden generator table
this milestone inherits as its safety net.

**M11.3** is not a hard dependency, but `level.dat` is where Task 5 records the
generator's name, parameters, and version. If M11.3 has not landed, Task 5
writes to `world.json` instead and M11.3 carries it forward; the plan says
which in Step 1 of that task.

The design this plan implements is
[the M11.4 world generation design](../specs/2026-08-17-m11-4-world-generation-design.md),
2026-08-17.

## What is hardcoded today

| Value | Where |
| --- | --- |
| Sea level 62, ~20 block IDs, log and leaf variants | `pkg/world/gen/flat.go:3-40` |
| Six ore entries: block, Y range, vein size, attempts per chunk | `pkg/world/gen/ores.go:21` |
| Eleven biome IDs and their temperature and rainfall thresholds | `pkg/world/gen/biome.go` |
| Noise scales: terrain `seed`, detail `seed+1`, temperature `seed+100`, rainfall `seed+200` | `pkg/world/gen/default.go:14`, `biome.go:26` |
| Superflat's four layers | `pkg/world/gen/flat.go:57-61` |
| Tree and vegetation density per biome | `pkg/world/gen/trees.go` |
| Generator selection: a two-case `switch` on `config.GeneratorType` | `server/server.go` |

## File Structure

**Created:**

| File | Responsibility |
| --- | --- |
| `pkg/world/gen/params.go` | `Params`, `Factory`, `Registry`, `DefaultRegistry` |
| `pkg/world/gen/params_test.go` | Round-trip, unknown-key rejection, duplicate registration |
| `pkg/world/gen/default_params.go` | `DefaultParams` and its defaults |
| `pkg/world/gen/flat_params.go` | `FlatParams`, layers, and its defaults |
| `pkg/world/gen/determinism_test.go` | The golden table, inherited from M11.2 |
| `pkg/world/gen/testdata/golden.json` | Chunk hashes per generator, seed, and parameter set |
| `examples/custom/main.go` | A server registering its own named generator |

**Modified:**

| File | Change |
| --- | --- |
| `pkg/world/gen/default.go` | Built from `DefaultParams`; passes fell out of constants |
| `pkg/world/gen/flat.go` | Built from `FlatParams`; the layer list replaces four `Set` calls |
| `pkg/world/gen/{surface,caves,ores,trees,biome}.go` | Take their parameters rather than reading package constants |
| `server/server.go` | Registry lookup replaces the `switch`; unknown name is an error |
| `server/options.go` | `WithGeneratorRegistry`, `WithGeneratorNamed` |
| `config/config.go` | `GeneratorParams json.RawMessage` beside `GeneratorType` |
| `examples/flat/main.go` | Demonstrates a layer list rather than the built-in flat |
| `README.md` | Generator section |

---

## Stage A — Parameters that change nothing

### Task 1: Promote the constants, defaults byte-identical

**Files:**
- Create: `pkg/world/gen/params.go`, `default_params.go`, `flat_params.go`, `params_test.go`
- Modify: `default.go`, `flat.go`, `surface.go`, `caves.go`, `ores.go`, `trees.go`, `biome.go`

**Interfaces:**
- Produces: `gen.Params`, `gen.DefaultParams`, `gen.FlatParams`, `gen.NewDefault(seed int64, p DefaultParams, reg world.StateRegistry) (Generator, error)`, same for flat.

- [x] **Step 1: Confirm the golden table is green before touching anything**

```bash
devbox run -- go test -mod vendor -run TestGeneratorGolden ./pkg/world/gen/
```

Expected: PASS. This table came from M11.2 and it is the only thing that will
say whether a transcribed constant is wrong. If it is red before this
milestone starts, stop: something in M11.2 or M11.3 moved terrain and that has
to be understood first.

- [x] **Step 2: Write the parameter types**

```go
type DefaultParams struct {
    SeaLevel     int         `json:"sea_level"`      // 62
    TerrainScale float64     `json:"terrain_scale"`
    DetailScale  float64     `json:"detail_scale"`
    Caves        CaveParams  `json:"caves"`
    Ores         []OreParams `json:"ores"`
    Trees        TreeParams  `json:"trees"`
    Biomes       BiomeParams `json:"biomes"`
}

type OreParams struct {
    Block    string `json:"block"`     // "minecraft:diamond_ore"
    MinY     int    `json:"min_y"`
    MaxY     int    `json:"max_y"`
    VeinSize int    `json:"vein_size"`
    Attempts int    `json:"attempts"`  // veins per chunk
}
```

`DefaultDefaults()` transcribes the six-entry `ores` table, sea level 62, and
every noise scale. Ore blocks are named, not numbered: `blockDiamondOre` (56)
becomes `"minecraft:diamond_ore"`, resolved through the registry M11.2 built.

- [x] **Step 3: Convert one sub-generator per commit**

Order: ores, caves, surface, trees, biomes. Each takes its parameters as a
struct field rather than reading package constants, and each is followed by:

```bash
devbox run -- go test -mod vendor -run TestGeneratorGolden ./pkg/world/gen/
```

A hash that moves means a transcription error in that file, and the whole
point of the ordering is that you know which one.

- [x] **Step 4: Convert flat to a layer list**

```go
func FlatDefaults() FlatParams {
    return FlatParams{
        Layers: []FlatLayer{
            {Block: "minecraft:bedrock", Thickness: 1},
            {Block: "minecraft:stone", Thickness: 2},
            {Block: "minecraft:dirt", Thickness: 1},
            {Block: "minecraft:grass", Thickness: 1},
        },
        Biome: "minecraft:plains",
    }
}
```

`HeightAt` becomes the sum of the layer thicknesses minus one, which for the
defaults is 4 — the value it returns today.

- [x] **Step 5: Gate and commit**

```bash
devbox run -- task lint
devbox run -- task test
git add -A
git commit -m "refactor(gen): promote the hardcoded constants to typed parameters

The golden chunk hashes are unchanged, which is the whole claim: the
defaults are what the constants were."
```

---

## Stage B — Named types

### Task 2: Factories and a registry that is not a global

**Files:**
- Modify: `pkg/world/gen/params.go`
- Modify: `server/server.go`, `server/options.go`, `config/config.go`

**Interfaces:**
- Produces: `gen.Factory`, `gen.Registry`, `gen.DefaultRegistry()`, `server.WithGeneratorRegistry`, `server.WithGeneratorNamed`.

- [x] **Step 1: Write the failing tests**

```go
func TestDefaultRegistryHasDefaultAndFlat(t *testing.T)
func TestRegisteringADuplicateNameErrors(t *testing.T)
func TestTwoRegistriesDoNotShareRegistrations(t *testing.T)
func TestAnUnknownGeneratorNameIsAConstructionError(t *testing.T)
func TestParamsRoundTripThroughJSON(t *testing.T)
func TestAnUnknownParameterKeyIsRejected(t *testing.T)
```

`TestTwoRegistriesDoNotShareRegistrations` is why the registry is a value: the
interoperability lane runs servers side by side in one test binary, and a
package-level map would let one test's generator leak into another's.

`TestAnUnknownGeneratorNameIsAConstructionError` is a behavior change worth
naming: today `server.New` falls through a `switch` to the noise generator, so
`-generator flta` silently gives you default terrain.

- [x] **Step 2: Implement**

```go
type Factory interface {
    Name() string
    Version() int                      // the generator version, see Task 5
    Defaults() Params
    Parse(raw json.RawMessage) (Params, error)
    New(seed int64, p Params, reg world.StateRegistry) (Generator, error)
}

type Registry interface {
    Register(f Factory) error
    Lookup(name string) (Factory, bool)
    Names() []string
}
```

`Parse` uses a decoder with `DisallowUnknownFields`, which is what makes a
typo in a parameter file an error rather than a silently ignored line.

- [x] **Step 3: Replace the switch**

```go
// Before
switch b.settings.GeneratorType {
case config.GeneratorFlat:
    generator = gen.NewFlatGenerator(b.settings.Seed)
default:
    generator = gen.NewDefaultGenerator(b.settings.Seed)
}

// After
factory, ok := b.registry.Lookup(b.settings.GeneratorType)
if !ok {
    return nil, fmt.Errorf("%w: unknown generator %q, have %v",
        ErrInvalidOption, b.settings.GeneratorType, b.registry.Names())
}
```

The error names the registered generators, because the first thing anyone does
with an unknown-name error is ask what the known ones are.

- [x] **Step 4: Carry parameters through config**

```go
GeneratorType   string          `json:"generator_type"`
GeneratorParams json.RawMessage `json:"generator_params,omitempty"`
```

Raw JSON because `config.Config` cannot name a type the application
registered. `config.Merge` gains the field, following the existing pattern of
"only when not set by a flag".

- [x] **Step 5: Gate and commit**

```bash
devbox run -- task lint
devbox run -- task test
devbox run -- task test:examples
git add -A
git commit -m "feat(gen): resolve generators by name through a registry"
```

### Task 3: An example that registers its own

**Files:**
- Create: `examples/custom/main.go`
- Modify: `examples/examples_test.go`, `Taskfile.yml`

**Interfaces:**
- Consumes: `gen.Factory`, `gen.Registry`, `server.WithGeneratorRegistry`.

- [x] **Step 1: Write the example**

A generator worth about forty lines — checkerboard columns of two blocks at a
parameterized period, say — registered under `"checker"` with its own
`CheckerParams`. It exists to prove the extension point from outside the
module, which is the only way to prove it: `examples/` is a separate module
with a `replace`, so it sees exactly what an external consumer sees.

- [x] **Step 2: Add it to the examples lane**

`examples_test.go`'s list becomes `{"minimal", "flat", "vanilla", "custom"}`
with port 25704, and `task build` builds four binaries.

- [x] **Step 3: Gate and commit**

```bash
devbox run -- task lint
devbox run -- task test
devbox run -- task test:examples
git add -A
git commit -m "feat(examples): register a named generator from outside the module"
```

---

## Stage C — Contracts

### Task 4: The determinism contract

**Files:**
- Modify: `pkg/world/gen/determinism_test.go`, `pkg/world/gen/testdata/golden.json`

- [x] **Step 1: Extend the table to cover parameters**

M11.2's table covers the two generators at fixed seeds. It grows a dimension:
each entry is `(generator, version, seed, params hash) → chunk hashes`, so a
parameter change that alters terrain is caught rather than absorbed.

- [x] **Step 2: Make regeneration deliberate**

```bash
devbox run -- go test -mod vendor -run TestGeneratorGolden -update ./pkg/world/gen/
```

The `-update` flag is the same convention the byte-parity fixtures use, and
the test's failure message says so, including the sentence that regenerating
means terrain moved and the commit message has to say why.

- [x] **Step 3: Assert determinism across processes**

Same seed and parameters, generated in this process and compared against the
table, plus a subtest that runs the generator twice in one process and
compares — which catches a generator that accumulated state between chunks,
the failure a per-run hash would miss.

- [x] **Step 4: Gate and commit**

```bash
devbox run -- task lint
devbox run -- task test
git add -A
git commit -m "test(gen): pin terrain to a golden table per seed and parameter set"
```

### Task 5: The world records what generated it

**Files:**
- Modify: `server/server.go`, `internal/server/storage/*`, `pkg/world/gen/params.go`

**Interfaces:**
- Consumes: `LevelData` from M11.3, or `world.json` if M11.3 has not landed.

- [x] **Step 1: Decide where it goes**

If M11.3 has landed, `LevelData` gains `GeneratorName`, `GeneratorVersion`,
and `GeneratorParams`. If it has not, the same three fields go into
`world.json` and M11.3 carries them into `LevelData` unchanged. Record which
in the commit message so the next milestone does not have to infer it.

- [x] **Step 2: Write the failing tests**

```go
func TestANewWorldRecordsItsGenerator(t *testing.T)
func TestAWorldWithNoGeneratorRecordAdoptsTheConfiguredOne(t *testing.T)
func TestAGeneratorVersionMismatchWarnsAndContinues(t *testing.T)
func TestAGeneratorNameMismatchWarnsAndUsesTheWorlds(t *testing.T)
```

The third and fourth are where the judgment is. A version mismatch keeps
generating with the new version, because regenerating old chunks would rewrite
terrain someone has built on. A **name** mismatch — the world says `flat`, the
config says `default` — uses the world's, because the alternative is
superflat's grass plane growing mountains at the edge of what has been
explored.

- [x] **Step 3: Implement**

At startup: read the record, compare with the configured generator, log the
comparison, and construct from the world's record when they disagree. A world
with no record — every world that exists today — adopts the configured
generator and writes it, which is the migration path and needs no separate
step.

- [x] **Step 4: Gate and commit**

```bash
devbox run -- task lint
devbox run -- task test
devbox run -- task test:examples
git add -A
git commit -m "feat(world): record the generator, its version, and its parameters"
```

### Task 6: Documentation and the milestone record

**Files:**
- Modify: `README.md`, `CLAUDE.md`, `../headless-minecraft/MASTER_PLAN.md`

- [x] **Step 1: Document the parameter surface**

`README.md` gains a generator section: the two named types, their parameters
with defaults, how to put them in `config.json`, and how an application
registers its own with a pointer at `examples/custom`.

The honest note goes here too: this generator does not reproduce vanilla
terrain and does not try, which the design lists as a non-goal and which a
README that lists "procedural world generation" as a feature currently leaves
someone to discover.

- [x] **Step 2: Record the milestone**

In `MASTER_PLAN.md`, tick M11.4 and record:

- whether the defaults really were byte-identical, or whether a constant was
  mistranscribed and the golden table caught it, which is the measurement of
  whether that safety net was worth building;
- what the parameter surface ended up being — how many knobs, and which ones
  turned out to be entangled rather than independent;
- whether the name-mismatch rule in Task 5 is the right one, which will only
  be known the first time someone hits it.

- [x] **Step 3: Final gate and commit**

```bash
devbox run -- task lint
devbox run -- task test
devbox run -- task test:examples
devbox run -- task test:interop
git add -A
git commit -m "docs: record M11.4 and document the generator parameters"
```

---

## Self-review notes

- **Task 1 is the whole milestone's risk and it is first.** Forty constants
  moving into structs is exactly the kind of change that looks mechanical and
  is not, and the sub-generator-per-commit ordering exists so a moved hash
  points at one file.
- **The golden table is inherited rather than written here.** That was M11.2's
  Step 1 and it is worth checking it exists before starting: this plan's first
  command is a test run, not an edit.
- **`GeneratorParams` as `json.RawMessage` in `config.Config` is a wart.** The
  alternative is a generic config type, which pushes the same problem up a
  level. Raw JSON parsed by the factory that owns the schema is the smaller
  compromise, and it is the same shape `level.dat` needs anyway.
- **The name-mismatch rule changes behavior for anyone who has been switching
  `-generator` on an existing world.** Today that silently generates new
  terrain in the new style beside the old; after Task 5 it warns and keeps the
  old. That is better and it is still a change someone will notice.
- **`HeightAt` and `Generate` can still disagree**, because `HeightAt`
  recomputes rather than reading the generated chunk, and caves carved after
  the height pass make it wrong at a cave mouth. This milestone does not fix
  it. The design says so and the test documents the gap; it belongs to whoever
  owns item landing next.
