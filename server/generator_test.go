package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/go-theft-craft/server/config"
	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/pkg/world/gen"
	"github.com/go-theft-craft/server/server"
)

// TestAnUnknownGeneratorNameIsAConstructionError is a behaviour change worth
// naming: before M11.4 a switch fell through to the noise generator, so
// `-generator flta` silently gave you default terrain.
func TestAnUnknownGeneratorNameIsAConstructionError(t *testing.T) {
	settings := config.DefaultConfig()
	settings.GeneratorType = "flta"

	_, err := server.New(server.WithSettings(settings))
	if !errors.Is(err, server.ErrInvalidOption) {
		t.Fatalf("New returned %v, want ErrInvalidOption", err)
	}
	// The first thing anyone does with an unknown-name error is ask what the
	// known ones are.
	for _, name := range []string{"default", "flat"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error does not name the registered generator %q: %v", name, err)
		}
	}
}

func TestWithGeneratorNamedSelectsAndConfigures(t *testing.T) {
	params := gen.FlatParams{
		Layers: []gen.FlatLayer{
			{Block: "minecraft:bedrock", Thickness: 1},
			{Block: "minecraft:sand", Thickness: 5},
		},
		Biome: "minecraft:desert",
	}

	srv, err := server.New(server.WithGeneratorNamed("flat", params))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	w := srv.World()
	if got := w.Block(world.BlockPos{X: 0, Y: 5, Z: 0}); got != w.Registry().Intern("minecraft:sand", nil) {
		t.Errorf("y=5 is %d, want sand", got)
	}
	if got := w.SpawnHeight(); got != 6 {
		t.Errorf("SpawnHeight = %d, want 6 (five layers above bedrock, plus one)", got)
	}
}

func TestGeneratorParametersComeThroughSettings(t *testing.T) {
	settings := config.DefaultConfig()
	settings.GeneratorType = "flat"
	settings.GeneratorParams = json.RawMessage(
		`{"layers":[{"block":"minecraft:stone","thickness":3}],"biome":"minecraft:plains"}`,
	)

	srv, err := server.New(server.WithSettings(settings))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	w := srv.World()
	stone := w.Registry().Intern("minecraft:stone", nil)
	for y := range 3 {
		if got := w.Block(world.BlockPos{X: 1, Y: y, Z: 1}); got != stone {
			t.Errorf("y=%d is %d, want stone", y, got)
		}
	}
	if got := w.Block(world.BlockPos{X: 1, Y: 3, Z: 1}); got != w.Air() {
		t.Errorf("y=3 is %d, want air", got)
	}
}

func TestBadGeneratorParametersAreRejectedAtConstruction(t *testing.T) {
	settings := config.DefaultConfig()
	settings.GeneratorType = "default"
	settings.GeneratorParams = json.RawMessage(`{"sea_lvel": 40}`)

	if _, err := server.New(server.WithSettings(settings)); !errors.Is(err, server.ErrInvalidOption) {
		t.Fatalf("New returned %v, want ErrInvalidOption for a misspelled key", err)
	}
}

// checkerFactory is a generator registered from outside the gen package,
// which is the extension point WithGeneratorRegistry exists for.
type checkerFactory struct{}

type checkerParams struct {
	Period int    `json:"period"`
	A      string `json:"a"`
	B      string `json:"b"`
}

func (checkerParams) Type() string { return "checker" }

func (checkerFactory) Name() string { return "checker" }
func (checkerFactory) Version() int { return 1 }

func (checkerFactory) Defaults() gen.Params {
	return checkerParams{Period: 4, A: "minecraft:stone", B: "minecraft:sand"}
}

func (f checkerFactory) Parse(raw json.RawMessage) (gen.Params, error) {
	p, _ := f.Defaults().(checkerParams)
	if len(raw) == 0 {
		return p, nil
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}

	return p, nil
}

func (f checkerFactory) New(_ int64, p gen.Params, reg world.StateRegistry) (gen.Generator, error) {
	params, ok := p.(checkerParams)
	if !ok {
		return nil, errors.New("checker: wrong parameters")
	}
	a, ok := reg.TryIntern(params.A, nil)
	if !ok {
		return nil, errors.New("checker: no block named " + params.A)
	}
	b, ok := reg.TryIntern(params.B, nil)
	if !ok {
		return nil, errors.New("checker: no block named " + params.B)
	}

	return &checkerGenerator{period: params.Period, a: a, b: b}, nil
}

type checkerGenerator struct {
	period int
	a, b   world.State
}

func (g *checkerGenerator) Generate(pos world.ChunkPos, into *world.Builder) error {
	for x := range 16 {
		for z := range 16 {
			block := g.a
			if ((pos.X*16+x)/g.period+(pos.Z*16+z)/g.period)%2 != 0 {
				block = g.b
			}
			if err := into.Set(x, 0, z, block); err != nil {
				return err
			}
		}
	}

	return nil
}

func (g *checkerGenerator) HeightAt(int, int) int { return 0 }

