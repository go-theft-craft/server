package anvil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/pkg/world/v47"
)

// javaCodec is the pair a region reader and writer need: a protocol 47 encoder
// for what goes on disk and the matching decoder for what comes back.
func javaCodec(t *testing.T) (*v47.Adapter, world.StateRegistry) {
	t.Helper()

	set, err := v1_8.Data()
	if err != nil {
		t.Fatalf("v1_8.Data: %v", err)
	}
	reg, err := world.NewJavaRegistry(set)
	if err != nil {
		t.Fatalf("NewJavaRegistry: %v", err)
	}
	a, err := v47.New(reg, set)
	if err != nil {
		t.Fatalf("v47.New: %v", err)
	}

	return a, reg
}

// writeRegion saves one chunk and returns the directory it landed in.
func writeRegion(t *testing.T, a *v47.Adapter, chunks ...*world.Chunk) string {
	t.Helper()

	dir := t.TempDir()
	byRegion := map[[2]int]map[world.ChunkPos][]byte{}
	for _, c := range chunks {
		payload, err := EncodeChunkNBT(c, a)
		if err != nil {
			t.Fatalf("EncodeChunkNBT: %v", err)
		}
		rx, rz := RegionOf(c.Pos)
		key := [2]int{rx, rz}
		if byRegion[key] == nil {
			byRegion[key] = map[world.ChunkPos][]byte{}
		}
		byRegion[key][c.Pos] = payload
	}
	for key, raw := range byRegion {
		payloads := map[world.ChunkPos]Payload{}
		for pos, nbtData := range raw {
			payloads[pos] = Payload{NBT: nbtData}
		}
		if err := SaveRegion(dir, key[0], key[1], payloads); err != nil {
			t.Fatalf("SaveRegion: %v", err)
		}
	}

	return dir
}

// TestARegionThisServerWroteReadsBackEqual is the property the milestone is
// named for: the server reads the world it writes.
func TestARegionThisServerWroteReadsBackEqual(t *testing.T) {
	a, reg := javaCodec(t)
	dim := world.Overworld18()

	stone := reg.Intern("minecraft:stone", nil)
	chest := reg.Intern("minecraft:chest", world.Properties{{Key: "metadata", Value: "2"}})

	want := newChunk(3, -2)
	for x := range 16 {
		for z := range 16 {
			setBlock(want, x, 0, z, stone)
		}
	}
	setBlock(want, 5, 130, 5, chest)
	for i := range want.Biomes {
		want.Biomes[i] = world.Biome(i % 40)
	}

	dir := writeRegion(t, a, want)

	rx, rz := RegionOf(want.Pos)
	region, err := OpenRegion(dir, rx, rz, dim, a, reg.Air())
	if err != nil {
		t.Fatalf("OpenRegion: %v", err)
	}

	got, present, err := region.Chunk(want.Pos)
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if !present {
		t.Fatal("the chunk this test just wrote reads back as absent")
	}

	if got.Pos != want.Pos {
		t.Fatalf("Pos = %v, want %v", got.Pos, want.Pos)
	}
	if got.Biomes != want.Biomes {
		t.Error("biomes did not survive the round trip")
	}
	for section := range dim.Sections() {
		wantSec, gotSec := want.Sections[section], got.Sections[section]
		if (wantSec == nil) != (gotSec == nil) {
			t.Fatalf("section %d: wrote nil=%v, read nil=%v", section, wantSec == nil, gotSec == nil)
		}
		if wantSec == nil {
			continue
		}
		for i, state := range wantSec.States() {
			if gotSec.At(i) != state {
				t.Fatalf("section %d block %d = %d, want %d", section, i, gotSec.At(i), state)
			}
		}
	}
}

