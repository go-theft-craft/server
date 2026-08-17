package gen

import (
	"fmt"

	"github.com/go-theft-craft/server/pkg/world"
)

// DefaultGenerator produces vanilla-like terrain with biomes, caves, ores, and trees.
type DefaultGenerator struct {
	bound

	seed   int64
	params DefaultParams

	terrain  *NoiseGenerator
	detail   *NoiseGenerator
	biomeGen *BiomeGenerator
	caveGen  *CaveGenerator
	oreGen   *OreGenerator
	treeGen  *TreeGenerator

	surface surfacePalette
	ores    []resolvedOre
}

// NewDefaultGenerator creates a DefaultGenerator from a seed, with the
// parameters the constants used to hold. Its palette is resolved when the
// world binds it.
func NewDefaultGenerator(seed int64) *DefaultGenerator {
	return newDefault(seed, DefaultDefaults())
}

// NewDefaultGeneratorWith creates one from explicit parameters and resolves
// every block they name straight away, so a name the registry does not know is
// an error here rather than a wrong block a thousand chunks later.
func NewDefaultGeneratorWith(seed int64, params DefaultParams, reg world.StateRegistry) (*DefaultGenerator, error) {
	g := newDefault(seed, params)
	if reg == nil {
		return g, nil
	}
	if err := g.Bind(reg); err != nil {
		return nil, err
	}

	return g, nil
}

func newDefault(seed int64, params DefaultParams) *DefaultGenerator {
	return &DefaultGenerator{
		seed:     seed,
		params:   params,
		terrain:  NewNoiseGenerator(seed),
		detail:   NewNoiseGenerator(seed + 1),
		biomeGen: NewBiomeGenerator(seed, params.Biomes, params.SeaLevel, params.TerrainScale),
		caveGen:  NewCaveGenerator(seed, params.Caves),
		oreGen:   NewOreGenerator(seed),
		treeGen:  NewTreeGenerator(seed, params.Trees, params.SeaLevel),
	}
}

// Params is what this generator was built from, for the world's metadata.
func (g *DefaultGenerator) Params() Params { return g.params }

// Bind resolves the palette and every block the parameters name.
func (g *DefaultGenerator) Bind(reg world.StateRegistry) error {
	if err := g.bound.Bind(reg); err != nil {
		return err
	}

	var err error
	if g.surface, err = resolveSurface(reg, g.params.Surface); err != nil {
		return err
	}
	g.ores, err = resolveOres(reg, g.params.Ores)

	return err
}

// Generate runs the four passes over one column.
func (g *DefaultGenerator) Generate(pos world.ChunkPos, into *world.Builder) error {
	c := setter{b: into, p: g.p}

	// Pass 1: compute heightmap and fill terrain + biomes.
	var heights [16][16]int
	for x := range 16 {
		for z := range 16 {
			bx := pos.X*16 + x
			bz := pos.Z*16 + z

			biome := g.biomeGen.BiomeAt(bx, bz)
			into.SetBiome(x, z, world.Biome(biome))

			height := g.terrainHeight(bx, bz, biome)
			heights[x][z] = height

			g.fillColumn(c, x, z, height, biome)
		}
	}

	// Pass 2: carve caves.
	g.caveGen.Carve(c, pos.X, pos.Z, &heights)

	// Pass 3: place ores.
	g.oreGen.Place(c, pos.X, pos.Z, &heights, g.ores)

	// Pass 4: place trees and vegetation.
	g.treeGen.Decorate(c, pos.X, pos.Z, &heights)

	return nil
}

// HeightAt recomputes the terrain height at a world block coordinate.
//
// It does not read the generated chunk, so it does not know about caves: at a
// cave mouth it reports the surface the terrain pass produced rather than the
// hole the carving pass left. That gap is documented rather than fixed here;
// it belongs to whoever owns where a dropped item lands.
func (g *DefaultGenerator) HeightAt(blockX, blockZ int) int {
	biome := g.biomeGen.BiomeAt(blockX, blockZ)

	return g.terrainHeight(blockX, blockZ, biome)
}

