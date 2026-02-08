package conn

import (
	"math/rand"

	"github.com/go-theft-craft/server/pkg/gamedata"
	"github.com/go-theft-craft/server/internal/server/player"
)

// canHarvest returns whether the player's held tool can harvest the given block
// (i.e. the block will actually drop items). If harvestTools is nil, any tool works.
func canHarvest(block gamedata.Block, heldItemID int16) bool {
	if block.HarvestTools == nil {
		return true
	}
	return block.HarvestTools[int(heldItemID)]
}

// calcBreakTime returns the expected break time in ticks for a block given the
// player's held item and game mode. Returns -1 for unbreakable blocks.
// Based on https://minecraft.wiki/w/Breaking#Speed
func calcBreakTime(block gamedata.Block, heldItemID int16, materials gamedata.MaterialRegistry) int {
	if block.Hardness == nil {
		return -1 // unbreakable (e.g. bedrock)
	}
	if !block.Diggable {
		return -1
	}

	hardness := *block.Hardness
	if hardness == 0 {
		return 0 // instant break (e.g. tall grass)
	}

	// Determine speed multiplier from tool.
	speedMultiplier := 1.0
	if materials != nil && block.Material != "" {
		mat, ok := materials.ByName(block.Material)
		if ok {
			if speed, hasSpeed := mat.ToolSpeeds[int(heldItemID)]; hasSpeed {
				speedMultiplier = speed
			}
		}
	}

	// Determine if this is the "best" tool (can harvest = gets drops).
	canHarv := canHarvest(block, heldItemID)

	// Base damage per tick.
	var damage float64
	if canHarv {
		damage = speedMultiplier / hardness / 30.0
	} else {
		// Wrong tool: 5x slower, no drops.
		damage = speedMultiplier / hardness / 100.0
	}

	if damage >= 1.0 {
		return 0 // instant break
	}

	ticks := int(1.0 / damage)
	return ticks
}

// blockDrops returns the item slots that should be dropped when a block is broken.
// Returns nil if the tool can't harvest this block.
func blockDrops(block gamedata.Block, heldItemID int16) []player.Slot {
	if !canHarvest(block, heldItemID) {
		return nil
	}

	if len(block.Drops) == 0 {
		return nil
	}

	var drops []player.Slot
	for _, d := range block.Drops {
		if d.ID <= 0 {
			continue
		}
		minC, maxC := d.MinCount, d.MaxCount
		// Most blocks don't specify minCount/maxCount in the data; default to 1.
		if minC == 0 && maxC == 0 {
			minC = 1
			maxC = 1
		}
		count := minC
		if maxC > minC {
			count = minC + rand.Intn(maxC-minC+1)
		}
		if count <= 0 {
			continue
		}
		drops = append(drops, player.Slot{
			BlockID:    int16(d.ID),
			ItemCount:  int8(count),
			ItemDamage: int16(d.Metadata),
		})
	}
	return drops
}

// lookupBlock finds a block by its state ID (stateID = blockID << 4 | metadata).
func (c *Connection) lookupBlock(stateID int32) (gamedata.Block, bool) {
	if c.gameData == nil || c.gameData.Blocks == nil {
		return gamedata.Block{}, false
	}
	blockID := int(stateID >> 4)
	return c.gameData.Blocks.ByID(blockID)
}
