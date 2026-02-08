package storage

import (
	"github.com/OCharnyshevich/minecraft-server/internal/server/player"
)

// PlayerData is the serializable representation of a player's state.
type PlayerData struct {
	UUID      string        `json:"uuid"`
	Username  string        `json:"username"`
	Position  PositionData  `json:"position"`
	GameMode  uint8         `json:"gamemode"`
	Inventory InventoryData `json:"inventory"`
}

// PositionData holds a player's world position and orientation.
type PositionData struct {
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Z     float64 `json:"z"`
	Yaw   float32 `json:"yaw"`
	Pitch float32 `json:"pitch"`
}

// InventoryData holds a player's inventory state.
type InventoryData struct {
	Slots    [36]SlotData `json:"slots"`
	Armor    [4]SlotData  `json:"armor"`
	HeldSlot int16        `json:"held_slot"`
}

// SlotData is the serializable representation of an inventory slot.
type SlotData struct {
	BlockID    int16 `json:"block_id"`
	ItemCount  int8  `json:"item_count"`
	ItemDamage int16 `json:"item_damage"`
}

// WorldData holds world-level metadata for persistence.
type WorldData struct {
	Age       int64 `json:"age"`
	TimeOfDay int64 `json:"time_of_day"`
}

// BlockOverrideEntry is a single block override for JSON serialization.
type BlockOverrideEntry struct {
	X       int   `json:"x"`
	Y       int   `json:"y"`
	Z       int   `json:"z"`
	StateID int32 `json:"state_id"`
}

// PlayerDataFromPlayer extracts serializable data from a runtime Player.
func PlayerDataFromPlayer(p *player.Player) *PlayerData {
	pos := p.GetPosition()
	inv := p.Inventory

	pd := &PlayerData{
		UUID:     p.UUID,
		Username: p.Username,
		Position: PositionData{
			X:     pos.X,
			Y:     pos.Y,
			Z:     pos.Z,
			Yaw:   pos.Yaw,
			Pitch: pos.Pitch,
		},
		GameMode: p.GetGameMode(),
		Inventory: InventoryData{
			HeldSlot: inv.GetHeldSlot(),
		},
	}

	inv.ReadSlots(func(slots [36]player.Slot, armor [4]player.Slot) {
		for i, s := range slots {
			pd.Inventory.Slots[i] = SlotData{
				BlockID:    s.BlockID,
				ItemCount:  s.ItemCount,
				ItemDamage: s.ItemDamage,
			}
		}
		for i, s := range armor {
			pd.Inventory.Armor[i] = SlotData{
				BlockID:    s.BlockID,
				ItemCount:  s.ItemCount,
				ItemDamage: s.ItemDamage,
			}
		}
	})

	return pd
}