func TestAMissingChunkReportsAbsentRatherThanEmpty(t *testing.T) {
	a, reg := javaCodec(t)

	written := newChunk(0, 0)
	setBlock(written, 0, 0, 0, reg.Intern("minecraft:stone", nil))
	dir := writeRegion(t, a, written)

	region, err := OpenRegion(dir, 0, 0, world.Overworld18(), a, reg.Air())
	if err != nil {
		t.Fatalf("OpenRegion: %v", err)
	}

	got, present, err := region.Chunk(world.ChunkPos{X: 7, Z: 9})
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if present || got != nil {
		t.Fatalf("an unwritten chunk read back as present (%v)", got)
	}
}

func TestAnAbsentRegionFileReportsNotExist(t *testing.T) {
	a, reg := javaCodec(t)

	_, err := OpenRegion(t.TempDir(), 0, 0, world.Overworld18(), a, reg.Air())
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("OpenRegion on an empty directory gave %v, want os.ErrNotExist", err)
	}
}

func TestACorruptSectorErrors(t *testing.T) {
	a, reg := javaCodec(t)

	written := newChunk(0, 0)
	setBlock(written, 0, 0, 0, reg.Intern("minecraft:stone", nil))
	dir := writeRegion(t, a, written)
	path := filepath.Join(dir, "r.0.0.mca")

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read region: %v", err)
	}

	for name, corrupt := range map[string]func([]byte){
		"the chunk points past the end of the file": func(b []byte) {
			b[0], b[1], b[2] = 0xFF, 0xFF, 0xFF
		},
		"the chunk points into the header": func(b []byte) {
			b[0], b[1], b[2] = 0, 0, 1
		},
		"the payload length is impossible": func(b []byte) {
			b[2*sectorSize], b[2*sectorSize+1] = 0x7F, 0xFF
		},
		"the compressed payload is garbage": func(b []byte) {
			for i := 2*sectorSize + 5; i < 2*sectorSize+64 && i < len(b); i++ {
				b[i] = 0xAA
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			broken := append([]byte(nil), raw...)
			corrupt(broken)
			if err := os.WriteFile(path, broken, 0o644); err != nil {
				t.Fatalf("write region: %v", err)
			}

			region, err := OpenRegion(dir, 0, 0, world.Overworld18(), a, reg.Air())
			if err != nil {
				return // refusing to open is also a refusal
			}
			if _, _, err := region.Chunk(world.ChunkPos{}); err == nil {
				t.Fatal("a corrupt region decoded without error")
			}
		})
	}
}

func TestAChunkFromTheWrongRegionIsRefused(t *testing.T) {
	a, reg := javaCodec(t)

	written := newChunk(0, 0)
	setBlock(written, 0, 0, 0, reg.Intern("minecraft:stone", nil))
	dir := writeRegion(t, a, written)

	region, err := OpenRegion(dir, 0, 0, world.Overworld18(), a, reg.Air())
	if err != nil {
		t.Fatalf("OpenRegion: %v", err)
	}
	if _, _, err := region.Chunk(world.ChunkPos{X: 40, Z: 0}); err == nil {
		t.Fatal("a chunk from another region decoded without error")
	}
}

// A block ID past 255 rides in the Add nibble array, which is the one part of
// the section format only a modded world reaches. Java 1.8 names no such
// block, so this goes through the identity codec rather than a registry.
func TestAHighBlockIDRoundTripsThroughAdd(t *testing.T) {
	const highState = 300<<4 | 5

	written := newChunk(0, 0)
	setBlock(written, 1, 2, 3, highState)

	dir := t.TempDir()
	payload, err := EncodeChunkNBT(written, identityEncoder{})
	if err != nil {
		t.Fatalf("EncodeChunkNBT: %v", err)
	}
	if err := SaveRegion(dir, 0, 0, map[world.ChunkPos]Payload{{}: {NBT: payload}}); err != nil {
		t.Fatalf("SaveRegion: %v", err)
	}

	region, err := OpenRegion(dir, 0, 0, world.Overworld18(), identityEncoder{}, 0)
	if err != nil {
		t.Fatalf("OpenRegion: %v", err)
	}
	got, present, err := region.Chunk(world.ChunkPos{})
	if err != nil || !present {
		t.Fatalf("Chunk: %v (present=%v)", err, present)
	}
	if state := got.Sections[0].At(world.SectionBlockIndex(1, 2, 3)); state != highState {
		t.Fatalf("block = %d, want %d — the Add nibble did not survive", state, highState)
	}
}

