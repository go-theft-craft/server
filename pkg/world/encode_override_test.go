package world

import (
	"encoding/binary"
	"testing"

	"github.com/go-theft-craft/server/pkg/world/gen"
)

// blockInEncoded reads one block back out of an encoded chunk, which is what
// the client does with it.
func blockInEncoded(t *testing.T, w *World, x, y, z int) uint16 {
	t.Helper()

	cx, cz := x>>4, z>>4
	packet := w.EncodeChunk(cx, cz)

	sectionIdx := y >> 4
	if packet.BitMap&(1<<uint(sectionIdx)) == 0 {
		t.Fatalf("section %d is absent from the chunk packet (bitmap %016b)", sectionIdx, packet.BitMap)
	}

	// Sections are written in order, so the offset is how many present
	// sections come before this one.
	ordinal := 0
	for i := range sectionIdx {
		if packet.BitMap&(1<<uint(i)) != 0 {
			ordinal++
		}
	}

	lx, ly, lz := x&0xF, y&0xF, z&0xF
	idx := ordinal*sectionBlockBytes + (ly*256+lz*16+lx)*2

	return binary.LittleEndian.Uint16(packet.ChunkData[idx:])
}

// A block placed where the generator produced nothing has to survive being
// sent: it is stored, and the client is told about it by a block change, but
// the next chunk send is what a reconnecting or respawning player sees.
func TestEncodeChunk_OverrideInAnEmptySectionSurvives(t *testing.T) {
	w := NewWorld(gen.NewFlatGenerator(0))

	// Section 0 is where a flat generator puts its terrain, so pick a height
	// far above it: section 8 has no generated blocks at all.
	const x, y, z = 5, 130, 5

	chest := int32(54) << 4
	w.SetBlock(x, y, z, chest)

	if got := w.GetBlock(x, y, z); got != chest {
		t.Fatalf("world stored %d, want %d — the override was not kept at all", got, chest)
	}

	if got := blockInEncoded(t, w, x, y, z); int32(got) != chest {
		t.Errorf("encoded chunk has block %d, want %d", got, chest)
	}
}

// The same block inside a section the generator did fill, which is the case
// that already worked.
func TestEncodeChunk_OverrideInAGeneratedSectionSurvives(t *testing.T) {
	w := NewWorld(gen.NewFlatGenerator(0))

	const x, y, z = 5, 5, 5

	chest := int32(54) << 4
	w.SetBlock(x, y, z, chest)

	if got := blockInEncoded(t, w, x, y, z); int32(got) != chest {
		t.Errorf("encoded chunk has block %d, want %d", got, chest)
	}
}
