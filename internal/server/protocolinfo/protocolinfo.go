// Package protocolinfo holds the protocol 47 constants that are not packets.
//
// They lived in pkg/gamedata/versions/pc_1_8 beside the generated packet
// structs. M6.1 deletes that package, and these three have no generated
// counterpart: the protocol number is checkable against the descriptor, the
// advertised version name is deliberately different from it, and the metadata
// terminator is a codec detail the server's own entity metadata writer needs.
//
// minecraft-protocol's generated/java/v1_8 package now defines an equivalent
// for all three, but its exported VersionName is "1.8.9", which names the
// dataset rather than what a client is told; these constants are kept local
// on purpose so a later reader does not point this package at that one and
// silently change what the status response puts on the wire.
package protocolinfo

const (
	// ProtocolVersion is Java Edition 1.8's wire protocol number.
	ProtocolVersion int32 = 47

	// VersionName is what the status response advertises.
	//
	// Protocol 47 has two names and this is the one a client is told, which
	// M10 settled and minecraft-protocol's docs/version-names.md records:
	// "1.8.9" names the dataset, because that is what the data was published
	// as, and "1.8.8" is what protocol 47 clients call themselves and what
	// the independent Node implementation lists. So this constant agrees
	// with the generated data's MinecraftVersion and differs from its
	// VersionName, and the test pins it against the former rather than
	// against a literal alone.
	VersionName string = "1.8.8"

	// MetadataEnd terminates an entity metadata list in protocol 47.
	// Protocol 775 terminates at 0xFF instead, which is why this is a
	// version-scoped constant rather than a shared one.
	MetadataEnd byte = 0x7F
)
