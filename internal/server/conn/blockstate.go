package conn

import (
	"strconv"
	"strings"

	"github.com/go-theft-craft/minecraft-protocol/data"

	"github.com/go-theft-craft/server/pkg/world"
)

// Block state handles at the connection.
//
// The world speaks version-neutral handles, so the handlers name blocks by
// their canonical name and compare handles rather than protocol 47 numbers.
// The two places a number is still unavoidable are the wire — a BlockChange
// carries the version's own encoding — and the inventory, whose item IDs are
// protocol 47's throughout. Both cross the boundary through the adapter, and
// nowhere else in this package shifts a state by four.

// The blocks the handlers name.
const (
	chestName         = "minecraft:chest"
	trappedChestName  = "minecraft:trapped_chest"
	craftingTableName = "minecraft:crafting_table"
	cactusName        = "minecraft:cactus"
)

// The inventory item IDs of the containers, which are protocol 47 numbers
// because a player.Slot is protocol 47 throughout. Only the item side uses
// them; the world side names the block.
const (
	chestBlockID        = 54
	trappedChestBlockID = 146
)

// blockStates is the connection's vocabulary of block state handles.
type blockStates struct {
	adapter world.Adapter
	reg     world.StateRegistry
	blocks  data.BlockRegistry

	air world.State
}

func newBlockStates(w *world.World, gd *data.Set) blockStates {
	s := blockStates{adapter: w.Adapter(), reg: w.Registry(), air: w.Air()}
	if gd != nil {
		s.blocks = gd.Blocks()
	}

	return s
}

// blockAt is the state at a position.
func (c *Connection) blockAt(x, y, z int) world.State {
	return c.world.Block(world.BlockPos{X: x, Y: y, Z: z})
}

// setBlockAt writes a state at a position.
func (c *Connection) setBlockAt(x, y, z int, state world.State) {
	c.world.SetBlock(world.BlockPos{X: x, Y: y, Z: z}, state)
}

// isAir reports whether a state is the dimension's empty block.
func (c *Connection) isAir(state world.State) bool { return state == c.states.air }

// blockName is the canonical name a state stands for.
func (c *Connection) blockName(state world.State) string {
	name, _, ok := c.states.reg.Lookup(state)
	if !ok {
		return ""
	}

	return name
}

// blockMetadata is the pre-flattening variant a state carries, or 0 for a
// block with no variants. It exists because a chest's facing and a stair's
// orientation are metadata on protocol 47 and the handlers still reason in
// those terms.
func (c *Connection) blockMetadata(state world.State) int32 {
	_, props, ok := c.states.reg.Lookup(state)
	if !ok {
		return 0
	}
	for _, p := range props {
		if p.Key != "metadata" {
			continue
		}
		n, err := strconv.Atoi(p.Value)
		if err != nil {
			return 0
		}

		return int32(n)
	}

	return 0
}

// blockState is the handle for a canonical name and a metadata variant.
func (c *Connection) blockState(name string, metadata int32) world.State {
	if metadata == 0 {
		return c.states.reg.Intern(name, nil)
	}

	return c.states.reg.Intern(name, world.Properties{
		{Key: "metadata", Value: strconv.Itoa(int(metadata))},
	})
}

// wireState is what a client is told a state is.
func (c *Connection) wireState(state world.State) int32 {
	v, err := c.states.adapter.EncodeState(state)
	if err != nil {
		return 0
	}

	return v
}

// blockOf resolves the game data entry a state stands for, by name rather than
// by ID: the handle knows what block it is, and the ID is the wire's business.
func (c *Connection) blockOf(state world.State) (data.Block, bool) {
	if c.states.blocks == nil {
		return data.Block{}, false
	}
	name := c.blockName(state)
	if name == "" {
		return data.Block{}, false
	}

	return c.states.blocks.ByName(strings.TrimPrefix(name, "minecraft:"))
}