// terrainHeight computes the terrain height at a world block coordinate.
// Different biomes scale noise amplitude differently.
func (g *DefaultGenerator) terrainHeight(bx, bz int, biome byte) int {
	// Base terrain noise.
	nx := float64(bx) / g.params.TerrainScale
	nz := float64(bz) / g.params.TerrainScale
	base := g.terrain.OctaveNoise2D(nx, nz, 6, 0.5)

	// Detail noise for small-scale variation.
	dx := float64(bx) / g.params.DetailScale
	dz := float64(bz) / g.params.DetailScale
	detail := g.detail.OctaveNoise2D(dx, dz, 3, 0.5)

	shape := g.biomeGen.terrainFor(biome)
	baseHeight := float64(g.params.SeaLevel) + shape.BaseOffset

	h := int(baseHeight + base*shape.Amplitude + detail*g.params.DetailAmplitude)

	return min(max(h, g.params.MinHeight), g.params.MaxHeight)
}

// fillColumn fills a single block column with terrain blocks.
func (g *DefaultGenerator) fillColumn(c setter, x, z, height int, biome byte) {
	// Bedrock layers: y=0 always, the next few randomized.
	c.set(x, 0, z, c.p.bedrock)
	for y := 1; y <= g.params.BedrockDepth; y++ {
		bx := x + y*7 // cheap variation
		if g.terrain.Noise2D(float64(bx)*0.5, float64(z)*0.5) > 0.0 {
			c.set(x, y, z, c.p.bedrock)
		} else {
			c.set(x, y, z, c.p.stone)
		}
	}

	// Stone fill from just above the bedrock up to surface minus the surface
	// depth (or to the surface if the column is below sea level).
	stoneBase := g.params.BedrockDepth + 1
	stoneTop := max(height-g.surfaceDepth(biome), stoneBase)
	for y := stoneBase; y <= stoneTop && y <= height; y++ {
		c.set(x, y, z, c.p.stone)
	}

	// Surface layers.
	g.applySurface(c, x, z, height, biome)

	// Water fill from surface+1 to sea level where terrain is below sea level.
	if height < g.params.SeaLevel {
		for y := height + 1; y <= g.params.SeaLevel; y++ {
			c.set(x, y, z, c.p.water)
		}
	}
}

// surfaceDepth is how many blocks of surface material go below the top block.
func (g *DefaultGenerator) surfaceDepth(biome byte) int {
	if biome == biomeDesert {
		return g.params.Surface.DesertDepth
	}

	return g.params.Surface.Depth
}

// surfacePalette is the surface pass's blocks, resolved once.
type surfacePalette struct {
	top        world.State
	filler     world.State
	underwater world.State
	sand       world.State
	sandstone  world.State
	gravel     world.State
	stone      world.State
}

func resolveSurface(reg world.StateRegistry, p SurfaceParams) (surfacePalette, error) {
	var out surfacePalette
	for _, field := range []struct {
		name string
		into *world.State
	}{
		{p.Top, &out.top},
		{p.Filler, &out.filler},
		{p.Underwater, &out.underwater},
		{p.Sand, &out.sand},
		{p.Sandstone, &out.sandstone},
		{p.Gravel, &out.gravel},
		{p.Stone, &out.stone},
	} {
		state, err := resolveBlock(reg, field.name)
		if err != nil {
			return surfacePalette{}, err
		}
		*field.into = state
	}

	return out, nil
}

// resolveBlock turns a parameter's block name into a handle, reporting rather
// than panicking: the name came from a file somebody wrote.
func resolveBlock(reg world.StateRegistry, name string) (world.State, error) {
	if name == "" {
		return 0, fmt.Errorf("gen: empty block name")
	}
	state, ok := reg.TryIntern(name, nil)
	if !ok {
		return 0, fmt.Errorf("gen: no block is named %q", name)
	}

	return state, nil
}