func TestAnApplicationCanRegisterItsOwnGenerator(t *testing.T) {
	registry := gen.DefaultRegistry()
	if err := registry.Register(checkerFactory{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	srv, err := server.New(
		server.WithGeneratorRegistry(registry),
		server.WithGeneratorNamed("checker", checkerParams{Period: 2, A: "minecraft:stone", B: "minecraft:sand"}),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	w := srv.World()
	stone := w.Registry().Intern("minecraft:stone", nil)
	sand := w.Registry().Intern("minecraft:sand", nil)

	if got := w.Block(world.BlockPos{X: 0, Y: 0, Z: 0}); got != stone {
		t.Errorf("(0,0) is %d, want stone", got)
	}
	if got := w.Block(world.BlockPos{X: 2, Y: 0, Z: 0}); got != sand {
		t.Errorf("(2,0) is %d, want sand", got)
	}
}

// The world's record of what generated it.

// TestANewWorldRecordsItsGenerator: a world with no record adopts the
// configured generator and writes it, which is the migration path for every
// world that exists today.
func TestANewWorldRecordsItsGenerator(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	srv, store := newStoredServer(t, dir)
	if _, err := srv.Load(ctx); err != nil {
		t.Fatalf("Load: %v", err)
	}

	level, found, err := store.World().Level(ctx, server.DefaultWorld)
	if err != nil {
		t.Fatalf("Level: %v", err)
	}
	if !found {
		t.Fatal("a started world wrote no level data")
	}
	if level.GeneratorName != "flat" {
		t.Errorf("generator name = %q, want flat", level.GeneratorName)
	}
	if level.GeneratorVersion != gen.FlatVersion {
		t.Errorf("generator version = %d, want %d", level.GeneratorVersion, gen.FlatVersion)
	}
	if len(level.GeneratorParams) == 0 {
		t.Error("the record holds no parameters")
	}
}

// TestAGeneratorNameMismatchUsesTheWorlds is the judgment call: the world says
// flat, the configuration says default, and the world wins. The alternative is
// superflat's grass plane growing mountains at the edge of what has been
// explored.
func TestAGeneratorNameMismatchUsesTheWorlds(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// A flat world, recorded.
	first, _ := newStoredServer(t, dir)
	if _, err := first.Load(ctx); err != nil {
		t.Fatalf("Load: %v", err)
	}
	first.SaveAll()

	// Reopened with the configuration asking for the noise generator.
	settings := config.DefaultConfig()
	settings.GeneratorType = config.GeneratorDefault

	store, err := server.FileStore(dir, nil)
	if err != nil {
		t.Fatalf("FileStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	second, err := server.New(append(store.Options(), server.WithSettings(settings))...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	w := second.World()
	// Flat terrain: bedrock, two stone, dirt, grass, then air.
	if got := w.Block(world.BlockPos{X: 3, Y: 4, Z: 3}); got != w.Registry().Intern("minecraft:grass", nil) {
		t.Errorf("y=4 is %d, want the flat world's grass", got)
	}
	if got := w.Block(world.BlockPos{X: 3, Y: 20, Z: 3}); got != w.Air() {
		t.Errorf("y=20 is %d, want air — the noise generator built over the flat world", got)
	}
}

// TestAGeneratorVersionMismatchWarnsAndContinues: regenerating the old chunks
// would rewrite terrain someone has built on, so the running version keeps
// generating and the difference is visible in the log instead.
func TestAGeneratorVersionMismatchWarnsAndContinues(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	srv, store := newStoredServer(t, dir)
	if _, err := srv.Load(ctx); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Rewrite the record as if an older build had made this world.
	level, _, err := store.World().Level(ctx, server.DefaultWorld)
	if err != nil {
		t.Fatalf("Level: %v", err)
	}
	level.GeneratorVersion = gen.FlatVersion + 41
	if err := store.World().SaveLevel(ctx, server.DefaultWorld, level); err != nil {
		t.Fatalf("SaveLevel: %v", err)
	}

	logged := &strings.Builder{}
	settings := config.DefaultConfig()
	settings.GeneratorType = config.GeneratorFlat

	reopened, err := server.FileStore(dir, nil)
	if err != nil {
		t.Fatalf("FileStore: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })

	second, err := server.New(append(
		reopened.Options(),
		server.WithSettings(settings),
		server.WithLogger(slog.New(slog.NewTextHandler(logged, nil))),
	)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if !strings.Contains(logged.String(), "different version") {
		t.Errorf("a version mismatch was not logged: %s", logged.String())
	}
	// And the world still works, generated by the running version.
	if got := second.World().Block(world.BlockPos{X: 0, Y: 0, Z: 0}); got == second.World().Air() {
		t.Error("the world did not generate after a version mismatch")
	}
}

// TestTheWorldsParametersWinOverTheConfigured is the same rule one level down:
// the parameters the world's terrain was made from are the ones it keeps.
func TestTheWorldsParametersWinOverTheConfigured(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	sand := json.RawMessage(`{"layers":[{"block":"minecraft:sand","thickness":2}],"biome":"minecraft:desert"}`)

	settings := config.DefaultConfig()
	settings.GeneratorType = config.GeneratorFlat
	settings.GeneratorParams = sand

	store, err := server.FileStore(dir, nil)
	if err != nil {
		t.Fatalf("FileStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	first, err := server.New(append(store.Options(), server.WithSettings(settings))...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := first.Load(ctx); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Reopened with different parameters in the configuration.
	settings2 := config.DefaultConfig()
	settings2.GeneratorType = config.GeneratorFlat
	settings2.GeneratorParams = json.RawMessage(
		`{"layers":[{"block":"minecraft:stone","thickness":6}],"biome":"minecraft:plains"}`,
	)

	store2, err := server.FileStore(dir, nil)
	if err != nil {
		t.Fatalf("FileStore: %v", err)
	}
	t.Cleanup(func() { _ = store2.Close() })

	second, err := server.New(append(store2.Options(), server.WithSettings(settings2))...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	w := second.World()
	if got := w.Block(world.BlockPos{X: 5, Y: 1, Z: 5}); got != w.Registry().Intern("minecraft:sand", nil) {
		t.Errorf("y=1 is %d, want the world's own sand", got)
	}
	if got := w.Block(world.BlockPos{X: 5, Y: 4, Z: 5}); got != w.Air() {
		t.Errorf("y=4 is %d, want air — the configured six layers of stone were used", got)
	}
}
