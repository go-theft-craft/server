package storage

import (
	"fmt"
	"strconv"
	"sync"

	"github.com/go-theft-craft/server/pkg/world"
)

// Sparse block identity.
//
// A block placed by a player gets an ID; a block the generator made does not.
// That asymmetry is the whole design: a 2000×2000 world is a billion blocks
// and 8.2 GB of identity alone, nearly all of it air and stone no query would
// ever reach, so identity is spent only on the blocks somebody chose to put
// somewhere.
//
// Position is a stable key, so unlike item identity this needs no index and no
// reconciliation of its own: a chunk's block IDs load with the chunk and are
// discarded with it. Blocks cannot duplicate — a position holds one block — so
// the duplication detector that item identity needs has nothing to do here.
//
// Universal block identity, an ID for generated blocks too, is not
// implemented. Nothing in this file can be switched into it: the table is
// sparse by construction, and a build that wanted the other thing would want a
// different structure, not a flag on this one.

// blockKey is a block's position within its column: y, then z, then x.
//
// It is a uint32 rather than the uint16 a 256-block dimension would fit in,
// because the dimension height is configuration and a key that silently
// wrapped at y=256 would put two blocks in one entry.
type blockKey uint32

func formatKey(k blockKey) string { return strconv.FormatUint(uint64(k), 10) }

func parseKey(s string) (blockKey, error) {
	v, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("storage: %q is not a block key: %w", s, err)
	}

	return blockKey(v), nil
}

func keyOf(pos world.BlockPos) blockKey {
	return blockKey(uint32(pos.Y)<<8 | uint32(pos.Z&15)<<4 | uint32(pos.X&15))
}

// BlockIdentity is the table of identified blocks, keyed by chunk.
//
// The zero value is not usable; NewBlockIdentity returns one that is. A nil
// *BlockIdentity is a working no-op, which is what "off by default" costs at
// the call site.
type BlockIdentity struct {
	mu     sync.RWMutex
	chunks map[world.ChunkPos]map[blockKey]world.ItemID
}

// NewBlockIdentity returns an empty table.
func NewBlockIdentity() *BlockIdentity {
	return &BlockIdentity{chunks: make(map[world.ChunkPos]map[blockKey]world.ItemID)}
}

// Set records that the block at pos has an identity.
func (b *BlockIdentity) Set(pos world.BlockPos, id world.ItemID) {
	if b == nil || !id.Valid() {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	cp := pos.ChunkPos()
	if b.chunks[cp] == nil {
		b.chunks[cp] = make(map[blockKey]world.ItemID)
	}
	b.chunks[cp][keyOf(pos)] = id
}

// At is the identity of the block at pos, if it has one.
func (b *BlockIdentity) At(pos world.BlockPos) (world.ItemID, bool) {
	if b == nil {
		return world.NoItemID, false
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	id, ok := b.chunks[pos.ChunkPos()][keyOf(pos)]

	return id, ok
}

// Take is At followed by release: the block stopped being there, so its
// identity stops being spent on a position nothing occupies.
func (b *BlockIdentity) Take(pos world.BlockPos) (world.ItemID, bool) {
	if b == nil {
		return world.NoItemID, false
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	cp := pos.ChunkPos()
	id, ok := b.chunks[cp][keyOf(pos)]
	if ok {
		delete(b.chunks[cp], keyOf(pos))
		if len(b.chunks[cp]) == 0 {
			delete(b.chunks, cp)
		}
	}

	return id, ok
}

// LoadChunk replaces one chunk's entries with what came off disk.
//
// Keys are the decimal block keys the sidecar holds and values are the
// "epoch:counter" text ItemID.String writes. An entry that will not parse is
// dropped and counted rather than failing the load: a corrupt audit entry must
// not stop a world from opening.
func (b *BlockIdentity) LoadChunk(pos world.ChunkPos, entries map[string]string) int {
	if b == nil || len(entries) == 0 {
		return 0
	}

	table := make(map[blockKey]world.ItemID, len(entries))
	bad := 0
	for key, value := range entries {
		k, err := parseKey(key)
		if err != nil {
			bad++

			continue
		}
		id, err := world.ParseItemID(value)
		if err != nil || !id.Valid() {
			bad++

			continue
		}
		table[k] = id
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if len(table) == 0 {
		delete(b.chunks, pos)
	} else {
		b.chunks[pos] = table
	}

	return bad
}

// Chunk is one chunk's entries in the form the sidecar stores, or nil for a
// chunk holding no identified block.
func (b *BlockIdentity) Chunk(pos world.ChunkPos) map[string]string {
	if b == nil {
		return nil
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	table := b.chunks[pos]
	if len(table) == 0 {
		return nil
	}

	out := make(map[string]string, len(table))
	for key, id := range table {
		out[formatKey(key)] = id.String()
	}

	return out
}

// Positions is every identified block in one chunk, with its identity.
//
// It is what reconciliation walks: an entry whose position no longer holds a
// block is an ID with nothing under it.
func (b *BlockIdentity) Positions(pos world.ChunkPos) map[world.BlockPos]world.ItemID {
	if b == nil {
		return nil
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	table := b.chunks[pos]
	if len(table) == 0 {
		return nil
	}

	out := make(map[world.BlockPos]world.ItemID, len(table))
	for key, id := range table {
		out[blockPosOf(pos, key)] = id
	}

	return out
}

// DropChunk forgets a chunk's identities, which is what unloading it means.
func (b *BlockIdentity) DropChunk(pos world.ChunkPos) {
	if b == nil {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	delete(b.chunks, pos)
}

// Len is how many blocks are identified, which is the table's memory cost and
// the number that says whether "sparse" held.
func (b *BlockIdentity) Len() int {
	if b == nil {
		return 0
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	n := 0
	for _, table := range b.chunks {
		n += len(table)
	}

	return n
}

func blockPosOf(chunk world.ChunkPos, key blockKey) world.BlockPos {
	return world.BlockPos{
		X: chunk.X*16 + int(key&15),
		Y: int(key >> 8),
		Z: chunk.Z*16 + int((key>>4)&15),
	}
}
