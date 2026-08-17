package conn

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	gamedata "github.com/go-theft-craft/minecraft-protocol/data"
	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/server/internal/server/packet"
	"github.com/go-theft-craft/server/internal/server/player"
	"github.com/go-theft-craft/server/internal/server/storage"
	"github.com/go-theft-craft/server/pkg/world"
)

func (c *Connection) startPlay(username, uuid string, skinProps []player.SkinProperty) error {
	c.log = c.log.With("player", username)

	uuidBytes := parseUUID(uuid)
	entityID := c.players.AllocateEntityID()
	c.self = player.NewPlayer(entityID, uuid, uuidBytes, username, skinProps, c.writePlayerPacket)

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

	// A joining player is whole. Health is session state and is not saved, so
	// this is the only place a new session's bar is decided.
	c.resetHealth()

	// 1. Join Game
	if err := c.send(&v1_8.PlayClientboundLogin{
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
	if err := c.send(&v1_8.PlayClientboundSpawnPosition{
		Location: blockPos(0, spawnY, 0),
	}); err != nil {
		return fmt.Errorf("write spawn position: %w", err)
	}

	// 3. Player Abilities (based on actual game mode)
	abilities := abilitiesForGameMode(gameMode)
	if err := c.send(&v1_8.PlayClientboundAbilities{
		Flags:        abilities,
		FlyingSpeed:  0.05,
		WalkingSpeed: 0.1,
	}); err != nil {
		return fmt.Errorf("write player abilities: %w", err)
	}

	// 4. Player Position And Look
	if err := c.send(&v1_8.PlayClientboundPosition{
		X:     posX,
		Y:     posY,
		Z:     posZ,
		Yaw:   posYaw,
		Pitch: posPitch,
		Flags: packet.PositionAbsolute,
	}); err != nil {
		return fmt.Errorf("write position and look: %w", err)
	}

	// 5. Chunk Data (view distance radius around player position)
	if err := c.sendInitialChunks(); err != nil {
		return fmt.Errorf("send initial chunks: %w", err)
	}

	// 6. Update Time (send current world time)
	worldAge, worldTime := c.world.GetTime()
	if err := c.send(&v1_8.PlayClientboundUpdateTime{
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
	if err := c.send(&v1_8.PlayClientboundChat{
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

// blockPos builds a generated protocol 47 block Position. Its packed encoding
// is byte-identical to java.EncodePosition written as an int64.
func blockPos(x, y, z int) v1_8.Position {
	return v1_8.Position{X: int32(x), Y: int16(y), Z: int32(z)}
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
					_ = c.send(&v1_8.PlayClientboundKickDisconnect{
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

			if err := c.send(&v1_8.PlayClientboundKeepAlive{
				KeepAliveID: id,
			}); err != nil {
				c.log.Error("keep alive write failed", "error", err)
				c.cancel()
				return
			}
		}
	}
}

// handlePlay dispatches on the value the generated session already decoded, so
// no play packet is decoded a second time here. A packet whose ID is not
// serverbound-play in protocol 47 arrives as a protocol.UnknownPacket and falls
// through to the default, exactly as an unmatched ID did before.
func (c *Connection) handlePlay(inbound protocol.Packet) error {
	switch value := inbound.Value.(type) {
	case *v1_8.PlayServerboundKeepAlive:
		c.mu.Lock()
		if value.KeepAliveID == c.lastKeepAliveID {
			c.keepAliveAcked = true
		}
		c.mu.Unlock()

	case *v1_8.PlayServerboundChat:
		c.log.Info("chat", "message", value.Message)
		if c.handleCommand(value.Message) {
			break
		}
		chatJSON := fmt.Sprintf(
			`{"translate":"chat.type.text","with":[%s,%s]}`,
			escapeJSON(c.self.Username), escapeJSON(value.Message),
		)
		c.players.Broadcast(&v1_8.PlayClientboundChat{
			Message:  chatJSON,
			Position: 0,
		})

	case *v1_8.PlayServerboundUseEntity:
		return c.handleUseEntity(value)

	case *v1_8.PlayServerboundFlying: // Player (ground state)
		// The heartbeat carries no position, but it is the only packet a
		// player who stands still sends — and standing still inside a cactus
		// has to keep hurting. The last known position is what to test.
		if c.self != nil {
			pos := c.self.GetPosition()
			c.checkContactDamage(pos.X, pos.Y, pos.Z)
		}

	case *v1_8.PlayServerboundPosition:
		c.handlePositionUpdate(value.X, value.Y, value.Z, 0, 0, value.OnGround, true, false)

	case *v1_8.PlayServerboundLook:
		c.handleLookUpdate(value.Yaw, value.Pitch, value.OnGround)

	case *v1_8.PlayServerboundPositionLook:
		c.handlePositionUpdate(value.X, value.Y, value.Z, value.Yaw, value.Pitch, value.OnGround, true, true)

	case *v1_8.PlayServerboundBlockDig:
		return c.handleBlockDig(value)

	case *v1_8.PlayServerboundBlockPlace:
		return c.handleBlockPlace(value)

	case *v1_8.PlayServerboundHeldItemSlot:
		if value.SlotID < 0 || value.SlotID > 8 {
			return nil
		}
		c.self.Inventory.SetHeldSlot(value.SlotID)
		heldItem := c.self.Inventory.HeldItem()
		c.broadcastSingleEquipment(c.self.EntityID, 0, heldItem)

	case *v1_8.PlayServerboundArmAnimation:
		c.players.BroadcastToTrackers(&v1_8.PlayClientboundAnimation{
			EntityID:  c.self.EntityID,
			Animation: 0, // swing arm
		}, c.self.EntityID)

	case *v1_8.PlayServerboundEntityAction:
		switch value.ActionID {
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

	case *v1_8.PlayServerboundSteerVehicle: // no vehicle support, ignore

	case *v1_8.PlayServerboundCloseWindow:
		return c.handleCloseWindow()

	case *v1_8.PlayServerboundWindowClick:
		return c.handleWindowClick(value)

	case *v1_8.PlayServerboundTransaction:
		return c.handleTransaction()

	case *v1_8.PlayServerboundSetCreativeSlot:
		return c.handleCreativeSlot(value)

	case *v1_8.PlayServerboundEnchantItem: // no enchanting support, ignore

	case *v1_8.PlayServerboundUpdateSign:
		c.log.Info("update sign",
			"x", int(value.Location.X), "y", int(value.Location.Y), "z", int(value.Location.Z),
			"line1", value.Text1, "line2", value.Text2, "line3", value.Text3, "line4", value.Text4)

	case *v1_8.PlayServerboundAbilities:
		c.handleAbilitiesUpdate(value)

	case *v1_8.PlayServerboundTabComplete:
		return c.handleTabComplete(value)

	case *v1_8.PlayServerboundSettings:
		c.log.Info("client settings", "locale", value.Locale, "viewDistance", value.ViewDistance)
		c.self.SetSkinParts(value.SkinParts)
		c.players.BroadcastEntityMetadata(c.self)

	case *v1_8.PlayServerboundClientCommand: // Client Status (respawn / stats request)
		return c.handleRespawn()

	case *v1_8.PlayServerboundCustomPayload:
		return c.handleCustomPayload(value)

	case *v1_8.PlayServerboundSpectate:
		if c.self.GetGameMode() != packet.GameModeSpectator {
			break
		}
		target := c.players.GetByUUID(value.Target.String())
		if target != nil {
			pos := target.GetPosition()
			c.teleportSelf(pos.X, pos.Y, pos.Z)
		}

	case *v1_8.PlayServerboundResourcePackReceive:
		c.log.Info("resource pack status", "hash", value.Hash, "result", value.Result)

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

	c.checkContactDamage(x, y, z)

	dx := newFX - oldFX
	dy := newFY - oldFY
	dz := newFZ - oldFZ

	yawAngle := player.DegreesToAngle(yaw)
	pitchAngle := player.DegreesToAngle(pitch)
	eid := c.self.EntityID

	// A move is sent relatively when the delta fits in a byte and absolutely
	// when it does not, which is what keeps a teleport from being reported as
	// a walk.
	fitsRelative := player.DeltaFitsInByte(dx, dy, dz)

	switch {
	case posChanged && lookChanged && fitsRelative:
		c.players.BroadcastToTrackers(&v1_8.PlayClientboundEntityMoveLook{
			EntityID: eid,
			DX:       int8(dx),
			DY:       int8(dy),
			DZ:       int8(dz),
			Yaw:      yawAngle,
			Pitch:    pitchAngle,
			OnGround: onGround,
		}, eid)

	case posChanged && !lookChanged && fitsRelative:
		c.players.BroadcastToTrackers(&v1_8.PlayClientboundRelEntityMove{
			EntityID: eid,
			DX:       int8(dx),
			DY:       int8(dy),
			DZ:       int8(dz),
			OnGround: onGround,
		}, eid)

	case posChanged:
		c.players.BroadcastToTrackers(&v1_8.PlayClientboundEntityTeleport{
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
		c.players.BroadcastToTrackers(&v1_8.PlayClientboundEntityHeadRotation{
			EntityID: eid,
			HeadYaw:  yawAngle,
		}, eid)
	}

	// Sprint particles: send block crack particles at player's feet.
	if posChanged && c.self.IsSprinting() {
		blockBelow := c.world.GetBlockID(int(math.Floor(x)), int(math.Floor(y))-1, int(math.Floor(z)))
		if blockBelow != 0 {
			particles := sprintParticles(x, y, z, blockBelow)
			c.players.BroadcastToTrackers(&particles, eid)
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

	c.players.BroadcastToTrackers(&v1_8.PlayClientboundEntityLook{
		EntityID: eid,
		Yaw:      yawAngle,
		Pitch:    pitchAngle,
		OnGround: onGround,
	}, eid)

	c.players.BroadcastToTrackers(&v1_8.PlayClientboundEntityHeadRotation{
		EntityID: eid,
		HeadYaw:  yawAngle,
	}, eid)
}

func (c *Connection) handleBlockDig(value *v1_8.PlayServerboundBlockDig) error {
	status := value.Status
	x, y, z := int(value.Location.X), int(value.Location.Y), int(value.Location.Z)

	switch status {
	case 0: // Started digging
		if c.self.GetGameMode() == packet.GameModeCreative {
			// Creative mode: instant break.
			c.breakBlock(x, y, z)
		} else {
			// Check if block is instant-break (hardness 0) in survival.
			stateID := c.world.GetBlockID(x, y, z)
			if block, ok := c.lookupBlock(stateID); ok {
				heldItem := c.self.Inventory.HeldItem()
				var materials gamedata.MaterialRegistry
				if c.gameData != nil {
					materials = c.gameData.Materials()
				}
				breakTicks := calcBreakTime(block, heldItem.BlockID, materials)
				if breakTicks == 0 {
					// Instant break even in survival (e.g. tall grass, torches).
					c.breakBlock(x, y, z)
					return nil
				}
				if breakTicks < 0 {
					// Unbreakable block, don't start animation.
					return nil
				}
			}
			// Broadcast dig start animation to other players.
			c.players.BroadcastToTrackers(&v1_8.PlayClientboundBlockBreakAnimation{
				EntityID:     c.self.EntityID,
				Location:     blockPos(x, y, z),
				DestroyStage: 0,
			}, c.self.EntityID)
		}
		return nil

	case 1: // Cancelled digging
		// Reset block break animation for other players.
		c.players.BroadcastToTrackers(&v1_8.PlayClientboundBlockBreakAnimation{
			EntityID:     c.self.EntityID,
			Location:     blockPos(x, y, z),
			DestroyStage: -1,
		}, c.self.EntityID)
		return nil

	case 2: // Finished digging
		// Validate that the block is actually diggable.
		stateID := c.world.GetBlockID(x, y, z)
		if block, ok := c.lookupBlock(stateID); ok {
			if !block.Diggable || block.Hardness == nil {
				// Unbreakable — resend the block to the client.
				_ = c.send(&v1_8.PlayClientboundBlockChange{Location: blockPos(x, y, z), Type: stateID})
				return nil
			}
		}

		// Reset animation and break the block.
		c.players.BroadcastToTrackers(&v1_8.PlayClientboundBlockBreakAnimation{
			EntityID:     c.self.EntityID,
			Location:     blockPos(x, y, z),
			DestroyStage: -1,
		}, c.self.EntityID)
		c.breakBlock(x, y, z)
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
			c.players.SpawnItemEntity(c.self.EntityID, dropped, pos.X, pos.Y+1.3, pos.Z, pos.Yaw, c.groundAtFunc())
		}

		// Sync the held slot back to the client so the UI updates.
		protoSlot := int16(slotHotbarStart) + heldSlot
		_ = c.sendSetSlot(0, protoSlot, c.self.Inventory.GetSlot(int(heldSlot)))

		// Update held item for trackers.
		newHeld := c.self.Inventory.HeldItem()
		c.broadcastSingleEquipment(c.self.EntityID, 0, newHeld)
	}

	return nil
}

// breakBlock removes a block from the world, broadcasts the change + break effect,
// and spawns item drops in survival mode.
func (c *Connection) breakBlock(x, y, z int) {
	oldBlockState := c.world.GetBlockID(x, y, z)

	// A broken container spills what it held, in every game mode: creative
	// suppresses the block's own drop, not the contents someone put inside.
	if isChestBlock(oldBlockState >> 4) {
		c.spillChest(x, y, z)
		// The survivor of a broken pair is no longer half of anything, so it
		// re-orients once the block is gone. Deferred for that reason.
		defer c.refreshChestCluster(oldBlockState>>4, world.BlockPos{X: x, Y: y, Z: z})
	}

	c.world.SetBlockID(x, y, z, 0)
	blockChange := &v1_8.PlayClientboundBlockChange{
		Location: blockPos(x, y, z),
		Type:     0,
	}
	c.players.BroadcastExcept(blockChange, c.self.EntityID)

	if oldBlockState != 0 {
		c.players.BroadcastToTrackers(&v1_8.PlayClientboundWorldEvent{
			EffectID: 2001,
			Location: blockPos(x, y, z),
			Data:     oldBlockState,
			Global:   false,
		}, c.self.EntityID)
	}

	_ = c.send(blockChange)

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
func (c *Connection) findGroundLevel(x, startY, z int) int {
	maxY := c.cfg.MaxBuildHeight
	if startY > maxY {
		startY = maxY
	}
	for y := startY - 1; y >= 0; y-- {
		if c.world.GetBlockID(x, y, z) != 0 {
			return y + 1
		}
	}
	return 0
}

// groundAtFunc returns a callback that finds the ground level below a given (x, y, z)
// block position. Scans downward from the item's current Y so it correctly finds
// cave floors instead of landing on ceilings or terrain far above the player.
func (c *Connection) groundAtFunc() func(x, y, z int) float64 {
	return func(x, y, z int) float64 {
		return float64(c.findGroundLevel(x, y, z))
	}
}

func (c *Connection) handleBlockPlace(value *v1_8.PlayServerboundBlockPlace) error {
	face := value.Direction
	heldBlockID := value.HeldItem.BlockID

	// Special position -1,-1,-1 means the player is using an item (not placing a block).
	if value.Location.X == -1 && value.Location.Y == -1 && value.Location.Z == -1 {
		// Try to equip armor from hotbar via right-click.
		if armorProtoSlot := armorSlotForItem(heldBlockID); armorProtoSlot >= 0 {
			heldIdx := int16(slotHotbarStart) + int16(c.self.Inventory.GetHeldSlot())
			heldItem := c.getWindowSlot(heldIdx)
			armorItem := c.getWindowSlot(armorProtoSlot)
			c.setWindowSlot(armorProtoSlot, heldItem)
			c.setWindowSlot(heldIdx, armorItem)
			_ = c.sendWindowItems()
		}
		return nil
	}

	clickedX, clickedY, clickedZ := int(value.Location.X), int(value.Location.Y), int(value.Location.Z)

	// Right-clicking a block that has a use takes priority over placing, unless
	// the player is sneaking — which is how vanilla lets you build against a
	// crafting table instead of opening it.
	if !c.self.IsSneaking() {
		switch c.world.GetBlockID(clickedX, clickedY, clickedZ) >> 4 {
		case craftingTableBlockID:
			return c.openCraftingTable()
		case chestBlockID, trappedChestBlockID:
			return c.openChest(clickedX, clickedY, clickedZ)
		}
	}

	// Empty slot means no block to place.
	if heldBlockID <= 0 {
		return nil
	}

	x, y, z := clickedX, clickedY, clickedZ

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

	// What gets placed is what the server has in hand, not what the packet
	// claims: the packet's item is the client's view, and trusting it lets a
	// client build with blocks it does not hold.
	heldSlot := c.self.Inventory.GetHeldSlot()
	held := c.self.Inventory.GetSlot(int(heldSlot))
	if held.IsEmpty() || held.BlockID <= 0 || !c.isPlaceable(held.BlockID) {
		return c.revertPlacement(x, y, z)
	}

	// Some blocks refuse positions that are legal for everything else. A chest
	// is one: it may join a lone chest but never a pair, so three never meet.
	if isChestBlock(int32(held.BlockID)) &&
		!c.canPlaceChestAt(int32(held.BlockID), world.BlockPos{X: x, Y: y, Z: z}) {
		return c.revertPlacement(x, y, z)
	}

	stateID := c.placementState(held)
	c.world.SetBlockID(x, y, z, stateID)

	blockChange := &v1_8.PlayClientboundBlockChange{
		Location: blockPos(x, y, z),
		Type:     stateID,
	}
	c.players.BroadcastExcept(blockChange, c.self.EntityID)
	if err := c.send(blockChange); err != nil {
		return err
	}

	// A new chest re-orients itself and its partner, so a pair never ends up
	// with two halves facing different ways.
	if isChestBlock(int32(held.BlockID)) {
		c.refreshChestCluster(int32(held.BlockID), world.BlockPos{X: x, Y: y, Z: z})
	}

	// Survival pays for the block. The client already decremented its own copy
	// of the stack when it predicted the placement, so a server that never
	// consumed anything drifted one item ahead of the client on every place,
	// and the next inventory sync handed that item back.
	if c.self.GetGameMode() == packet.GameModeCreative {
		return nil
	}

	c.self.Inventory.RemoveOne(int(heldSlot))
	remaining := c.self.Inventory.GetSlot(int(heldSlot))
	if err := c.sendSetSlot(0, int16(slotHotbarStart)+heldSlot, remaining); err != nil {
		return err
	}
	c.broadcastSingleEquipment(c.self.EntityID, 0, remaining)

	return nil
}

// maxBlockID is the last ID protocol 47 gives a block; everything above it is
// an item.
const maxBlockID = 255

// placementState is the block state a held item becomes when it is placed.
//
// Most blocks take their metadata straight from the item's damage value, which
// is what carries a variant like a wood type. A block with a facing does not:
// its metadata says which way it points, and only some values are states the
// block actually has. A 1.8 client resolves a chunk section value against the
// registry of valid states and draws air when it finds none, so a state
// invented here does not merely look wrong — the block is not there at all.
func (c *Connection) placementState(held player.Slot) int32 {
	meta := int32(held.ItemDamage) & 0xF

	if isChestBlock(int32(held.BlockID)) {
		meta = chestFacing(c.self.GetPosition().Yaw)
	}

	return int32(held.BlockID)<<4 | meta
}

// isPlaceable reports whether an item ID names a block. Items — a sword, a
// bucket — are not placed by a right-click on a block face, and the state ID
// built from one would be a block the world has no entry for. With no game
// data loaded there is nothing to check against, so the caller is trusted.
func (c *Connection) isPlaceable(itemID int16) bool {
	// Protocol 47 numbers blocks 0 through 255 and items above them, so an ID
	// past the block range names an item whatever the registry says. The
	// world already holds an apple, some seeds and a diamond pickaxe stored as
	// blocks, which a client draws as air because no such block state exists.
	if itemID <= 0 || itemID > maxBlockID {
		return false
	}

	if c.gameData == nil || c.gameData.Blocks() == nil {
		return true
	}

	_, ok := c.lookupBlock(int32(itemID) << 4)

	return ok
}

// revertPlacement tells the client what is really at the position it just
// predicted a block into, so a refused placement does not leave a ghost block
// on screen until the chunk is reloaded.
func (c *Connection) revertPlacement(x, y, z int) error {
	if err := c.send(&v1_8.PlayClientboundBlockChange{
		Location: blockPos(x, y, z),
		Type:     c.world.GetBlockID(x, y, z),
	}); err != nil {
		return err
	}

	// The client decremented its own stack when it predicted the placement, so
	// a refusal has to hand the item back. Without this the player watches the
	// block reappear and the item stay gone until the next inventory sync.
	heldSlot := int16(c.self.Inventory.GetHeldSlot())

	return c.sendSetSlot(0, slotHotbarStart+heldSlot, c.self.Inventory.GetSlot(int(heldSlot)))
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
	var chunks []world.ChunkPos
	for cx := centerCX - viewDist; cx <= centerCX+viewDist; cx++ {
		for cz := centerCZ - viewDist; cz <= centerCZ+viewDist; cz++ {
			if !c.isChunkInBounds(cx, cz) {
				continue
			}
			chunks = append(chunks, world.ChunkPos{X: cx, Z: cz})
		}
	}

	// Sort by squared distance from center (closest first).
	sort.Slice(chunks, func(i, j int) bool {
		di := (chunks[i].X-centerCX)*(chunks[i].X-centerCX) + (chunks[i].Z-centerCZ)*(chunks[i].Z-centerCZ)
		dj := (chunks[j].X-centerCX)*(chunks[j].X-centerCX) + (chunks[j].Z-centerCZ)*(chunks[j].Z-centerCZ)
		return di < dj
	})

	for _, pos := range chunks {
		chunk, err := c.world.Adapter().EncodeChunk(c.world.Chunk(pos))
		if err != nil {
			return err
		}
		if err := c.send(chunk); err != nil {
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
			pos := world.ChunkPos{X: cx, Z: cz}
			if _, loaded := c.loadedChunks[pos]; loaded {
				continue
			}
			if !c.isChunkInBounds(cx, cz) {
				continue
			}
			chunk, err := c.world.Adapter().EncodeChunk(c.world.Chunk(pos))
			if err != nil {
				c.log.Error("encode chunk", "cx", cx, "cz", cz, "error", err)

				return
			}
			if err := c.send(chunk); err != nil {
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
		unload, err := c.world.Adapter().EncodeUnload(pos)
		if err != nil {
			c.log.Error("encode unload", "cx", pos.X, "cz", pos.Z, "error", err)

			continue
		}
		if err := c.send(unload); err != nil {
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
		_ = c.send(&v1_8.PlayClientboundPosition{
			X:     clampedX,
			Y:     y,
			Z:     clampedZ,
			Yaw:   yaw,
			Pitch: pitch,
			Flags: packet.PositionAbsolute,
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

// sprintParticles builds the WorldParticles (0x2A) block-crack effect at the
// player's feet. Particle ID 37 = block crack, carrying the block state as its
// single VarInt data element. The field order and widths match the raw builder
// this replaced byte for byte.
func sprintParticles(x, y, z float64, blockState int32) v1_8.PlayClientboundWorldParticles {
	return v1_8.PlayClientboundWorldParticles{
		ParticleID:   37,
		LongDistance: false,
		X:            float32(x),
		Y:            float32(y),
		Z:            float32(z),
		OffsetX:      0.5,
		OffsetY:      0.1,
		OffsetZ:      0.5,
		ParticleData: 0.0,
		Particles:    5,
		Data:         v1_8.PlayClientboundWorldParticlesDataSwitch{Case37: [1]int32{blockState}},
	}
}

// handleUseEntity processes a UseEntity (0x02) packet. The generated model
// carries the interact-at hit position (mouse==2) as switch fields the session
// already consumed; only the target and mouse are needed here.
func (c *Connection) handleUseEntity(value *v1_8.PlayServerboundUseEntity) error {
	targetID := value.Target
	mouse := value.Mouse

	// mouse=1 is attack.
	if mouse != 1 {
		return nil
	}

	target := c.players.GetByEntityID(targetID)
	if target == nil {
		return nil
	}

	// Broadcast hurt animation to all trackers of the target.
	c.players.BroadcastToTrackers(&v1_8.PlayClientboundEntityStatus{
		EntityID:     targetID,
		EntityStatus: 2, // hurt animation
	}, targetID)
	// Also send to the target itself.
	_ = target.WritePacket(&v1_8.PlayClientboundEntityStatus{
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
	velPkt := &v1_8.PlayClientboundEntityVelocity{
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
func (c *Connection) handleAbilitiesUpdate(value *v1_8.PlayServerboundAbilities) {
	wantsFlying := value.Flags&int8(packet.AbilityFlying) != 0
	mode := c.self.GetGameMode()

	// Only creative and spectator may fly.
	if wantsFlying && mode != packet.GameModeCreative && mode != packet.GameModeSpectator {
		// Send corrective abilities back.
		_ = c.send(&v1_8.PlayClientboundAbilities{
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
	if err := c.send(&v1_8.PlayClientboundRespawn{
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
	c.loadedChunks = make(map[world.ChunkPos]struct{})
	if err := c.sendInitialChunks(); err != nil {
		return fmt.Errorf("respawn send chunks: %w", err)
	}

	// Send position.
	if err := c.send(&v1_8.PlayClientboundPosition{
		X:     0.5,
		Y:     float64(spawnY),
		Z:     0.5,
		Yaw:   0,
		Pitch: 0,
		Flags: packet.PositionAbsolute,
	}); err != nil {
		return fmt.Errorf("write respawn position: %w", err)
	}

	// Restore health.
	c.resetHealth()
	_ = c.send(&v1_8.PlayClientboundUpdateHealth{
		Health:         c.health,
		Food:           maxFood,
		FoodSaturation: 5,
	})

	// Send abilities.
	_ = c.send(&v1_8.PlayClientboundAbilities{
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
func (c *Connection) handleCustomPayload(value *v1_8.PlayServerboundCustomPayload) error {
	switch value.Channel {
	case "MC|Brand":
		c.log.Info("client brand", "brand", string(value.Data))
		_ = c.send(&v1_8.PlayClientboundCustomPayload{
			Channel: "MC|Brand",
			Data:    []byte("GoTheftCraft"),
		})
	default:
		c.log.Debug("plugin channel", "channel", value.Channel, "size", len(value.Data))
	}

	return nil
}
