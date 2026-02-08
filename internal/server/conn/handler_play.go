package conn

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/OCharnyshevich/minecraft-server/internal/gamedata"
	pkt "github.com/OCharnyshevich/minecraft-server/internal/gamedata/versions/pc_1_8"
	mcnet "github.com/OCharnyshevich/minecraft-server/internal/server/net"
	"github.com/OCharnyshevich/minecraft-server/internal/server/packet"
	"github.com/OCharnyshevich/minecraft-server/internal/server/player"
	"github.com/OCharnyshevich/minecraft-server/internal/server/storage"
	"github.com/OCharnyshevich/minecraft-server/internal/server/world/gen"
)

func (c *Connection) startPlay(username, uuid string, skinProps []player.SkinProperty) error {
	c.log = c.log.With("player", username)

	uuidBytes := parseUUID(uuid)
	entityID := c.players.AllocateEntityID()
	c.self = player.NewPlayer(entityID, uuid, uuidBytes, username, skinProps, c.writePacket)

	// Try to load saved player data.
	var savedData *storage.PlayerData
	if c.storage != nil {
		var err error
		savedData, err = c.storage.LoadPlayer(uuid)
		if err != nil {
			c.log.Error("load player data", "error", err)
		}
	}

	gameMode := uint8(packet.GameModeCreative)
	spawnY := c.world.SpawnHeight()
	posX, posY, posZ := 0.5, float64(spawnY), 0.5
	var posYaw float32
	var posPitch float32

	if savedData != nil {
		gameMode = savedData.GameMode
		posX = savedData.Position.X
		posY = savedData.Position.Y
		posZ = savedData.Position.Z
		posYaw = savedData.Position.Yaw
		posPitch = savedData.Position.Pitch

		// Convert saved inventory data to runtime types.
		var slots [36]player.Slot
		var armor [4]player.Slot
		for i, s := range savedData.Inventory.Slots {
			slots[i] = player.Slot{BlockID: s.BlockID, ItemCount: s.ItemCount, ItemDamage: s.ItemDamage}
		}
		for i, s := range savedData.Inventory.Armor {
			armor[i] = player.Slot{BlockID: s.BlockID, ItemCount: s.ItemCount, ItemDamage: s.ItemDamage}
		}

		c.self.ApplyData(player.Position{
			X: posX, Y: posY, Z: posZ,
			Yaw: posYaw, Pitch: posPitch,
		}, gameMode, slots, armor, savedData.Inventory.HeldSlot)

		c.log.Info("restored saved player data")
	}

	// Set player position so chunk loading uses the correct coordinates.
	// For returning players ApplyData already did this, but for new players
	// the NewPlayer default (0.5, 4.0, 0.5) would be stale.
	c.self.SetPosition(posX, posY, posZ, posYaw, posPitch, true)

	// 1. Join Game
	if err := c.writePacket(&pkt.Login{
		EntityID:         entityID,
		GameMode:         gameMode,
		Dimension:        packet.DimensionOverworld,
		Difficulty:       packet.DifficultyEasy,
		MaxPlayers:       uint8(c.cfg.MaxPlayers),
		LevelType:        c.cfg.GeneratorType,
		ReducedDebugInfo: false,
	}); err != nil {
		return fmt.Errorf("write join game: %w", err)
	}

	// 2. Spawn Position
	if err := c.writePacket(&pkt.SpawnPosition{
		Location: mcnet.EncodePosition(0, spawnY, 0),
	}); err != nil {
		return fmt.Errorf("write spawn position: %w", err)
	}

	// 3. Player Abilities (based on actual game mode)
	abilities := abilitiesForGameMode(gameMode)
	if err := c.writePacket(&pkt.AbilitiesCB{
		Flags:        abilities,
		FlyingSpeed:  0.05,
		WalkingSpeed: 0.1,
	}); err != nil {
		return fmt.Errorf("write player abilities: %w", err)
	}

	// 4. Player Position And Look
	if err := c.writePacket(&pkt.PositionCB{
		X:     posX,
		Y:     posY,
		Z:     posZ,
		Yaw:   posYaw,
		Pitch: posPitch,
		Flags: 0x00, // all absolute
	}); err != nil {
		return fmt.Errorf("write position and look: %w", err)
	}

	// 5. Chunk Data (view distance radius around player position)
	if err := c.sendInitialChunks(); err != nil {
		return fmt.Errorf("send initial chunks: %w", err)
	}

	// 6. Update Time (send current world time)
	worldAge, worldTime := c.world.GetTime()
	if err := c.writePacket(&pkt.UpdateTime{
		Age:  worldAge,
		Time: worldTime,
	}); err != nil {
		return fmt.Errorf("write update time: %w", err)
	}

	// 7. Window Items (inventory sync)
	if err := c.sendWindowItems(); err != nil {
		return fmt.Errorf("send window items: %w", err)
	}

	// 8. Chat Message — "Hello, world!"
	if err := c.writePacket(&pkt.ChatCB{
		Message:  `{"text":"Hello, world!","color":"gold"}`,
		Position: 0,
	}); err != nil {
		return fmt.Errorf("write chat message: %w", err)
	}

	// 9. Register with player manager (sends cross-wise PlayerInfo + spawns).
	c.players.Add(c.self)

	// 10. Start KeepAlive goroutine
	go c.keepAliveLoop()

	c.log.Info("join sequence complete", "entityID", entityID)
	return nil
}

