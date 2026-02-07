package conn

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	mcnet "github.com/OCharnyshevich/minecraft-server/internal/server/net"
	"github.com/OCharnyshevich/minecraft-server/internal/server/packet"
	"github.com/OCharnyshevich/minecraft-server/internal/server/world"
)

func (c *Connection) startPlay(username, uuid string) error {
	c.log = c.log.With("player", username)

	// 1. Join Game
	if err := c.writePacket(&packet.JoinGame{
		EntityID:         1,
		GameMode:         1, // Creative
		Dimension:        0, // Overworld
		Difficulty:       1, // Easy
		MaxPlayers:       uint8(c.cfg.MaxPlayers),
		LevelType:        "flat",
		ReducedDebugInfo: false,
	}); err != nil {
		return fmt.Errorf("write join game: %w", err)
	}

	// 2. Spawn Position
	if err := c.writePacket(&packet.SpawnPosition{
		Location: mcnet.EncodePosition(0, 4, 0),
	}); err != nil {
		return fmt.Errorf("write spawn position: %w", err)
	}

	// 3. Player Abilities (Creative: Invulnerable + AllowFlight + CreativeMode = 0x0D)
	if err := c.writePacket(&packet.PlayerAbilities{
		Flags:        0x0D,
		FlyingSpeed:  0.05,
		WalkingSpeed: 0.1,
	}); err != nil {
		return fmt.Errorf("write player abilities: %w", err)
	}

	// 4. Player Position And Look
	if err := c.writePacket(&packet.PlayerPositionAndLook{
		X:     0.5,
		Y:     4.0,
		Z:     0.5,
		Yaw:   0,
		Pitch: 0,
		Flags: 0x00, // all absolute
	}); err != nil {
		return fmt.Errorf("write position and look: %w", err)
	}

	// 5. Chunk Data (7×7 grid)
	if err := world.WriteChunkGrid(c.conn); err != nil {
		return fmt.Errorf("write chunk grid: %w", err)
	}

	// 6. Player Info (Add Player action)
	if err := c.writePlayerInfo(username, uuid); err != nil {
		return fmt.Errorf("write player info: %w", err)
	}

	// 7. Chat Message — "Hello, world!"
	if err := c.writePacket(&packet.ChatMessage{
		JSONData: `{"text":"Hello, world!","color":"gold"}`,
		Position: 0,
	}); err != nil {
		return fmt.Errorf("write chat message: %w", err)
	}

	// 8. Start KeepAlive goroutine
	go c.keepAliveLoop()

	c.log.Info("join sequence complete")
	return nil
}

func (c *Connection) writePlayerInfo(username, uuid string) error {
	// PlayerInfo packet with action=0 (Add Player), one entry
	var buf bytes.Buffer

	// Action: VarInt = 0 (Add Player)
	if _, err := mcnet.WriteVarInt(&buf, 0); err != nil {
		return fmt.Errorf("write player info action: %w", err)
	}
	// Number of players: VarInt = 1
	if _, err := mcnet.WriteVarInt(&buf, 1); err != nil {
		return fmt.Errorf("write player info count: %w", err)
	}

	// UUID: 16 bytes
	uuidBytes := parseUUID(uuid)
	buf.Write(uuidBytes[:])

	// Name
	if _, err := mcnet.WriteString(&buf, username); err != nil {
		return fmt.Errorf("write player info name: %w", err)
	}

	// Number of properties: VarInt = 0
	if _, err := mcnet.WriteVarInt(&buf, 0); err != nil {
		return fmt.Errorf("write player info properties: %w", err)
	}

	// Gamemode: VarInt = 1 (Creative)
	if _, err := mcnet.WriteVarInt(&buf, 1); err != nil {
		return fmt.Errorf("write player info gamemode: %w", err)
	}

	// Ping: VarInt = 0
	if _, err := mcnet.WriteVarInt(&buf, 0); err != nil {
		return fmt.Errorf("write player info ping: %w", err)
	}

	// Has display name: bool = false
	if err := binary.Write(&buf, binary.BigEndian, uint8(0)); err != nil {
		return fmt.Errorf("write player info display name: %w", err)
	}

	return c.writePacket(&packet.PlayerInfo{
		Data: buf.Bytes(),
	})
}

func (c *Connection) keepAliveLoop() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	var id int32
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			id++
			if err := c.writePacket(&packet.KeepAliveClientbound{
				KeepAliveID: id,
			}); err != nil {
				c.log.Error("keep alive write failed", "error", err)
				c.cancel()
				return
			}
		}
	}
}

func (c *Connection) handlePlay(packetID int32, data []byte) error {
	switch packetID {
	case 0x00: // KeepAlive
		var pkt packet.KeepAliveServerbound
		if err := mcnet.Unmarshal(data, &pkt); err != nil {
			return fmt.Errorf("unmarshal keep alive: %w", err)
		}
		// acknowledged, no action needed

	case 0x01: // Chat Message
		var pkt packet.ChatMessageServerbound
		if err := mcnet.Unmarshal(data, &pkt); err != nil {
			return fmt.Errorf("unmarshal chat: %w", err)
		}
		c.log.Info("chat", "message", pkt.Message)

	case 0x03: // Player (ground state)
		// heartbeat, ignore

	case 0x04: // Player Position
		// track position silently

	case 0x05: // Player Look
		// track look silently

	case 0x06: // Player Position And Look
		// track position+look silently

	case 0x15: // Client Settings
		var pkt packet.ClientSettings
		if err := mcnet.Unmarshal(data, &pkt); err != nil {
			return fmt.Errorf("unmarshal client settings: %w", err)
		}
		c.log.Info("client settings", "locale", pkt.Locale, "viewDistance", pkt.ViewDistance)

	default:
		// ignore unknown packets silently
	}

	return nil
}

// parseUUID parses a hyphenated UUID string into 16 bytes.
func parseUUID(s string) [16]byte {
	var uuid [16]byte
	hexStr := strings.ReplaceAll(s, "-", "")
	b, _ := hex.DecodeString(hexStr)
	copy(uuid[:], b)
	return uuid
}
