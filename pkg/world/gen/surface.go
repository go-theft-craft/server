package gen

// applySurface places the biome-specific surface blocks on top of the stone column.
func applySurface(c *ChunkData, x, z, height int, biome byte) {
	switch biome {
	case biomeDesert:
		// Sand on top, sandstone below.
		for y := height; y > height-4 && y > 3; y-- {
			c.SetBlock(x, y, z, blockSand<<4)
		}
		if height-4 > 3 {
			c.SetBlock(x, height-4, z, blockSandstone<<4)
		}
		if height-5 > 3 {
			c.SetBlock(x, height-5, z, blockSandstone<<4)
		}

	case biomeOcean:
		// Gravel on the ocean floor.
		for y := height; y > height-3 && y > 3; y-- {
			c.SetBlock(x, y, z, blockGravel<<4)
		}
		for y := height - 3; y > height-5 && y > 3; y-- {
			c.SetBlock(x, y, z, blockDirt<<4)
		}

	case biomeBeach:
		// Sand on beaches.
		for y := height; y > height-4 && y > 3; y-- {
			c.SetBlock(x, y, z, blockSand<<4)
		}
		if height-4 > 3 {
			c.SetBlock(x, height-4, z, blockSandstone<<4)
		}

	case biomeMountains:
		// Stone with thin dirt/grass cap above tree line, normal below.
		if height > 100 {
			// Bare stone peaks.
			for y := height; y > height-4 && y > 3; y-- {
				c.SetBlock(x, y, z, blockStone<<4)
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
func applyDefaultSurface(c *ChunkData, x, z, height int) {
	if height <= 3 {
		return
	}
	if height > seaLevel {
		c.SetBlock(x, height, z, blockGrass<<4)
	} else {
		// Underwater: dirt instead of grass.
		c.SetBlock(x, height, z, blockDirt<<4)
	}
	for y := height - 1; y > height-4 && y > 3; y-- {
		c.SetBlock(x, y, z, blockDirt<<4)
	}
}