// abilitiesForGameMode returns the ability flags for a given game mode.
func abilitiesForGameMode(mode uint8) int8 {
	switch mode {
	case packet.GameModeCreative:
		return packet.AbilityInvulnerable | packet.AbilityAllowFlight | packet.AbilityCreativeMode
	case packet.GameModeSpectator:
		return packet.AbilityInvulnerable | packet.AbilityAllowFlight
	default:
		return 0
	}
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

	case 0x02: // Use Entity
		return c.handleUseEntity(data)

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

	case 0x0C: // Steer Vehicle — no vehicle support, ignore
		// consume and discard

	case 0x0D: // Close Window
		return c.handleCloseWindow(data)

	case 0x0E: // Window Click
		return c.handleWindowClick(data)

	case 0x0F: // Transaction
		return c.handleTransaction(data)

	case 0x10: // Set Creative Slot
		return c.handleCreativeSlot(data)

	case 0x11: // Enchant Item — no enchanting support, ignore
		// consume and discard

	case 0x12: // Update Sign
		var p pkt.UpdateSignSB
		if err := mcnet.Unmarshal(data, &p); err != nil {
			return fmt.Errorf("unmarshal update sign: %w", err)
		}
		x, y, z := mcnet.DecodePosition(p.Location)
		c.log.Info("update sign", "x", x, "y", y, "z", z,
			"line1", p.Text1, "line2", p.Text2, "line3", p.Text3, "line4", p.Text4)

	case 0x13: // Player Abilities (SB)
		var p pkt.AbilitiesSB
		if err := mcnet.Unmarshal(data, &p); err != nil {
			return fmt.Errorf("unmarshal abilities sb: %w", err)
		}
		c.handleAbilitiesUpdate(p)

	case 0x14: // Tab Complete
		return c.handleTabComplete(data)

	case 0x15: // Client Settings
		var p pkt.Settings
		if err := mcnet.Unmarshal(data, &p); err != nil {
			return fmt.Errorf("unmarshal client settings: %w", err)
		}
		c.log.Info("client settings", "locale", p.Locale, "viewDistance", p.ViewDistance)
		c.self.SetSkinParts(p.SkinParts)
		c.players.BroadcastEntityMetadata(c.self)

	case 0x16: // Client Status (respawn / stats request)
		return c.handleRespawn()

	case 0x17: // Custom Payload (plugin channel)
		return c.handleCustomPayload(data)

	case 0x18: // Spectate
		var p pkt.Spectate
		if err := mcnet.Unmarshal(data, &p); err != nil {
			return fmt.Errorf("unmarshal spectate: %w", err)
		}
		if c.self.GetGameMode() != packet.GameModeSpectator {
			break
		}
		targetUUID := formatUUID(p.Target)
		target := c.players.GetByUUID(targetUUID)
		if target != nil {
			pos := target.GetPosition()
			c.teleportSelf(pos.X, pos.Y, pos.Z)
		}

	case 0x19: // Resource Pack Status
		var p pkt.ResourcePackReceive
		if err := mcnet.Unmarshal(data, &p); err != nil {
			return fmt.Errorf("unmarshal resource pack status: %w", err)
		}
		c.log.Info("resource pack status", "hash", p.Hash, "result", p.Result)

	default:
		// ignore unknown packets silently
	}

	return nil
}

