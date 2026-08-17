package gen

// applySurface places the biome-specific surface blocks on top of the stone column.
func applySurface(c setter, x, z, height int, biome byte) {
	switch biome {
	case biomeDesert:
		// Sand on top, sandstone below.
		for y := height; y > height-4 && y > 3; y-- {
			c.set(x, y, z, c.p.sand)
		}
		if height-4 > 3 {
			c.set(x, height-4, z, c.p.sandstone)
		}
		if height-5 > 3 {
			c.set(x, height-5, z, c.p.sandstone)
		}

	case biomeOcean:
		// Gravel on the ocean floor.
		for y := height; y > height-3 && y > 3; y-- {
			c.set(x, y, z, c.p.gravel)
		}
		for y := height - 3; y > height-5 && y > 3; y-- {
			c.set(x, y, z, c.p.dirt)
		}

	case biomeBeach:
		// Sand on beaches.
		for y := height; y > height-4 && y > 3; y-- {
			c.set(x, y, z, c.p.sand)
		}
		if height-4 > 3 {
			c.set(x, height-4, z, c.p.sandstone)
		}

	case biomeMountains:
		// Stone with thin dirt/grass cap above tree line, normal below.
		if height > 100 {
			// Bare stone peaks.
			for y := height; y > height-4 && y > 3; y-- {
				c.set(x, y, z, c.p.stone)
			}
		} else {
			applyDefaultSurface(c, x, z, height)
		}

	case biomeSnowyTaiga, biomeTundra:
		// Grass + dirt, snow will be added later via decoration if needed.
		applyDefaultSurface(c, x, z, height)

	default:
		applyDefaultSurface(c, x, z, height)
	}
}

// applyDefaultSurface places grass on top with dirt below.
func applyDefaultSurface(c setter, x, z, height int) {
	if height <= 3 {
		return
	}
	if height > seaLevel {
		c.set(x, height, z, c.p.grass)
	} else {
		// Underwater: dirt instead of grass.
		c.set(x, height, z, c.p.dirt)
	}
	for y := height - 1; y > height-4 && y > 3; y-- {
		c.set(x, y, z, c.p.dirt)
	}
}
