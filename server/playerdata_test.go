package server_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/server"
)

// legacyPlayerJSON is a players/<uuid>.json exactly as the pre-M11.3 server
// wrote one. The UUID is generated, not a real account's.
const legacyPlayerJSON = `{
  "uuid": "00000000-0000-3000-8000-000000000001",
  "username": "Fixture",
  "position": {
    "x": 12.5,
    "y": 68,
    "z": -3.25,
    "yaw": 90,
    "pitch": -12.5
  },
  "gamemode": 1,
  "inventory": {
    "slots": [
      {"block_id": 1, "item_count": 64, "item_damage": 0},
      {"block_id": -1, "item_count": 0, "item_damage": 0}
    ],
    "armor": [
      {"block_id": 306, "item_count": 1, "item_damage": 3}
    ],
    "held_slot": 4
  }
}
`

// TestLegacyPlayerJSONLoads is the whole compatibility argument for making
// player data public: the shape must not move, because an existing file has to
// keep loading.
func TestLegacyPlayerJSONLoads(t *testing.T) {
	const uuid = "00000000-0000-3000-8000-000000000001"

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "players"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "players", uuid+".json")
	if err := os.WriteFile(path, []byte(legacyPlayerJSON), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	store, err := server.FilePlayerStore(dir)
	if err != nil {
		t.Fatalf("FilePlayerStore: %v", err)
	}

	data, err := store.LoadPlayer(uuid)
	if err != nil {
		t.Fatalf("LoadPlayer: %v", err)
	}
	if data == nil {
		t.Fatal("the fixture loaded as an absent player")
	}

	if data.Username != "Fixture" || data.GameMode != 1 {
		t.Errorf("username %q gamemode %d, want %q and 1", data.Username, data.GameMode, "Fixture")
	}
	if data.Position.X != 12.5 || data.Position.Y != 68 || data.Position.Z != -3.25 {
		t.Errorf("position = %+v, want 12.5, 68, -3.25", data.Position)
	}
	if data.Position.Yaw != 90 || data.Position.Pitch != -12.5 {
		t.Errorf("orientation = %v/%v, want 90/-12.5", data.Position.Yaw, data.Position.Pitch)
	}
	if want := (world.ItemStack{ID: 1, Count: 64}); data.Inventory.Slots[0] != want {
		t.Errorf("slot 0 = %+v, want %+v", data.Inventory.Slots[0], want)
	}
	if want := (world.ItemStack{ID: 306, Count: 1, Damage: 3}); data.Inventory.Armor[0] != want {
		t.Errorf("armor 0 = %+v, want %+v", data.Inventory.Armor[0], want)
	}
	if data.Inventory.HeldSlot != 4 {
		t.Errorf("held slot = %d, want 4", data.Inventory.HeldSlot)
	}
}

func TestAPlayerRoundTripsThroughTheFileStore(t *testing.T) {
	dir := t.TempDir()
	store, err := server.FilePlayerStore(dir)
	if err != nil {
		t.Fatalf("FilePlayerStore: %v", err)
	}

	want := server.PlayerData{
		UUID:     "00000000-0000-3000-8000-00000000002a",
		Username: "Round",
		Position: server.Position{X: -1.5, Y: 70, Z: 2.25, Yaw: 45, Pitch: 10},
		GameMode: 0,
		Inventory: server.Inventory{
			HeldSlot: 8,
		},
	}
	want.Inventory.Slots[35] = world.ItemStack{ID: 264, Count: 12, Damage: 1}
	want.Inventory.Armor[3] = world.ItemStack{ID: 310, Count: 1}

	if err := store.SavePlayer(want); err != nil {
		t.Fatalf("SavePlayer: %v", err)
	}

	got, err := store.LoadPlayer(want.UUID)
	if err != nil {
		t.Fatalf("LoadPlayer: %v", err)
	}
	if got == nil || *got != want {
		t.Fatalf("round trip gave %+v, want %+v", got, want)
	}
}

func TestAnUnknownPlayerIsAbsentRatherThanAnError(t *testing.T) {
	store, err := server.FilePlayerStore(t.TempDir())
	if err != nil {
		t.Fatalf("FilePlayerStore: %v", err)
	}

	data, err := store.LoadPlayer("00000000-0000-3000-8000-000000000009")
	if err != nil {
		t.Fatalf("LoadPlayer: %v", err)
	}
	if data != nil {
		t.Fatalf("an unknown player loaded as %+v", data)
	}
}

// externalPlayerStore is what an application outside this module writes. It
// compiles only if PlayerStore names no internal type.
type externalPlayerStore struct {
	saved map[string]server.PlayerData
}

func (s *externalPlayerStore) LoadPlayer(uuid string) (*server.PlayerData, error) {
	data, ok := s.saved[uuid]
	if !ok {
		return nil, nil
	}

	return &data, nil
}

func (s *externalPlayerStore) SavePlayer(data server.PlayerData) error {
	s.saved[data.UUID] = data

	return nil
}

func TestAnExternalTypeSatisfiesPlayerStore(t *testing.T) {
	store := &externalPlayerStore{saved: map[string]server.PlayerData{}}

	srv, err := server.New(server.WithPlayerStore(store))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if srv == nil {
		t.Fatal("New returned no server")
	}
}

func TestWithPlayerStoreRejectsNil(t *testing.T) {
	if _, err := server.New(server.WithPlayerStore(nil)); err == nil {
		t.Error("WithPlayerStore accepted nil; use no option at all to run without it")
	}
}
