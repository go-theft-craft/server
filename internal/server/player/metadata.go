package player

import (
	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
)

// Metadata type IDs for MC 1.8 entity metadata format.
const (
	metaTypeByte  = 0
	metaTypeShort = 1
	metaTypeInt   = 2
	metaTypeFloat = 3
	metaTypeSlot  = 5
)

// metaByteItem builds a single byte-type metadata entry (index, value byte).
// The generated codec packs the header as (Type<<5)|Key, matching the 1.8 wire
// header (index & 0x1F) | (typeID << 5), and appends the 0x7F terminator on
// encode, so entries carry no terminator of their own.
func metaByteItem(index byte, val byte) v1_8.EntityMetadataItem {
	return v1_8.EntityMetadataItem{
		AnonymousBitField1: v1_8.EntityMetadataItemAnonymousBitField1Bits{
			Type: metaTypeByte,
			Key:  index,
		},
		Value: v1_8.EntityMetadataItemValueSwitch{Case0: int8(val)},
	}
}

// BuildEntityMetadata builds entity metadata for broadcasting state changes.
// Includes entityFlags (index 0) and skinParts (index 10).
func BuildEntityMetadata(p *Player) v1_8.EntityMetadata {
	return v1_8.EntityMetadata{
		metaByteItem(0, p.GetEntityFlags()),
		metaByteItem(10, p.GetSkinParts()),
	}
}

// BuildSpawnMetadata builds entity metadata for the NamedEntitySpawn packet.
func BuildSpawnMetadata(p *Player) v1_8.EntityMetadata {
	return BuildEntityMetadata(p)
}
