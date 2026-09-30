package conn

import (
	protocol "github.com/go-theft-craft/minecraft-protocol"
	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/server/internal/server/player"
	"github.com/go-theft-craft/server/pkg/world"
)

// v47Dialect is the play path in protocol 47.
//
// Every write method is the packet literal that used to sit in a handler,
// moved rather than rewritten, and the byte-parity fixtures are what say no
// byte moved with it. The entity half is player.V47Packets, embedded rather
// than repeated, because the player manager builds those packets too.
type v47Dialect struct {
	player.V47Packets
}

func newV47Dialect() *v47Dialect { return &v47Dialect{} }

func (*v47Dialect) Protocol() protocol.Protocol { return v1_8.Protocol() }

// Read maps every serverbound play packet the play path handles. steer_vehicle
// and enchant_item are handled by being ignored, so they are known and have no
// action; do not give them one for symmetry.
func (*v47Dialect) Read(packet protocol.Packet) (Action, bool) {
	switch value := packet.Value.(type) {
	case *v1_8.PlayServerboundKeepAlive:
		return KeepAliveAction{ID: int64(value.KeepAliveID)}, true
	case *v1_8.PlayServerboundChat:
		return ChatAction{Message: value.Message}, true
	case *v1_8.PlayServerboundUseEntity:
		return UseEntityAction{Target: value.Target, Mouse: value.Mouse}, true
	case *v1_8.PlayServerboundFlying:
		return MoveAction{OnGround: value.OnGround}, true
	case *v1_8.PlayServerboundPosition:
		return MoveAction{
			X: value.X, Y: value.Y, Z: value.Z,
			OnGround: value.OnGround, HasPos: true,
		}, true
	case *v1_8.PlayServerboundLook:
		return MoveAction{
			Yaw: value.Yaw, Pitch: value.Pitch,
			OnGround: value.OnGround, HasLook: true,
		}, true
	case *v1_8.PlayServerboundPositionLook:
		return MoveAction{
			X: value.X, Y: value.Y, Z: value.Z,
			Yaw: value.Yaw, Pitch: value.Pitch,
			OnGround: value.OnGround, HasPos: true, HasLook: true,
		}, true
	case *v1_8.PlayServerboundBlockDig:
		return DigAction{
			Pos:    fromV47Position(value.Location),
			Status: DigStatus(value.Status),
			Face:   BlockFace(value.Face),
		}, true
	case *v1_8.PlayServerboundBlockPlace:
		return PlaceAction{
			Pos:      fromV47Position(value.Location),
			Face:     BlockFace(value.Direction),
			HeldItem: player.SlotFromV47(value.HeldItem),
			Cursor:   [3]int8{value.CursorX, value.CursorY, value.CursorZ},
		}, true
	case *v1_8.PlayServerboundHeldItemSlot:
		return HeldSlotAction{Slot: value.SlotID}, true
	case *v1_8.PlayServerboundArmAnimation:
		return ArmSwingAction{}, true
	case *v1_8.PlayServerboundEntityAction:
		return EntityActionAction{
			EntityID: value.EntityID, ActionID: value.ActionID, JumpBoost: value.JumpBoost,
		}, true
	case *v1_8.PlayServerboundCloseWindow:
		return CloseWindowAction{WindowID: int8(value.WindowID)}, true
	case *v1_8.PlayServerboundWindowClick:
		return ClickAction{
			WindowID:    int8(value.WindowID),
			Slot:        value.Slot,
			MouseButton: value.MouseButton,
			Action:      value.Action,
			Mode:        value.Mode,
			Item:        player.SlotFromV47(value.Item),
		}, true
	case *v1_8.PlayServerboundTransaction:
		return TransactionAction{
			WindowID: value.WindowID, Action: value.Action, Accepted: value.Accepted,
		}, true
	case *v1_8.PlayServerboundSetCreativeSlot:
		return CreativeSlotAction{Slot: value.Slot, Item: player.SlotFromV47(value.Item)}, true
	case *v1_8.PlayServerboundUpdateSign:
		return SignAction{
			Pos:   fromV47Position(value.Location),
			Text1: value.Text1, Text2: value.Text2, Text3: value.Text3, Text4: value.Text4,
		}, true
	case *v1_8.PlayServerboundAbilities:
		return AbilitiesAction{
			Flags: value.Flags, FlyingSpeed: value.FlyingSpeed, WalkingSpeed: value.WalkingSpeed,
		}, true
	case *v1_8.PlayServerboundTabComplete:
		return TabCompleteAction{Text: value.Text}, true
	case *v1_8.PlayServerboundSettings:
		return SettingsAction{
			Locale:       value.Locale,
			ViewDistance: value.ViewDistance,
			ChatFlags:    value.ChatFlags,
			ChatColors:   value.ChatColors,
			SkinParts:    value.SkinParts,
		}, true
	case *v1_8.PlayServerboundClientCommand:
		return ClientCommandAction{Payload: value.Payload}, true
	case *v1_8.PlayServerboundCustomPayload:
		return PayloadAction{Channel: value.Channel, Data: value.Data}, true
	case *v1_8.PlayServerboundSpectate:
		return SpectateAction{Target: value.Target.String()}, true
	case *v1_8.PlayServerboundResourcePackReceive:
		return ResourcePackAction{Hash: value.Hash, Result: value.Result}, true
	case *v1_8.PlayServerboundSteerVehicle, *v1_8.PlayServerboundEnchantItem:
		return nil, true
	default:
		return nil, false
	}
}

