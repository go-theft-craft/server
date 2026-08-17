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
// not write, and it is the one test in this package with no fixture.
//
// The plan calls for a region written by a vanilla 1.8.9 server: run vanilla
// locally, walk a small area, copy one region file into testdata, and confirm
// it holds no player data before committing it. That cannot be done from
// inside this repository — it needs a Mojang server jar and a running world —
// so the gap is recorded here rather than quietly omitted. What stands in for
// it today: every chunk this package writes is validated by
// minecraft-protocol's own NBT reader (see assertUpstreamAccepts), which has
// no stake in this writer and already caught one malformed structure.
func TestAVanillaRegionReads(t *testing.T) {
	if _, err := os.Stat(filepath.Join("testdata", "r.0.0.mca")); err != nil {
		t.Skip("no vanilla-written region fixture: see this test's comment")
	}

	a, reg := javaCodec(t)
	region, err := OpenRegion("testdata", 0, 0, world.Overworld18(), a, reg.Air())
	if err != nil {
		t.Fatalf("OpenRegion: %v", err)
	}

	found := 0
	for cx := range 32 {
		for cz := range 32 {
			c, present, err := region.Chunk(world.ChunkPos{X: cx, Z: cz})
			if err != nil {
				t.Fatalf("chunk (%d,%d): %v", cx, cz, err)
			}
			if present && c != nil {
				found++
			}
		}
	}
	if found == 0 {
		t.Fatal("the vanilla fixture holds no chunks")
	}
	t.Logf("read %d chunks from the vanilla fixture", found)
}

// TestAChestRoundTripsThroughARegionFile is where the containers live now:
// inside the chunk, written where vanilla writes them, so a chunk save carries
// them and an external tool can find them.
func TestAChestRoundTripsThroughARegionFile(t *testing.T) {
	a, reg := javaCodec(t)

	written := newChunk(1, 1)
	setBlock(written, 4, 5, 6, reg.Intern("minecraft:chest", world.Properties{{Key: "metadata", Value: "2"}}))

	contents := world.EmptyChest()
	contents[0] = world.ItemStack{ID: 1, Count: 64}               // stone
	contents[13] = world.ItemStack{ID: 278, Count: 1, Damage: 42} // a worn diamond pickaxe
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
	if back != contents {
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

// TestAVanillaChestReads is the other half of the missing fixture: only a
// region file a vanilla server wrote can confirm that the Items list, the Slot
// byte, and the id string are what an external reader expects. See
// TestAVanillaRegionReads for why it is not here.
func TestAVanillaChestReads(t *testing.T) {
	t.Skip("no vanilla-written region fixture: see TestAVanillaRegionReads")
}
