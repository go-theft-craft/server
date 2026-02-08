package gamedata

type Protocol struct {
	Types  map[string]string
	Phases map[string]ProtocolPhase
}

type ProtocolPhase struct {
	ToClient ProtocolDirection
	ToServer ProtocolDirection
}

type ProtocolDirection struct {
	Packets []Packet
}

type Packet struct {
	Name   string
	ID     int
	Fields []PacketField
}

type PacketField struct {
	Name string
	Type string
}
