package storage

import (
	"strconv"
	"strings"

	"github.com/go-theft-craft/server/pkg/world"
)

// Container item identity in the sidecar.
//
// The items in a chest live in the chunk, and the vanilla format has no field
// for their identity, which is the whole reason the sidecar exists. Keeping
// them here rather than in an index of their own gives them the property block
// identity already has: they load and unload with the column they are in, and
// a chunk nobody has visited costs nothing.
//
// A player's own items do not come through here. They are in a file of their
// own, as JSON, and an ItemStack marshals its IDs, so they survive without
// help.

// containerKey names one slot of one container: the block key of the container
// and the slot number inside it.
func containerKey(pos world.BlockPos, slot int) string {
	return formatKey(keyOf(pos)) + ":" + strconv.Itoa(slot)
}

func parseContainerKey(chunk world.ChunkPos, s string) (world.BlockPos, int, bool) {
	block, slot, ok := strings.Cut(s, ":")
	if !ok {
		return world.BlockPos{}, 0, false
	}
	k, err := parseKey(block)
	if err != nil {
		return world.BlockPos{}, 0, false
	}
	n, err := strconv.Atoi(slot)
	if err != nil || n < 0 || n >= world.ChestSlots {
		return world.BlockPos{}, 0, false
	}

	return blockPosOf(chunk, k), n, true
}

// ContainerIdentity is what a chunk's containers carry, in the form the
// sidecar stores. It returns nil for a chunk whose containers hold no
// identified item, which is every chunk on a server that tracks none.
func ContainerIdentity(c *world.Chunk) map[string][]string {
	if c == nil || len(c.Chests) == 0 {
		return nil
	}

	var out map[string][]string
	for pos, contents := range c.Chests {
		for slot := range contents {
			ids := contents[slot].IDs
			if len(ids) == 0 {
				continue
			}
			if out == nil {
				out = make(map[string][]string)
			}
			text := make([]string, len(ids))
			for i, id := range ids {
				text[i] = id.String()
			}
			out[containerKey(pos, slot)] = text
		}
	}

	return out
}

// ApplyContainerIdentity puts stored identity back onto a chunk's containers
// and reports how many entries would not parse.
//
// It writes into the chunk, so it may only be called before the world
// publishes it. An entry naming a slot that is now empty is dropped: the
// identity is then unaccounted for, and saying so is reconciliation's job
// rather than this function's.
func ApplyContainerIdentity(chunk world.ChunkPos, c *world.Chunk, entries map[string][]string) int {
	if c == nil || len(entries) == 0 {
		return 0
	}

	bad := 0
	for key, text := range entries {
		pos, slot, ok := parseContainerKey(chunk, key)
		if !ok {
			bad++

			continue
		}
		contents, held := c.Chests[pos]
		if !held || contents[slot].IsEmpty() {
			continue
		}

		ids := make([]world.ItemID, 0, len(text))
		for _, s := range text {
			id, err := world.ParseItemID(s)
			if err != nil || !id.Valid() {
				bad++

				continue
			}
			ids = append(ids, id)
		}
		if len(ids) == 0 {
			continue
		}

		contents[slot].IDs = ids
		c.Chests[pos] = contents
	}

	return bad
}
