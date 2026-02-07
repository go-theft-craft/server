package conn

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	pkt "github.com/OCharnyshevich/minecraft-server/internal/gamedata/versions/pc_1_8"
	mcnet "github.com/OCharnyshevich/minecraft-server/internal/server/net"
	"github.com/OCharnyshevich/minecraft-server/internal/server/packet"
	"github.com/OCharnyshevich/minecraft-server/internal/server/player"
)

func (c *Connection) startPlay(username, uuid string, skinProps []player.SkinProperty) error {
	c.log = c.log.With("player", username)

	uuidBytes := parseUUID(uuid)
	entityID := c.players.AllocateEntityID()
	c.self = player.NewPlayer(entityID, uuid, uuidBytes, username, skinProps, c.writePacket)

	// 1. Join Game
	if err := c.writePacket(&pkt.Login{
		EntityID:         entityID,
		GameMode:         packet.GameModeCreative,
		Dimension:        packet.DimensionOverworld,
		Difficulty:       packet.DifficultyEasy,
		MaxPlayers:       uint8(c.cfg.MaxPlayers),
		LevelType:        c.cfg.GeneratorType,
		ReducedDebugInfo: false,
	}); err != nil {
		return fmt.Errorf("write join game: %w", err)
	}

	spawnY := c.world.SpawnHeight()

	// 2. Spawn Position
	if err := c.writePacket(&pkt.SpawnPosition{
		Location: mcnet.EncodePosition(0, spawnY, 0),
	}); err != nil {
		return fmt.Errorf("write spawn position: %w", err)
	}

	// 3. Player Abilities (Creative: Invulnerable + AllowFlight + CreativeMode)
	if err := c.writePacket(&pkt.AbilitiesCB{
		Flags:        packet.AbilityInvulnerable | packet.AbilityAllowFlight | packet.AbilityCreativeMode,
		FlyingSpeed:  0.05,
		WalkingSpeed: 0.1,
	}); err != nil {
		return fmt.Errorf("write player abilities: %w", err)
	}

	// 4. Player Position And Look
	if err := c.writePacket(&pkt.PositionCB{
		X:     0.5,
		Y:     float64(spawnY),
		Z:     0.5,
		Yaw:   0,
		Pitch: 0,
		Flags: 0x00, // all absolute
	}); err != nil {
		return fmt.Errorf("write position and look: %w", err)
	}

	// 5. Chunk Data (view distance radius grid)
	if err := c.world.WriteChunkGrid(c.rw, c.cfg.ViewDistance); err != nil {
		return fmt.Errorf("write chunk grid: %w", err)
	}

	// 6. Chat Message — "Hello, world!"
	if err := c.writePacket(&pkt.ChatCB{
		Message:  `{"text":"Hello, world!","color":"gold"}`,
		Position: 0,
	}); err != nil {
		return fmt.Errorf("write chat message: %w", err)
	}

	// 7. Register with player manager (sends cross-wise PlayerInfo + spawns).
	c.players.Add(c.self)

	// 8. Start KeepAlive goroutine
	go c.keepAliveLoop()

	c.log.Info("join sequence complete", "entityID", entityID)
	return nil
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
					_ = c.writePacket(&pkt.KickDisconnect{
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

			if err := c.writePacket(&pkt.KeepAliveCB{
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
		var p pkt.KeepAliveSB
		if err := mcnet.Unmarshal(data, &p); err != nil {
			return fmt.Errorf("unmarshal keep alive: %w", err)
		}
		c.mu.Lock()
		if p.KeepAliveID == c.lastKeepAliveID {
			c.keepAliveAcked = true
		}
		c.mu.Unlock()

	case 0x01: // Chat Message
		var p pkt.ChatSB
		if err := mcnet.Unmarshal(data, &p); err != nil {
			return fmt.Errorf("unmarshal chat: %w", err)
		}
		c.log.Info("chat", "message", p.Message)
		if c.handleCommand(p.Message) {
			break
		}
		chatJSON := fmt.Sprintf(
			`{"translate":"chat.type.text","with":[%s,%s]}`,
			escapeJSON(c.self.Username), escapeJSON(p.Message),
		)
		c.players.Broadcast(&pkt.ChatCB{
			Message:  chatJSON,
			Position: 0,
		})

	case 0x03: // Player (ground state)
		// heartbeat, ignore

	case 0x04: // Player Position
		var p pkt.PositionSB
		if err := mcnet.Unmarshal(data, &p); err != nil {
			return fmt.Errorf("unmarshal player position: %w", err)
		}
		c.handlePositionUpdate(p.X, p.Y, p.Z, 0, 0, p.OnGround, true, false)

	case 0x05: // Player Look
		var p pkt.Look
		if err := mcnet.Unmarshal(data, &p); err != nil {
			return fmt.Errorf("unmarshal player look: %w", err)
		}
		c.handleLookUpdate(p.Yaw, p.Pitch, p.OnGround)

	case 0x06: // Player Position And Look
		var p pkt.PositionLook
		if err := mcnet.Unmarshal(data, &p); err != nil {
			return fmt.Errorf("unmarshal player position and look: %w", err)
		}
		c.handlePositionUpdate(p.X, p.Y, p.Z, p.Yaw, p.Pitch, p.OnGround, true, true)

	case 0x07: // Block Dig
		return c.handleBlockDig(data)

	case 0x08: // Block Place
		return c.handleBlockPlace(data)

	case 0x09: // Held Item Change
		var p pkt.HeldItemSlotSB
		if err := mcnet.Unmarshal(data, &p); err != nil {
			return fmt.Errorf("unmarshal held item slot: %w", err)
		}
		if p.SlotID < 0 || p.SlotID > 8 {
			return nil
		}
		c.self.Inventory.SetHeldSlot(p.SlotID)
		heldItem := c.self.Inventory.HeldItem()
		eqData := player.BuildSingleEquipment(c.self.EntityID, 0, heldItem)
		c.players.BroadcastToTrackers(&pkt.EntityEquipment{Data: eqData}, c.self.EntityID)

	case 0x0A: // Animation (arm swing)
		c.players.BroadcastToTrackers(&pkt.Animation{
			EntityID:  c.self.EntityID,
			Animation: 0, // swing arm
		}, c.self.EntityID)

	case 0x0B: // Entity Action
		var p pkt.EntityAction
		if err := mcnet.Unmarshal(data, &p); err != nil {
			return fmt.Errorf("unmarshal entity action: %w", err)
		}
		switch p.ActionID {
		case 0: // start sneak
			c.self.SetSneaking(true)
			c.players.BroadcastEntityMetadata(c.self)
		case 1: // stop sneak
			c.self.SetSneaking(false)
			c.players.BroadcastEntityMetadata(c.self)
		case 3: // start sprint
			c.self.SetSprinting(true)
			c.players.BroadcastEntityMetadata(c.self)
		case 4: // stop sprint
			c.self.SetSprinting(false)
			c.players.BroadcastEntityMetadata(c.self)
		}

	case 0x15: // Client Settings
		var p pkt.Settings
		if err := mcnet.Unmarshal(data, &p); err != nil {
			return fmt.Errorf("unmarshal client settings: %w", err)
		}
		c.log.Info("client settings", "locale", p.Locale, "viewDistance", p.ViewDistance)
		c.self.SetSkinParts(p.SkinParts)
		c.players.BroadcastEntityMetadata(c.self)

	default:
		// ignore unknown packets silently
	}

	return nil
}

func (c *Connection) handlePositionUpdate(x, y, z float64, yaw, pitch float32, onGround bool, posChanged, lookChanged bool) {
	if c.self == nil {
		return
	}

	// Preserve current look if only position changed.
	if !lookChanged {
		pos := c.self.GetPosition()
		yaw = pos.Yaw
		pitch = pos.Pitch
	}

	oldFX, oldFY, oldFZ, newFX, newFY, newFZ := c.self.SetPosition(x, y, z, yaw, pitch, onGround)

	dx := newFX - oldFX
	dy := newFY - oldFY
	dz := newFZ - oldFZ

	yawAngle := player.DegreesToAngle(yaw)
	pitchAngle := player.DegreesToAngle(pitch)
	eid := c.self.EntityID

	if posChanged && lookChanged && player.DeltaFitsInByte(dx, dy, dz) {
		c.players.BroadcastToTrackers(&pkt.EntityMoveLook{
			EntityID: eid,
			DX:       int8(dx),
			DY:       int8(dy),
			DZ:       int8(dz),
			Yaw:      yawAngle,
			Pitch:    pitchAngle,
			OnGround: onGround,
		}, eid)
	} else if posChanged && !lookChanged && player.DeltaFitsInByte(dx, dy, dz) {
		c.players.BroadcastToTrackers(&pkt.RelEntityMove{
			EntityID: eid,
			DX:       int8(dx),
			DY:       int8(dy),
			DZ:       int8(dz),
			OnGround: onGround,
		}, eid)
	} else if posChanged {
		c.players.BroadcastToTrackers(&pkt.EntityTeleport{
			EntityID: eid,
			X:        newFX,
			Y:        newFY,
			Z:        newFZ,
			Yaw:      yawAngle,
			Pitch:    pitchAngle,
			OnGround: onGround,
		}, eid)
	}

	if lookChanged {
		c.players.BroadcastToTrackers(&pkt.EntityHeadRotation{
			EntityID: eid,
			HeadYaw:  yawAngle,
		}, eid)
	}

	// Sprint particles: send block crack particles at player's feet.
	if posChanged && c.self.IsSprinting() {
		blockBelow := c.world.GetBlock(int(math.Floor(x)), int(math.Floor(y))-1, int(math.Floor(z)))
		if blockBelow != 0 {
			c.players.BroadcastToTrackers(&pkt.WorldParticles{
				Data: buildSprintParticles(x, y, z, blockBelow),
			}, eid)
		}
	}

	c.players.UpdateTracking(c.self)
}

func (c *Connection) handleLookUpdate(yaw, pitch float32, onGround bool) {
	if c.self == nil {
		return
	}

	c.self.UpdateLook(yaw, pitch, onGround)

	yawAngle := player.DegreesToAngle(yaw)
	pitchAngle := player.DegreesToAngle(pitch)
	eid := c.self.EntityID

	c.players.BroadcastToTrackers(&pkt.EntityLook{
		EntityID: eid,
		Yaw:      yawAngle,
		Pitch:    pitchAngle,
		OnGround: onGround,
	}, eid)

	c.players.BroadcastToTrackers(&pkt.EntityHeadRotation{
		EntityID: eid,
		HeadYaw:  yawAngle,
	}, eid)
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
		oldBlockState := c.world.GetBlock(x, y, z)
		c.world.SetBlock(x, y, z, 0) // air
		blockChange := &pkt.BlockChange{
			Location: posVal,
			Type:     0,
		}
		c.players.BroadcastExcept(blockChange, c.self.EntityID)

		// Broadcast block break effect (particles + sound).
		if oldBlockState != 0 {
			c.players.BroadcastToTrackers(&pkt.WorldEvent{
				EffectID: 2001,
				Location: posVal,
				Data:     oldBlockState,
				Global:   false,
			}, c.self.EntityID)
		}

		return c.writePacket(blockChange)
	}

	// status 3 = drop stack, status 4 = drop single item
	if status == 3 || status == 4 {
		heldSlot := c.self.Inventory.GetHeldSlot()
		heldItem := c.self.Inventory.HeldItem()
		if heldItem.IsEmpty() {
			return nil
		}

		var dropped player.Slot
		if status == 4 {
			dropped = c.self.Inventory.RemoveOne(int(heldSlot))
		} else {
			dropped = heldItem
			c.self.Inventory.SetSlot(int(heldSlot), player.EmptySlot)
		}

		if !dropped.IsEmpty() {
			pos := c.self.GetPosition()
			c.players.SpawnItemEntity(c.self.EntityID, dropped, pos.X, pos.Y+1.3, pos.Z, pos.Yaw)
		}

		// Update held item for trackers.
		newHeld := c.self.Inventory.HeldItem()
		eqData := player.BuildSingleEquipment(c.self.EntityID, 0, newHeld)
		c.players.BroadcastToTrackers(&pkt.EntityEquipment{Data: eqData}, c.self.EntityID)
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

	blockChange := &pkt.BlockChange{
		Location: mcnet.EncodePosition(x, y, z),
		Type:     stateID,
	}
	c.players.BroadcastExcept(blockChange, c.self.EntityID)
	return c.writePacket(blockChange)
}

// parseUUID parses a hyphenated UUID string into 16 bytes.
func parseUUID(s string) [16]byte {
	var uuid [16]byte
	hexStr := strings.ReplaceAll(s, "-", "")
	b, _ := hex.DecodeString(hexStr)
	copy(uuid[:], b)
	return uuid
}

// escapeJSON marshals a string to a JSON string literal (with quotes).
func escapeJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// buildSprintParticles builds WorldParticles raw data for sprint block-crack particles.
// Particle ID 37 = block crack, with block state as additional data.
func buildSprintParticles(x, y, z float64, blockState int32) []byte {
	var buf bytes.Buffer

	_ = binary.Write(&buf, binary.BigEndian, int32(37))    // particle ID: block crack
	_ = binary.Write(&buf, binary.BigEndian, false)        // long distance
	_ = binary.Write(&buf, binary.BigEndian, float32(x))   // x
	_ = binary.Write(&buf, binary.BigEndian, float32(y))   // y
	_ = binary.Write(&buf, binary.BigEndian, float32(z))   // z
	_ = binary.Write(&buf, binary.BigEndian, float32(0.5)) // offset X
	_ = binary.Write(&buf, binary.BigEndian, float32(0.1)) // offset Y
	_ = binary.Write(&buf, binary.BigEndian, float32(0.5)) // offset Z
	_ = binary.Write(&buf, binary.BigEndian, float32(0.0)) // speed
	_ = binary.Write(&buf, binary.BigEndian, int32(5))     // count
	_, _ = mcnet.WriteVarInt(&buf, blockState)             // block state data

	return buf.Bytes()
}