func (c *Connection) handlePositionUpdate(x, y, z float64, yaw, pitch float32, onGround bool, posChanged, lookChanged bool) {
	if c.self == nil {
		return
	}

	// Clamp to world boundary if configured.
	if c.cfg.WorldRadius > 0 {
		x, z = c.clampToWorldBounds(x, y, z, yaw, pitch)
	}

	// Preserve current look if only position changed.
	if !lookChanged {
		pos := c.self.GetPosition()
		yaw = pos.Yaw
		pitch = pos.Pitch
	}

	oldFX, oldFY, oldFZ, newFX, newFY, newFZ := c.setPositionAndUpdateChunks(x, y, z, yaw, pitch, onGround)

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

	// Try to pick up nearby item entities.
	if c.players.TryPickupItems(c.self) > 0 {
		_ = c.sendWindowItems()
	}
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

	switch status {
	case 0: // Started digging
		if c.self.GetGameMode() == packet.GameModeCreative {
			// Creative mode: instant break.
			c.breakBlock(x, y, z, posVal)
		} else {
			// Check if block is instant-break (hardness 0) in survival.
			stateID := c.world.GetBlock(x, y, z)
			if block, ok := c.lookupBlock(stateID); ok {
				heldItem := c.self.Inventory.HeldItem()
				var materials gamedata.MaterialRegistry
				if c.gameData != nil {
					materials = c.gameData.Materials
				}
				breakTicks := calcBreakTime(block, heldItem.BlockID, materials)
				if breakTicks == 0 {
					// Instant break even in survival (e.g. tall grass, torches).
					c.breakBlock(x, y, z, posVal)
					return nil
				}
				if breakTicks < 0 {
					// Unbreakable block, don't start animation.
					return nil
				}
			}
			// Broadcast dig start animation to other players.
			c.players.BroadcastToTrackers(&pkt.BlockBreakAnimation{
				EntityID:     c.self.EntityID,
				Location:     posVal,
				DestroyStage: 0,
			}, c.self.EntityID)
		}
		return nil

	case 1: // Cancelled digging
		// Reset block break animation for other players.
		c.players.BroadcastToTrackers(&pkt.BlockBreakAnimation{
			EntityID:     c.self.EntityID,
			Location:     posVal,
			DestroyStage: -1,
		}, c.self.EntityID)
		return nil

	case 2: // Finished digging
		// Validate that the block is actually diggable.
		stateID := c.world.GetBlock(x, y, z)
		if block, ok := c.lookupBlock(stateID); ok {
			if !block.Diggable || block.Hardness == nil {
				// Unbreakable — resend the block to the client.
				_ = c.writePacket(&pkt.BlockChange{Location: posVal, Type: stateID})
				return nil
			}
		}

		// Reset animation and break the block.
		c.players.BroadcastToTrackers(&pkt.BlockBreakAnimation{
			EntityID:     c.self.EntityID,
			Location:     posVal,
			DestroyStage: -1,
		}, c.self.EntityID)
		c.breakBlock(x, y, z, posVal)
		return nil
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
			c.players.SpawnItemEntity(c.self.EntityID, dropped, pos.X, pos.Y+1.3, pos.Z, pos.Yaw, c.playerGroundY(pos))
		}

		// Sync the held slot back to the client so the UI updates.
		protoSlot := int16(36) + heldSlot
		_ = c.sendSetSlot(0, protoSlot, c.self.Inventory.GetSlot(int(heldSlot)))

		// Update held item for trackers.
		newHeld := c.self.Inventory.HeldItem()
		eqData := player.BuildSingleEquipment(c.self.EntityID, 0, newHeld)
		c.players.BroadcastToTrackers(&pkt.EntityEquipment{Data: eqData}, c.self.EntityID)
	}

	return nil
}

