// Command custom runs a server with a world generator this program defines and
// registers by name.
//
// It exists to prove the extension point from outside the framework's module.
// `examples/` is a separate module with a `replace` for the parent, so what
// this file can reach is exactly what any consumer can reach: if a generator
// can be written here, it can be written anywhere.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/pkg/world/gen"
	"github.com/go-theft-craft/server/server"
)

// checkerName is what the generator is registered and selected as.
const checkerName = "checker"

// CheckerParams configures the checkerboard.
type CheckerParams struct {
	// Period is how many blocks wide each square is.
	Period int `json:"period"`
	// Height is how many layers thick the board is.
	Height int `json:"height"`
	// Light and Dark are the two blocks it alternates between.
	Light string `json:"light"`
	Dark  string `json:"dark"`
}

// Type implements gen.Params.
func (CheckerParams) Type() string { return checkerName }

// CheckerFactory builds the checkerboard generator.
type CheckerFactory struct{}

// Name implements gen.Factory.
func (CheckerFactory) Name() string { return checkerName }

// Version implements gen.Factory. Bump it when a change moves terrain.
func (CheckerFactory) Version() int { return 1 }

// Defaults implements gen.Factory.
func (CheckerFactory) Defaults() gen.Params {
	return CheckerParams{
		Period: 8,
		Height: 4,
		Light:  "minecraft:quartz_block",
		Dark:   "minecraft:coal_block",
	}
}

// Parse implements gen.Factory. It starts from the defaults, so a parameter
// file may set one field and leave the rest alone.
func (f CheckerFactory) Parse(raw json.RawMessage) (gen.Params, error) {
	params, _ := f.Defaults().(CheckerParams)
	if len(raw) == 0 {
		return params, nil
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, fmt.Errorf("%s parameters: %w", checkerName, err)
	}
	if params.Period < 1 {
		return nil, fmt.Errorf("%s: period %d must be at least 1", checkerName, params.Period)
	}
	if params.Height < 1 {
		return nil, fmt.Errorf("%s: height %d must be at least 1", checkerName, params.Height)
	}

	return params, nil
}

// New implements gen.Factory. Block names are resolved here, once, so a name
// nothing knows is an error at startup rather than a wrong block later.
func (f CheckerFactory) New(_ int64, p gen.Params, reg world.StateRegistry) (gen.Generator, error) {
	params, ok := p.(CheckerParams)
	if !ok {
		return nil, fmt.Errorf("%s: got %s parameters", checkerName, p.Type())
	}

	light, ok := reg.TryIntern(params.Light, nil)
	if !ok {
		return nil, fmt.Errorf("%s: no block is named %q", checkerName, params.Light)
	}
	dark, ok := reg.TryIntern(params.Dark, nil)
	if !ok {
		return nil, fmt.Errorf("%s: no block is named %q", checkerName, params.Dark)
	}

	return &checkerGenerator{params: params, light: light, dark: dark}, nil
}

type checkerGenerator struct {
	params      CheckerParams
	light, dark world.State
}

// Generate fills the board. Nothing here knows what a block ID is: the two
// states came from the registry and the builder takes them as they are.
func (g *checkerGenerator) Generate(pos world.ChunkPos, into *world.Builder) error {
	for x := range 16 {
		for z := range 16 {
			bx := pos.X*16 + x
			bz := pos.Z*16 + z

			block := g.light
			if (floorDiv(bx, g.params.Period)+floorDiv(bz, g.params.Period))%2 != 0 {
				block = g.dark
			}

			for y := range g.params.Height {
				if err := into.Set(x, y, z, block); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// HeightAt is the top block of the board.
func (g *checkerGenerator) HeightAt(int, int) int { return g.params.Height - 1 }

// floorDiv divides towards negative infinity, so the board does not mirror
// across the origin the way integer division towards zero would make it.
func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}

	return q
}

func main() {
	port := flag.Int("port", 25565, "server port")
	period := flag.Int("period", 8, "checkerboard square size in blocks")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Start from the framework's registry and add to it, so "default" and
	// "flat" are still selectable beside the new one.
	registry := gen.DefaultRegistry()
	if err := registry.Register(CheckerFactory{}); err != nil {
		log.Error("register generator", "error", err)
		os.Exit(1)
	}

	params, _ := (CheckerFactory{}).Defaults().(CheckerParams)
	params.Period = *period

	srv, err := server.New(
		server.WithLogger(log),
		server.WithPort(*port),
		server.WithMOTD("custom generator example"),
		server.WithGeneratorRegistry(registry),
		server.WithGeneratorNamed(checkerName, params),
		server.WithWorldRadius(4),
	)
	if err != nil {
		log.Error("create server", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := srv.Start(ctx); err != nil {
		log.Error("server error", "error", err)
		os.Exit(1)
	}
}
