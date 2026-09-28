package player

// byteEntry builds a single byte-type metadata entry.
func byteEntry(index uint8, val byte) MetadataEntry {
	return MetadataEntry{Index: index, Kind: MetadataByte, Byte: int8(val)}
}

// BuildEntityMetadata builds entity metadata for broadcasting state changes.
// Includes entityFlags (index 0) and skinParts (index 10).
func BuildEntityMetadata(p *Player) []MetadataEntry {
	return []MetadataEntry{
		byteEntry(0, p.GetEntityFlags()),
		byteEntry(10, p.GetSkinParts()),
	}
}

// BuildSpawnMetadata builds entity metadata for the NamedEntitySpawn packet.
func BuildSpawnMetadata(p *Player) []MetadataEntry {
	return BuildEntityMetadata(p)
}
