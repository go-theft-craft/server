package gen

const (
	blockAir       = 0
	blockStone     = 1
	blockGrass     = 2
	blockDirt      = 3
	blockBedrock   = 7
	blockWater     = 9 // stationary water
	blockSand      = 12
	blockGravel    = 13
	blockLog       = 17
	blockLeaves    = 18
	blockSandstone = 24
	blockTallGrass = 31
	blockFlower    = 38
	blockCactus    = 81
	blockDeadBush  = 32

	blockCoalOre     = 16
	blockIronOre     = 15
	blockGoldOre     = 14
	blockDiamondOre  = 56
	blockRedstoneOre = 73
	blockLapisOre    = 21
	blockLava        = 11 // stationary lava

	// Log variants (metadata).
	logOak    = 0
	logSpruce = 1
	logBirch  = 2

	// Leaves variants (metadata).
	leavesOak    = 0
	leavesSpruce = 1
	leavesBirch  = 2

	biomePlains = 1

	seaLevel = 62
)

// FlatGenerator generates a classic superflat world:
// bedrock at y=0, stone y=1..2, dirt y=3, grass y=4.
type FlatGenerator struct{}

// NewFlatGenerator creates a FlatGenerator.
func NewFlatGenerator(_ int64) *FlatGenerator {
	return &FlatGenerator{}
}

func (g *FlatGenerator) Generate(_, _ int) *ChunkData {
	c := &ChunkData{}

	for x := 0; x < 16; x++ {
		for z := 0; z < 16; z++ {
			c.SetBlock(x, 0, z, blockBedrock<<4)
			c.SetBlock(x, 1, z, blockStone<<4)
			c.SetBlock(x, 2, z, blockStone<<4)
			c.SetBlock(x, 3, z, blockDirt<<4)
			c.SetBlock(x, 4, z, blockGrass<<4)
			c.SetBiome(x, z, biomePlains)
		}
	}
	return c
}

func (g *FlatGenerator) HeightAt(_, _ int) int {
	return 4 // top solid block is at y=4 (grass)
}
