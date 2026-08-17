package gen

// Biome IDs matching Minecraft 1.8 protocol.
const (
	biomeOcean      byte = 0
	biomePlains     byte = 1
	biomeSavanna    byte = 35
	biomeForest     byte = 4
	biomeDarkForest byte = 29
	biomeTaiga      byte = 5
	biomeSnowyTaiga byte = 30
	biomeDesert     byte = 2
	biomeJungle     byte = 21
	biomeMountains  byte = 3 // extreme hills
	biomeBeach      byte = 16
	biomeTundra     byte = 12
)

// biomeNames maps this generator's biome IDs to the names its parameters use.
//
// The names are the modern ones — "minecraft:mountains" rather than 1.8's
// "extreme_hills" — because a parameter file outlives a protocol version and
// the ID is what belongs to the version.
var biomeNames = map[byte]string{
	biomeOcean:      "minecraft:ocean",
	biomePlains:     "minecraft:plains",
	biomeDesert:     "minecraft:desert",
	biomeMountains:  "minecraft:mountains",
	biomeForest:     "minecraft:forest",
	biomeTaiga:      "minecraft:taiga",
	biomeTundra:     "minecraft:tundra",
	biomeBeach:      "minecraft:beach",
	biomeJungle:     "minecraft:jungle",
	biomeDarkForest: "minecraft:dark_forest",
	biomeSnowyTaiga: "minecraft:snowy_taiga",
	biomeSavanna:    "minecraft:savanna",
}

// biomeName is the parameter key for a biome ID.
func biomeName(id byte) string { return biomeNames[id] }

// BiomeGenerator selects biomes using temperature/rainfall noise fields.
type BiomeGenerator struct {
	tempNoise *NoiseGenerator
	rainNoise *NoiseGenerator
	terrain   *NoiseGenerator

	params       BiomeParams
	seaLevel     float64
	terrainScale float64
}

// NewBiomeGenerator creates a BiomeGenerator from a seed and the parameters
// that shape it.
func NewBiomeGenerator(seed int64, params BiomeParams, seaLevel int, terrainScale float64) *BiomeGenerator {
	return &BiomeGenerator{
		tempNoise:    NewNoiseGenerator(seed + 100),
		rainNoise:    NewNoiseGenerator(seed + 200),
		terrain:      NewNoiseGenerator(seed),
		params:       params,
		seaLevel:     float64(seaLevel),
		terrainScale: terrainScale,
	}
}

// BiomeAt returns the biome ID at the given world block coordinates.
func (bg *BiomeGenerator) BiomeAt(bx, bz int) byte {
	// Sample temperature and rainfall at large scale.
	tx := float64(bx) / bg.params.TemperatureScale
	tz := float64(bz) / bg.params.RainfallScale
	temp := bg.tempNoise.OctaveNoise2D(tx, tz, 4, 0.5)*0.8 + 0.75 // center around 0.75
	rain := bg.rainNoise.OctaveNoise2D(tx+100, tz+100, 4, 0.5)*0.5 + 0.5

	// Check for ocean: very low terrain at this position.
	nx := float64(bx) / bg.terrainScale
	nz := float64(bz) / bg.terrainScale
	terrainBase := bg.terrain.OctaveNoise2D(nx, nz, 6, 0.5)
	terrainHeight := bg.seaLevel + terrainBase*8.0
	if terrainHeight < bg.seaLevel-bg.params.OceanBelow {
		return biomeOcean
	}

	// Check for beach: terrain near sea level.
	if terrainHeight < bg.seaLevel-bg.params.BeachBelow {
		return biomeBeach
	}

	return selectBiome(temp, rain)
}

// terrainFor is a biome's height shape, falling back to the default for a
// biome the parameters do not name.
func (bg *BiomeGenerator) terrainFor(biome byte) BiomeTerrain {
	if t, ok := bg.params.Terrain[biomeName(biome)]; ok {
		return t
	}

	return bg.params.DefaultTerrain
}

// selectBiome maps temperature and rainfall to a biome ID.
//
//	Temp\Rain     | Dry (<0.3)    | Medium (0.3-0.6) | Wet (>0.6)
//	Cold <0.3     | Tundra (12)   | Snowy Taiga (30)  | Taiga (5)
//	Mild 0.3-0.7  | Plains (1)    | Forest (4)        | Dark Forest (29)
//	Warm 0.7-1.2  | Savanna (35)  | Plains (1)        | Jungle (21)
//	Hot >1.2      | Desert (2)    | Desert (2)        | Jungle (21)
func selectBiome(temp, rain float64) byte {
	switch {
	case temp < 0.3:
		switch {
		case rain < 0.3:
			return biomeTundra
		case rain < 0.6:
			return biomeSnowyTaiga
		default:
			return biomeTaiga
		}
	case temp < 0.7:
		switch {
		case rain < 0.3:
			return biomePlains
		case rain < 0.6:
			return biomeForest
		default:
			return biomeDarkForest
		}
	case temp < 1.2:
		switch {
		case rain < 0.3:
			return biomeSavanna
		case rain < 0.6:
			return biomePlains
		default:
			return biomeJungle
		}
	default:
		if rain > 0.6 {
			return biomeJungle
		}
		return biomeDesert
	}
}