// breakBlock removes a block from the world, broadcasts the change + break effect,
// and spawns item drops in survival mode.
func (c *Connection) breakBlock(x, y, z int, posVal int64) {
	oldBlockState := c.world.GetBlock(x, y, z)
	c.world.SetBlock(x, y, z, 0)
	blockChange := &pkt.BlockChange{
		Location: posVal,
		Type:     0,
	}
	c.players.BroadcastExcept(blockChange, c.self.EntityID)

	if oldBlockState != 0 {
		c.players.BroadcastToTrackers(&pkt.WorldEvent{
			EffectID: 2001,
			Location: posVal,
			Data:     oldBlockState,
			Global:   false,
		}, c.self.EntityID)
	}

	_ = c.writePacket(blockChange)

	// Spawn item drops in survival mode.
	if c.self.GetGameMode() != packet.GameModeCreative {
		if block, ok := c.lookupBlock(oldBlockState); ok {
			heldItem := c.self.Inventory.HeldItem()
			drops := blockDrops(block, heldItem.BlockID)
			for _, drop := range drops {
				groundY := c.findGroundLevel(x, y, z)
				c.players.SpawnBlockDrop(drop, float64(x)+0.5, float64(groundY)+0.1, float64(z)+0.5, float64(y)+0.5)
			}
		}
	}
}

// findGroundLevel scans downward from startY to find the first non-air block,
// returning the Y coordinate where an item would rest (top of that block).
// Capped at 64 blocks scan depth.
func (c *Connection) findGroundLevel(x, startY, z int) int {
	for y := startY - 1; y >= startY-64 && y >= 0; y-- {
		if c.world.GetBlock(x, y, z) != 0 {
			return y + 1
		}
	}
	return 0
}

// playerGroundY returns the ground level (as float64) below the player's current position.
func (c *Connection) playerGroundY(pos player.Position) float64 {
	return float64(c.findGroundLevel(int(math.Floor(pos.X)), int(pos.Y), int(math.Floor(pos.Z))))
}

