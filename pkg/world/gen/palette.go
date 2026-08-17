package gen

import (
	"github.com/go-theft-craft/server/pkg/world"
)

// palette is a generator's block vocabulary, resolved once against the
// registry the server built. Generation writes handles, so the names live here
// and nowhere on the write path.
type palette struct {
	air       world.State
	stone     world.State
	grass     world.State
	dirt      world.State
	bedrock   world.State
	water     world.State
	lava      world.State
	sand      world.State
	sandstone world.State
	gravel    world.State

	logOak    world.State
	logSpruce world.State
	logBirch  world.State

	leavesOak    world.State
	leavesSpruce world.State
	leavesBirch  world.State

	tallGrass world.State
	flower    world.State
	cactus    world.State
	deadBush  world.State

	coalOre     world.State
	ironOre     world.State
	goldOre     world.State
	diamondOre  world.State
	redstoneOre world.State
	lapisOre    world.State
}

// metadata names one pre-flattening variant.
func metadata(n int) world.Properties {
	switch n {
	case 1:
		return world.Properties{{Key: "metadata", Value: "1"}}
	case 2:
		return world.Properties{{Key: "metadata", Value: "2"}}
	default:
		return nil
	}
}

// newPalette resolves every name the generators use. A name the registry does
// not know panics here, at construction, rather than producing a wrong block a
// thousand chunks later.
func newPalette(reg world.StateRegistry) palette {
	return palette{
		air:       reg.Air(),
		stone:     reg.Intern("minecraft:stone", nil),
		grass:     reg.Intern("minecraft:grass", nil),
		dirt:      reg.Intern("minecraft:dirt", nil),
		bedrock:   reg.Intern("minecraft:bedrock", nil),
		water:     reg.Intern("minecraft:water", nil),
		lava:      reg.Intern("minecraft:lava", nil),
		sand:      reg.Intern("minecraft:sand", nil),
		sandstone: reg.Intern("minecraft:sandstone", nil),
		gravel:    reg.Intern("minecraft:gravel", nil),

		logOak:    reg.Intern("minecraft:log", nil),
		logSpruce: reg.Intern("minecraft:log", metadata(1)),
		logBirch:  reg.Intern("minecraft:log", metadata(2)),

		leavesOak:    reg.Intern("minecraft:leaves", nil),
		leavesSpruce: reg.Intern("minecraft:leaves", metadata(1)),
		leavesBirch:  reg.Intern("minecraft:leaves", metadata(2)),

		// Metadata 1 is tall grass; metadata 0 is the dead shrub.
		tallGrass: reg.Intern("minecraft:tallgrass", metadata(1)),
		flower:    reg.Intern("minecraft:red_flower", nil),
		cactus:    reg.Intern("minecraft:cactus", nil),
		deadBush:  reg.Intern("minecraft:deadbush", nil),

		coalOre:     reg.Intern("minecraft:coal_ore", nil),
		ironOre:     reg.Intern("minecraft:iron_ore", nil),
		goldOre:     reg.Intern("minecraft:gold_ore", nil),
		diamondOre:  reg.Intern("minecraft:diamond_ore", nil),
		redstoneOre: reg.Intern("minecraft:redstone_ore", nil),
		lapisOre:    reg.Intern("minecraft:lapis_ore", nil),
	}
}