// TestAVanillaRegionReads is the check that this reader handles a file it did
// not write.
//
// testdata/r.0.0.mca holds nine chunks generated and saved by a vanilla 1.8.9
// server. How it was made, why nine of the 225 chunks that server wrote are
// here rather than all of them, and what the file was scanned for before it
// was committed are in
// docs/verification/2026-08-19-vanilla-region-fixture.md. No player ever
// joined the world it came from.
//
// The second opinion this replaces was assertUpstreamAccepts, which says a
// payload this package writes is well-formed NBT. It cannot say this package
// reads what somebody else's writer produced, which is what a stored world
// costs if it is wrong.
func TestAVanillaRegionReads(t *testing.T) {
	a, reg := javaCodec(t)
	region, err := OpenRegion("testdata", 0, 0, world.Overworld18(), a, reg.Air())
	if err != nil {
		t.Fatalf("OpenRegion: %v", err)
	}

	air := reg.Air()
	stone := reg.Intern("minecraft:stone", nil)

	found, withStone := 0, 0
	for cx := range 32 {
		for cz := range 32 {
			c, present, err := region.Chunk(world.ChunkPos{X: cx, Z: cz})
			if err != nil {
				t.Fatalf("chunk (%d,%d): %v", cx, cz, err)
			}
			if !present || c == nil {
				continue
			}
			found++

			solid, stones := 0, 0
			for _, section := range c.Sections {
				if section == nil {
					continue
				}
				for x := range 16 {
					for y := range 16 {
						for z := range 16 {
							switch section.At(world.SectionBlockIndex(x, y, z)) {
							case air:
							case stone:
								stones++
								solid++
							default:
								solid++
							}
						}
					}
				}
			}
			if solid == 0 {
				t.Errorf("chunk (%d,%d) came back with nothing but air", cx, cz)
			}
			if stones > 0 {
				withStone++
			}
		}
	}

	if found != vanillaFixtureChunks {
		t.Fatalf("read %d chunks from the vanilla fixture, want %d", found, vanillaFixtureChunks)
	}
	if withStone != vanillaFixtureChunks {
		t.Errorf("%d of %d chunks hold stone; a generated overworld column holds stone", withStone, vanillaFixtureChunks)
	}
}

// vanillaFixtureChunks is how many chunks testdata/r.0.0.mca holds. A reader
// that silently skipped one would otherwise pass a test that only asked for
// more than none.
const vanillaFixtureChunks = 9

// TestAChestRoundTripsThroughARegionFile is where the containers live now:
// inside the chunk, written where vanilla writes them, so a chunk save carries
// them and an external tool can find them.
func TestAChestRoundTripsThroughARegionFile(t *testing.T) {
	a, reg := javaCodec(t)

	written := newChunk(1, 1)
	setBlock(written, 4, 5, 6, reg.Intern("minecraft:chest", world.Properties{{Key: "metadata", Value: "2"}}))

	contents := world.EmptyChest()
	contents[0] = world.ItemStack{BlockID: 1, ItemCount: 64}                   // stone
	contents[13] = world.ItemStack{BlockID: 278, ItemCount: 1, ItemDamage: 42} // a worn diamond pickaxe
	written.Chests = map[world.BlockPos]world.ChestContents{
		{X: 1*16 + 4, Y: 5, Z: 1*16 + 6}: contents,
	}

	dir := writeRegion(t, a, written)
	region, err := OpenRegion(dir, 0, 0, world.Overworld18(), a, reg.Air())
	if err != nil {
		t.Fatalf("OpenRegion: %v", err)
	}

	got, present, err := region.Chunk(written.Pos)
	if err != nil || !present {
		t.Fatalf("Chunk: %v (present=%v)", err, present)
	}

	pos := world.BlockPos{X: 1*16 + 4, Y: 5, Z: 1*16 + 6}
	back, ok := got.Chests[pos]
	if !ok {
		t.Fatalf("the chest at %v did not come back; chunk holds %v", pos, got.Chests)
	}
	if !back.Equal(contents) {
		t.Fatalf("chest contents came back as %v, want %v", back, contents)
	}
}

