// Package pc_1_8 holds the last wire types this repository owns.
//
// Every game-data registry that used to live beside them — blocks, items,
// recipes, materials, and the rest — now comes from minecraft-protocol's data
// package. These packet structs stayed behind deliberately: M3 migrates the
// transport and the status and login packets, and M6 replaces them with
// generated types and deletes this package along with cmd/codegen, cmd/dmd,
// and the downloaded schemas they read.
//
// The doc comment lives here rather than in packets.go because that file is
// regenerated, which would discard it.
package pc_1_8

// The wire constants the packet structs in this package are written against.
//
// VersionName is what the status response advertises. It stays "1.8.8" rather
// than following minecraft-protocol's "1.8.9", because both are protocol 47
// and this migration does not change a byte the server puts on the wire.
// Reconciling the two names is a decision on its own, not a side effect.
const (
	ProtocolVersion int32  = 47
	VersionName     string = "1.8.8"
	MetadataEnd     byte   = 0x7F
)