// groundAtFunc returns a callback that finds the ground level at any (x, z) block position.
// The scan starts from the player's current Y level.
func (c *Connection) groundAtFunc() func(x, z int) float64 {
	startY := int(c.self.GetPosition().Y)
	return func(x, z int) float64 {
		return float64(c.findGroundLevel(x, startY+10, z))
	}
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

// setPositionAndUpdateChunks wraps SetPosition and triggers chunk loading if the player crossed a chunk boundary.
func (c *Connection) setPositionAndUpdateChunks(x, y, z float64, yaw, pitch float32, onGround bool) (oldFX, oldFY, oldFZ, newFX, newFY, newFZ int32) {
	oldCX, oldCZ := c.self.ChunkX(), c.self.ChunkZ()
	oldFX, oldFY, oldFZ, newFX, newFY, newFZ = c.self.SetPosition(x, y, z, yaw, pitch, onGround)
	newCX, newCZ := c.self.ChunkX(), c.self.ChunkZ()
	if oldCX != newCX || oldCZ != newCZ {
		c.updateLoadedChunks(newCX, newCZ)
	}
	return
}

// sendInitialChunks sends chunks around the player's current position and tracks them.
// Chunks are sorted closest-first so the player sees their surroundings immediately.
func (c *Connection) sendInitialChunks() error {
	centerCX, centerCZ := c.self.ChunkX(), c.self.ChunkZ()
	viewDist := c.cfg.ViewDistance

	// Collect all chunk positions in range.
	var chunks []gen.ChunkPos
	for cx := centerCX - viewDist; cx <= centerCX+viewDist; cx++ {
		for cz := centerCZ - viewDist; cz <= centerCZ+viewDist; cz++ {
			if !c.isChunkInBounds(cx, cz) {
				continue
			}
			chunks = append(chunks, gen.ChunkPos{X: cx, Z: cz})
		}
	}

	// Sort by squared distance from center (closest first).
	sort.Slice(chunks, func(i, j int) bool {
		di := (chunks[i].X-centerCX)*(chunks[i].X-centerCX) + (chunks[i].Z-centerCZ)*(chunks[i].Z-centerCZ)
		dj := (chunks[j].X-centerCX)*(chunks[j].X-centerCX) + (chunks[j].Z-centerCZ)*(chunks[j].Z-centerCZ)
		return di < dj
	})

	for _, pos := range chunks {
		chunk := c.world.EncodeChunk(pos.X, pos.Z)
		if err := c.writePacket(&chunk); err != nil {
			return err
		}
		c.loadedChunks[pos] = struct{}{}
	}
	return nil
}

// updateLoadedChunks sends new chunks and unloads old ones when the player crosses a chunk boundary.
func (c *Connection) updateLoadedChunks(newCX, newCZ int) {
	viewDist := c.cfg.ViewDistance

	// Load new chunks in the view square.
	for cx := newCX - viewDist; cx <= newCX+viewDist; cx++ {
		for cz := newCZ - viewDist; cz <= newCZ+viewDist; cz++ {
			pos := gen.ChunkPos{X: cx, Z: cz}
			if _, loaded := c.loadedChunks[pos]; loaded {
				continue
			}
			if !c.isChunkInBounds(cx, cz) {
				continue
			}
			chunk := c.world.EncodeChunk(cx, cz)
			if err := c.writePacket(&chunk); err != nil {
				c.log.Error("send chunk", "cx", cx, "cz", cz, "error", err)
				return
			}
			c.loadedChunks[pos] = struct{}{}
		}
	}

	// Unload chunks outside view distance.
	for pos := range c.loadedChunks {
		if player.InViewDistance(pos.X, pos.Z, newCX, newCZ, viewDist) {
			continue
		}
		// MC 1.8: send MapChunk with GroundUp=true, BitMap=0, empty data to unload.
		if err := c.writePacket(&pkt.MapChunk{
			X:         int32(pos.X),
			Z:         int32(pos.Z),
			GroundUp:  true,
			BitMap:    0,
			ChunkData: []byte{},
		}); err != nil {
			c.log.Error("unload chunk", "cx", pos.X, "cz", pos.Z, "error", err)
		}
		delete(c.loadedChunks, pos)
	}
}

// clampToWorldBounds clamps player position to world boundary.
// Returns (possibly clamped) x and z. Sends a position correction if clamped.
func (c *Connection) clampToWorldBounds(x, y, z float64, yaw, pitch float32) (float64, float64) {
	r := c.cfg.WorldRadius
	minBlock := float64(-r * 16)
	maxBlock := float64(r*16 + 16)

	clampedX, clampedZ := x, z
	if clampedX < minBlock {
		clampedX = minBlock
	} else if clampedX >= maxBlock {
		clampedX = maxBlock - 0.01
	}
	if clampedZ < minBlock {
		clampedZ = minBlock
	} else if clampedZ >= maxBlock {
		clampedZ = maxBlock - 0.01
	}

	if clampedX != x || clampedZ != z {
		_ = c.writePacket(&pkt.PositionCB{
			X:     clampedX,
			Y:     y,
			Z:     clampedZ,
			Yaw:   yaw,
			Pitch: pitch,
			Flags: 0x00,
		})
	}

	return clampedX, clampedZ
}

// isChunkInBounds returns whether a chunk is within the world boundary.
func (c *Connection) isChunkInBounds(cx, cz int) bool {
	r := c.cfg.WorldRadius
	if r <= 0 {
		return true
	}
	return cx >= -r && cx <= r && cz >= -r && cz <= r
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

// handleUseEntity processes a UseEntity (0x02) packet.
// Uses mc:"rest" encoding, so we parse manually.
func (c *Connection) handleUseEntity(data []byte) error {
	r := bytes.NewReader(data)

	targetID, _, err := mcnet.ReadVarInt(r)
	if err != nil {
		return fmt.Errorf("read use entity target: %w", err)
	}

	mouse, _, err := mcnet.ReadVarInt(r)
	if err != nil {
		return fmt.Errorf("read use entity mouse: %w", err)
	}

	// mouse=2 (interact at) has 3 extra floats for the hit position.
	if mouse == 2 {
		if _, err := mcnet.ReadF32(r); err != nil {
			return fmt.Errorf("read use entity target x: %w", err)
		}
		if _, err := mcnet.ReadF32(r); err != nil {
			return fmt.Errorf("read use entity target y: %w", err)
		}
		if _, err := mcnet.ReadF32(r); err != nil {
			return fmt.Errorf("read use entity target z: %w", err)
		}
	}

	// mouse=1 is attack.
	if mouse != 1 {
		return nil
	}

	target := c.players.GetByEntityID(targetID)
	if target == nil {
		return nil
	}

	// Broadcast hurt animation to all trackers of the target.
	c.players.BroadcastToTrackers(&pkt.EntityStatus{
		EntityID:     targetID,
		EntityStatus: 2, // hurt animation
	}, targetID)
	// Also send to the target itself.
	_ = target.WritePacket(&pkt.EntityStatus{
		EntityID:     targetID,
		EntityStatus: 2,
	})

	// Compute knockback direction from attacker to target.
	attackerPos := c.self.GetPosition()
	targetPos := target.GetPosition()
	dx := targetPos.X - attackerPos.X
	dz := targetPos.Z - attackerPos.Z
	dist := math.Sqrt(dx*dx + dz*dz)
	if dist > 0 {
		dx /= dist
		dz /= dist
	}

	// Send velocity packet (protocol units: 1/8000 blocks/tick).
	// Broadcast to all trackers so the attacker sees the knockback too.
	vx := int16(dx * 0.4 * 8000)
	vy := int16(0.36 * 8000)
	vz := int16(dz * 0.4 * 8000)
	velPkt := &pkt.EntityVelocity{
		EntityID:  targetID,
		VelocityX: vx,
		VelocityY: vy,
		VelocityZ: vz,
	}
	_ = target.WritePacket(velPkt)
	c.players.BroadcastToTrackers(velPkt, targetID)

	return nil
}

// handleAbilitiesUpdate processes a PlayerAbilities (0x13) server-bound packet.
func (c *Connection) handleAbilitiesUpdate(p pkt.AbilitiesSB) {
	wantsFlying := p.Flags&int8(packet.AbilityFlying) != 0
	mode := c.self.GetGameMode()

	// Only creative and spectator may fly.
	if wantsFlying && mode != packet.GameModeCreative && mode != packet.GameModeSpectator {
		// Send corrective abilities back.
		_ = c.writePacket(&pkt.AbilitiesCB{
			Flags:        abilitiesForGameMode(mode),
			FlyingSpeed:  0.05,
			WalkingSpeed: 0.1,
		})
		return
	}

	c.self.SetFlying(wantsFlying)
}

// handleRespawn processes a ClientStatus (0x16) packet.
// ActionID 0 = perform respawn, ActionID 1 = request stats.
func (c *Connection) handleRespawn() error {
	if !c.dead {
		return nil
	}
	c.dead = false

	// Send Respawn packet.
	if err := c.writePacket(&pkt.Respawn{
		Dimension:  int32(packet.DimensionOverworld),
		Difficulty: packet.DifficultyEasy,
		Gamemode:   c.self.GetGameMode(),
		LevelType:  c.cfg.GeneratorType,
	}); err != nil {
		return fmt.Errorf("write respawn: %w", err)
	}

	// Reset position to spawn.
	spawnY := c.world.SpawnHeight()
	c.self.SetPosition(0.5, float64(spawnY), 0.5, 0, 0, true)

	// Clear and resend chunks.
	c.loadedChunks = make(map[gen.ChunkPos]struct{})
	if err := c.sendInitialChunks(); err != nil {
		return fmt.Errorf("respawn send chunks: %w", err)
	}

	// Send position.
	if err := c.writePacket(&pkt.PositionCB{
		X:     0.5,
		Y:     float64(spawnY),
		Z:     0.5,
		Yaw:   0,
		Pitch: 0,
		Flags: 0x00,
	}); err != nil {
		return fmt.Errorf("write respawn position: %w", err)
	}

	// Restore health.
	_ = c.writePacket(&pkt.UpdateHealth{
		Health:         20,
		Food:           20,
		FoodSaturation: 5,
	})

	// Send abilities.
	_ = c.writePacket(&pkt.AbilitiesCB{
		Flags:        abilitiesForGameMode(c.self.GetGameMode()),
		FlyingSpeed:  0.05,
		WalkingSpeed: 0.1,
	})

	// Resync inventory.
	_ = c.sendWindowItems()

	// Update tracking.
	c.players.UpdateTracking(c.self)

	return nil
}

// handleCustomPayload processes a CustomPayload (0x17) plugin channel packet.
func (c *Connection) handleCustomPayload(data []byte) error {
	var p pkt.CustomPayloadSB
	if err := mcnet.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("unmarshal custom payload: %w", err)
	}

	switch p.Channel {
	case "MC|Brand":
		c.log.Info("client brand", "brand", string(p.Data))
		_ = c.writePacket(&pkt.CustomPayloadCB{
			Channel: "MC|Brand",
			Data:    []byte("GoTheftCraft"),
		})
	default:
		c.log.Debug("plugin channel", "channel", p.Channel, "size", len(p.Data))
	}

	return nil
}