// A chunk with no containers still round-trips, and an unknown tile entity is
// skipped rather than refused: a world may hold furnaces and signs this server
// has no idea about.
func TestAChunkWithNoChestsRoundTrips(t *testing.T) {
	a, reg := javaCodec(t)

	written := newChunk(0, 0)
	setBlock(written, 0, 0, 0, reg.Intern("minecraft:stone", nil))

	dir := writeRegion(t, a, written)
	region, err := OpenRegion(dir, 0, 0, world.Overworld18(), a, reg.Air())
	if err != nil {
		t.Fatalf("OpenRegion: %v", err)
	}

	got, present, err := region.Chunk(world.ChunkPos{})
	if err != nil || !present {
		t.Fatalf("Chunk: %v (present=%v)", err, present)
	}
	if len(got.Chests) != 0 {
		t.Fatalf("a chunk with no chests came back with %v", got.Chests)
	}
}

// TestAVanillaChestReads is the other half of the fixture: only a region file
// a vanilla server wrote can confirm that the Items list, the Slot byte, and
// the id string are what an external reader expects.
//
// The chest is the one edit made to the fixture world, through the vanilla
// server's own /setblock, so vanilla's tile-entity writer produced every byte
// read back here. Stone is a full stack in the first slot and a damaged
// diamond pickaxe sits in slot 13, which is where a chest's second row starts.
func TestAVanillaChestReads(t *testing.T) {
	a, reg := javaCodec(t)
	region, err := OpenRegion("testdata", 0, 0, world.Overworld18(), a, reg.Air())
	if err != nil {
		t.Fatalf("OpenRegion: %v", err)
	}

	c, present, err := region.Chunk(world.ChunkPos{X: 4, Z: 12})
	if err != nil || !present {
		t.Fatalf("the chunk holding the fixture chest: %v (present=%v)", err, present)
	}

	at := world.BlockPos{X: 72, Y: 80, Z: 200}

	// The block and its tile entity are two records in the file, and a reader
	// that found one without the other would leave a chest nothing can open.
	chest := reg.Intern("minecraft:chest", world.Properties{{Key: "metadata", Value: "2"}})
	section := c.Sections[world.Overworld18().SectionIndex(at.Y)]
	if section == nil {
		t.Fatalf("the section holding the fixture chest came back empty")
	}
	if state := section.At(world.SectionBlockIndex(at.X&0xF, at.Y&0xF, at.Z&0xF)); state != chest {
		t.Errorf("the block at %v is %d, want the chest state %d", at, state, chest)
	}

	contents, ok := c.Chests[at]
	if !ok {
		t.Fatalf("the vanilla chest at %v did not come back; chunk holds %v", at, c.Chests)
	}

	stone, okStone := a.ItemID("minecraft:stone")
	pickaxe, okPickaxe := a.ItemID("minecraft:diamond_pickaxe")
	if !okStone || !okPickaxe {
		t.Fatalf("this version does not name the fixture's items: stone=%v pickaxe=%v", okStone, okPickaxe)
	}

	want := world.EmptyChest()
	want[0] = world.ItemStack{BlockID: stone, ItemCount: 64}
	want[13] = world.ItemStack{BlockID: pickaxe, ItemCount: 1, ItemDamage: 42}
	if !contents.Equal(want) {
		t.Fatalf("the vanilla chest reads as %v, want %v", contents, want)
	}
}
