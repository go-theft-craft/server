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
		GameMode:         packet.GameModeCreative,
		Dimension:        packet.DimensionOverworld,
		Difficulty:       packet.DifficultyEasy,
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

	// 3. Player Abilities (Creative: Invulnerable + AllowFlight + CreativeMode)
	if err := c.writePacket(&packet.PlayerAbilities{
		Flags:        packet.AbilityInvulnerable | packet.AbilityAllowFlight | packet.AbilityCreativeMode,
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
	if err := world.WriteChunkGrid(c.rw); err != nil {
		return fmt.Errorf("write chunk grid: %w", err)
	}

	// 5b. Replay block overrides so dig/place changes survive relogin.
	if err := c.sendBlockOverrides(); err != nil {
		return fmt.Errorf("send block overrides: %w", err)
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

	// Gamemode: VarInt (Creative)
	if _, err := mcnet.WriteVarInt(&buf, int32(packet.GameModeCreative)); err != nil {
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
			c.mu.Lock()
			if !c.keepAliveAcked && id > 0 {
				if time.Since(c.lastKeepAliveSent) > 30*time.Second {
					c.mu.Unlock()
					_ = c.writePacket(&packet.PlayDisconnect{
						Reason: `{"text":"Timed out"}`,
					})
					c.disconnect("keepalive timeout")
					return
				}
			}
			id++
			c.lastKeepAliveID = id
			c.lastKeepAliveSent = time.Now()
			c.keepAliveAcked = false
			c.mu.Unlock()

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
		c.mu.Lock()
		if pkt.KeepAliveID == c.lastKeepAliveID {
			c.keepAliveAcked = true
		}
		c.mu.Unlock()

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

	case 0x07: // Block Dig
		return c.handleBlockDig(data)

	case 0x08: // Block Place
		return c.handleBlockPlace(data)

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

func (c *Connection) handleBlockDig(data []byte) error {
	r := bytes.NewReader(data)

	status, _, err := mcnet.ReadVarInt(r)
	if err != nil {
		return fmt.Errorf("read dig status: %w", err)
	}

	posVal, err := mcnet.ReadI64(r)
	if err != nil {
		return fmt.Errorf("read dig position: %w", err)
	}
	x, y, z := mcnet.DecodePosition(posVal)

	// status 0 = Started digging, 2 = Finished digging
	// In Creative mode, the client sends status=0 for instant break.
	if status == 0 || status == 2 {
		c.world.SetBlock(x, y, z, 0) // air
		return c.writePacket(&packet.BlockChange{
			Location: posVal,
			BlockID:  0,
		})
	}

	return nil
}

func (c *Connection) handleBlockPlace(data []byte) error {
	r := bytes.NewReader(data)

	posVal, err := mcnet.ReadI64(r)
	if err != nil {
		return fmt.Errorf("read place position: %w", err)
	}

	face, err := mcnet.ReadI8(r)
	if err != nil {
		return fmt.Errorf("read place face: %w", err)
	}

	slot, err := readSlot(r)
	if err != nil {
		return fmt.Errorf("read place slot: %w", err)
	}

	// Read cursor position (3 x u8) - we don't use these but must consume them.
	if _, err := mcnet.ReadU8(r); err != nil {
		return fmt.Errorf("read cursor x: %w", err)
	}
	if _, err := mcnet.ReadU8(r); err != nil {
		return fmt.Errorf("read cursor y: %w", err)
	}
	if _, err := mcnet.ReadU8(r); err != nil {
		return fmt.Errorf("read cursor z: %w", err)
	}

	// Special position -1,-1,-1 means the player is using an item (not placing a block).
	if posVal == -1 {
		return nil
	}

	// Empty slot means no block to place.
	if slot.BlockID <= 0 {
		return nil
	}

	x, y, z := mcnet.DecodePosition(posVal)

	// Compute target position from face direction.
	switch face {
	case 0: // -Y
		y--
	case 1: // +Y
		y++
	case 2: // -Z
		z--
	case 3: // +Z
		z++
	case 4: // -X
		x--
	case 5: // +X
		x++
	default:
		return nil
	}

	stateID := int32(slot.BlockID) << 4
	c.world.SetBlock(x, y, z, stateID)

	return c.writePacket(&packet.BlockChange{
		Location: mcnet.EncodePosition(x, y, z),
		BlockID:  stateID,
	})
}

// sendBlockOverrides sends BlockChange packets for all world overrides
// within the visible chunk grid so changes persist across relogins.
func (c *Connection) sendBlockOverrides() error {
	const chunkRange = 3 // chunks -3..3 → blocks -48..63
	minBlock := -chunkRange * 16
	maxBlock := (chunkRange+1)*16 - 1

	var sendErr error
	c.world.ForEachOverride(func(pos world.BlockPos, stateID int32) {
		if sendErr != nil {
			return
		}
		if pos.X < minBlock || pos.X > maxBlock || pos.Z < minBlock || pos.Z > maxBlock {
			return
		}
		sendErr = c.writePacket(&packet.BlockChange{
			Location: mcnet.EncodePosition(pos.X, pos.Y, pos.Z),
			BlockID:  stateID,
		})
	})
	return sendErr
}

// parseUUID parses a hyphenated UUID string into 16 bytes.
func parseUUID(s string) [16]byte {
	var uuid [16]byte
	hexStr := strings.ReplaceAll(s, "-", "")
	b, _ := hex.DecodeString(hexStr)
	copy(uuid[:], b)
	return uuid
}