func (*v47Dialect) Join(f JoinFields) world.Packet {
	return &v1_8.PlayClientboundLogin{
		EntityID:         f.EntityID,
		GameMode:         f.GameMode,
		Dimension:        f.Dimension,
		Difficulty:       f.Difficulty,
		MaxPlayers:       f.MaxPlayers,
		LevelType:        f.LevelType,
		ReducedDebugInfo: f.ReducedDebugInfo,
	}
}

func (*v47Dialect) Position(f PositionFields) world.Packet {
	return &v1_8.PlayClientboundPosition{
		X:     f.X,
		Y:     f.Y,
		Z:     f.Z,
		Yaw:   f.Yaw,
		Pitch: f.Pitch,
		Flags: f.Flags,
	}
}

// KeepAlive narrows the ID to protocol 47's VarInt. The server counts its own
// IDs up from one, so the narrowing never loses anything it sent.
func (*v47Dialect) KeepAlive(id int64) world.Packet {
	return &v1_8.PlayClientboundKeepAlive{KeepAliveID: int32(id)}
}

func (*v47Dialect) Chat(message string, position int8) world.Packet {
	return &v1_8.PlayClientboundChat{Message: message, Position: position}
}

func (*v47Dialect) BlockChange(pos world.BlockPos, state int32) world.Packet {
	return &v1_8.PlayClientboundBlockChange{Location: v47Position(pos), Type: state}
}

func (*v47Dialect) WindowItems(window int8, slots []player.Slot) world.Packet {
	items := make([]v1_8.Slot, len(slots))
	for i, slot := range slots {
		items[i] = player.V47Slot(slot)
	}

	return &v1_8.PlayClientboundWindowItems{WindowID: uint8(window), Items: items}
}

func (*v47Dialect) SetSlot(window int8, slot int16, item player.Slot) world.Packet {
	return &v1_8.PlayClientboundSetSlot{
		WindowID: window,
		Slot:     slot,
		Item:     player.V47Slot(item),
	}
}

func (*v47Dialect) OpenWindow(f OpenWindowFields) world.Packet {
	return &v1_8.PlayClientboundOpenWindow{
		WindowID:      uint8(f.WindowID),
		InventoryType: f.InventoryType,
		WindowTitle:   f.WindowTitle,
		SlotCount:     f.SlotCount,
	}
}

func (*v47Dialect) Transaction(window int8, action int16, accepted bool) world.Packet {
	return &v1_8.PlayClientboundTransaction{
		WindowID: window,
		Action:   action,
		Accepted: accepted,
	}
}

func (*v47Dialect) Abilities(flags int8, flying, walking float32) world.Packet {
	return &v1_8.PlayClientboundAbilities{
		Flags:        flags,
		FlyingSpeed:  flying,
		WalkingSpeed: walking,
	}
}

func (*v47Dialect) UpdateHealth(health float32, food int32, saturation float32) world.Packet {
	return &v1_8.PlayClientboundUpdateHealth{
		Health:         health,
		Food:           food,
		FoodSaturation: saturation,
	}
}

func (*v47Dialect) UpdateTime(age, time int64) world.Packet {
	return &v1_8.PlayClientboundUpdateTime{Age: age, Time: time}
}

func (*v47Dialect) SpawnPosition(pos world.BlockPos) world.Packet {
	return &v1_8.PlayClientboundSpawnPosition{Location: v47Position(pos)}
}

func (*v47Dialect) Respawn(f RespawnFields) world.Packet {
	return &v1_8.PlayClientboundRespawn{
		Dimension:  f.Dimension,
		Difficulty: f.Difficulty,
		Gamemode:   f.GameMode,
		LevelType:  f.LevelType,
	}
}

func (*v47Dialect) GameStateChange(reason uint8, value float32) world.Packet {
	return &v1_8.PlayClientboundGameStateChange{Reason: reason, GameMode: value}
}

func (*v47Dialect) Kick(reason string) world.Packet {
	return &v1_8.PlayClientboundKickDisconnect{Reason: reason}
}

func (*v47Dialect) TabComplete(matches []string) world.Packet {
	return &v1_8.PlayClientboundTabComplete{Matches: matches}
}

func (*v47Dialect) CustomPayload(channel string, payload []byte) world.Packet {
	return &v1_8.PlayClientboundCustomPayload{Channel: channel, Data: payload}
}

func (*v47Dialect) BlockBreakAnimation(entity int32, pos world.BlockPos, stage int8) world.Packet {
	return &v1_8.PlayClientboundBlockBreakAnimation{
		EntityID:     entity,
		Location:     v47Position(pos),
		DestroyStage: stage,
	}
}

func (*v47Dialect) WorldEvent(effect int32, pos world.BlockPos, data int32, global bool) world.Packet {
	return &v1_8.PlayClientboundWorldEvent{
		EffectID: effect,
		Location: v47Position(pos),
		Data:     data,
		Global:   global,
	}
}

// SprintParticles builds the WorldParticles (0x2A) block-crack effect at the
// player's feet. Particle ID 37 = block crack, carrying the block state as its
// single VarInt data element. The field order and widths match the raw builder
// this replaced byte for byte.
func (*v47Dialect) SprintParticles(x, y, z float64, state int32) world.Packet {
	return &v1_8.PlayClientboundWorldParticles{
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
		Data:         v1_8.PlayClientboundWorldParticlesDataSwitch{Case37: [1]int32{state}},
	}
}

// v47Position builds a generated protocol 47 block Position. Its packed
// encoding is byte-identical to java.EncodePosition written as an int64.
func v47Position(pos world.BlockPos) v1_8.Position {
	return v1_8.Position{X: int32(pos.X), Y: int16(pos.Y), Z: int32(pos.Z)}
}

func fromV47Position(pos v1_8.Position) world.BlockPos {
	return world.BlockPos{X: int(pos.X), Y: int(pos.Y), Z: int(pos.Z)}
}
