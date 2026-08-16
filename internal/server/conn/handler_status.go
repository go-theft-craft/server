package conn

import (
	"encoding/json"
	"fmt"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/server/internal/server/protocolinfo"
)

type statusResponse struct {
	Version     statusVersion `json:"version"`
	Players     statusPlayers `json:"players"`
	Description statusDesc    `json:"description"`
}

type statusVersion struct {
	Name     string `json:"name"`
	Protocol int    `json:"protocol"`
}

type statusPlayers struct {
	Max    int `json:"max"`
	Online int `json:"online"`
}

type statusDesc struct {
	Text string `json:"text"`
}

// handleStatus answers the two packets the status state allows, both as
// generated values.
//
// The response JSON is unchanged, field order included, because a fixture pins
// the bytes.
func (c *Connection) handleStatus(packet protocol.Packet) error {
	switch value := packet.Value.(type) {
	case *v1_8.StatusServerboundPingStart:
		return c.writeServerInfo()

	case *v1_8.StatusServerboundPing:
		// The payload is echoed exactly; a client measures its latency from
		// what it gets back.
		return c.send(&v1_8.StatusClientboundPing{Time: value.Time})

	default:
		return fmt.Errorf("unexpected status packet 0x%02X (%T)", packet.ID, packet.Value)
	}
}

func (c *Connection) writeServerInfo() error {
	response := statusResponse{
		Version: statusVersion{
			Name:     protocolinfo.VersionName,
			Protocol: int(protocolinfo.ProtocolVersion),
		},
		Players: statusPlayers{
			Max:    c.cfg.MaxPlayers,
			Online: c.players.PlayerCount(),
		},
		Description: statusDesc{
			Text: c.cfg.MOTD,
		},
	}

	encoded, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("marshal status response: %w", err)
	}

	return c.send(&v1_8.StatusClientboundServerInfo{Response: string(encoded)})
}
