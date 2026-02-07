package player

import (
	"bytes"

	pkt "github.com/OCharnyshevich/minecraft-server/internal/gamedata/versions/pc_1_8"
)

// Metadata type IDs for MC 1.8 entity metadata format.
const (
	metaTypeByte  = 0
	metaTypeShort = 1
	metaTypeInt   = 2
	metaTypeFloat = 3
	metaTypeSlot  = 5
)

// writeMetaByte writes a single byte-type metadata entry.
// Header: (index & 0x1F) | (typeID << 5), then the value byte.
func writeMetaByte(buf *bytes.Buffer, index byte, val byte) {
	buf.WriteByte((index & 0x1F) | (metaTypeByte << 5))
	buf.WriteByte(val)
}

// BuildEntityMetadata builds entity metadata bytes for broadcasting state changes.
// Includes entityFlags (index 0) and skinParts (index 10).
func BuildEntityMetadata(p *Player) []byte {
	var buf bytes.Buffer

	writeMetaByte(&buf, 0, p.GetEntityFlags())
	writeMetaByte(&buf, 10, p.GetSkinParts())
	buf.WriteByte(pkt.MetadataEnd)

	return buf.Bytes()
}

// BuildSpawnMetadata builds entity metadata bytes for the NamedEntitySpawn packet.
func BuildSpawnMetadata(p *Player) []byte {
	return BuildEntityMetadata(p)
}
