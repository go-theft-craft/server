package server

import (
	"github.com/go-theft-craft/server/pkg/world"
)

// Player persistence.
//
// PlayerData is the whole of what a player's session carries between logins.
// It is public and names no internal type, so an application can keep players
// wherever it likes — a database, an object store, nothing at all — by
// implementing PlayerStore.
//
// Its JSON shape is the one the pre-M11.3 server wrote, field for field, so an
// existing players/<uuid>.json loads unchanged. That constraint is the reason
// world.ItemStack carries block_id / item_count / item_damage tags rather than
// names it would have chosen for itself.

// PlayerData is a player's saved state.
type PlayerData struct {
	UUID      string    `json:"uuid"`
	Username  string    `json:"username"`
	Position  Position  `json:"position"`
	GameMode  uint8     `json:"gamemode"`
	Inventory Inventory `json:"inventory"`
}

// Position is where a player stood and what they were looking at.
type Position struct {
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Z     float64 `json:"z"`
	Yaw   float32 `json:"yaw"`
	Pitch float32 `json:"pitch"`
}

// Inventory is a player's carried items.
//
// The slots are the 36 of the main inventory and hotbar in window order,
// followed by the four armor slots. HeldSlot is the hotbar index, 0 through 8.
type Inventory struct {
	Slots    [36]world.ItemStack `json:"slots"`
	Armor    [4]world.ItemStack  `json:"armor"`
	HeldSlot int16               `json:"held_slot"`
}

// PlayerStore is per-player persistence.
//
// LoadPlayer returns nil, nil for a player who has never logged in, which the
// server treats as a new player rather than as an error.
type PlayerStore interface {
	LoadPlayer(uuid string) (*PlayerData, error)
	SavePlayer(data PlayerData) error
}
