package gen

// The surface pass.
//
// Every literal here used to be a constant: the 4 is the surface depth, and
// the 3 that guards every loop is the bedrock depth — the surface never eats
// into the layers the bedrock pass wrote.

// applySurface places the biome-specific surface blocks on top of the stone
// column.
func (g *DefaultGenerator) applySurface(c setter, x, z, height int, biome byte) {
	depth := g.params.Surface.Depth
	floor := g.params.BedrockDepth

	switch biome {
	case biomeDesert:
		// Sand on top, sandstone below.
		for y := height; y > height-depth && y > floor; y-- {
			c.set(x, y, z, g.surface.sand)
		}
		if height-depth > floor {
			c.set(x, height-depth, z, g.surface.sandstone)
		}
		if height-depth-1 > floor {
			c.set(x, height-depth-1, z, g.surface.sandstone)
		}

	case biomeOcean:
		// Gravel on the ocean floor, dirt under it.
		for y := height; y > height-(depth-1) && y > floor; y-- {
			c.set(x, y, z, g.surface.gravel)
		}
		for y := height - (depth - 1); y > height-(depth+1) && y > floor; y-- {
			c.set(x, y, z, g.surface.underwater)
		}

	case biomeBeach:
		// Sand on beaches.
		for y := height; y > height-depth && y > floor; y-- {
			c.set(x, y, z, g.surface.sand)
		}
		if height-depth > floor {
			c.set(x, height-depth, z, g.surface.sandstone)
		}

	case biomeMountains:
		// Bare stone peaks above the tree line, normal ground below.
		if height > g.params.Surface.BareStoneAbove {
			for y := height; y > height-depth && y > floor; y-- {
				c.set(x, y, z, g.surface.stone)
			}
		} else {
			g.applyDefaultSurface(c, x, z, height)
		}

	default:
		// Snowy taiga and tundra included: snow is decoration, not surface.
		g.applyDefaultSurface(c, x, z, height)
	}
}

// applyDefaultSurface places the top block with filler below it.
func (g *DefaultGenerator) applyDefaultSurface(c setter, x, z, height int) {
	depth := g.params.Surface.Depth
	floor := g.params.BedrockDepth

	if height <= floor {
		return
	}
	if height > g.params.SeaLevel {
		c.set(x, height, z, g.surface.top)
	} else {
		// Underwater: the filler block instead of the top one.
		c.set(x, height, z, g.surface.underwater)
	}
	for y := height - 1; y > height-depth && y > floor; y-- {
		c.set(x, y, z, g.surface.filler)
	}
}
