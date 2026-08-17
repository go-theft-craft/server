package server_test

import (
	"context"
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

	data, found, err := store.LoadPlayer(context.Background(), uuid)
	if err != nil {
		t.Fatalf("LoadPlayer: %v", err)
	}
	if !found {
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
	if want := (world.ItemStack{BlockID: 1, ItemCount: 64}); !data.Inventory.Slots[0].Equal(want) {
		t.Errorf("slot 0 = %+v, want %+v", data.Inventory.Slots[0], want)
	}
	if want := (world.ItemStack{BlockID: 306, ItemCount: 1, ItemDamage: 3}); !data.Inventory.Armor[0].Equal(want) {
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
	want.Inventory.Slots[35] = world.ItemStack{BlockID: 264, ItemCount: 12, ItemDamage: 1}
	want.Inventory.Armor[3] = world.ItemStack{BlockID: 310, ItemCount: 1}

	if err := store.SavePlayer(context.Background(), want); err != nil {
		t.Fatalf("SavePlayer: %v", err)
	}

	got, found, err := store.LoadPlayer(context.Background(), want.UUID)
	if err != nil {
		t.Fatalf("LoadPlayer: %v", err)
	}
	if !found || !samePlayerData(got, want) {
		t.Fatalf("round trip gave %+v (found=%v), want %+v", got, found, want)
	}
}

func TestAnUnknownPlayerIsAbsentRatherThanAnError(t *testing.T) {
	store, err := server.FilePlayerStore(t.TempDir())
	if err != nil {
		t.Fatalf("FilePlayerStore: %v", err)
	}

	data, found, err := store.LoadPlayer(context.Background(), "00000000-0000-3000-8000-000000000009")
	if err != nil {
		t.Fatalf("LoadPlayer: %v", err)
	}
	if found {
		t.Fatalf("an unknown player loaded as %+v", data)
	}
}

// externalPlayerStore is what an application outside this module writes. It
// compiles only if PlayerStore names no internal type.
type externalPlayerStore struct {
	saved map[string]server.PlayerData
}

func (s *externalPlayerStore) LoadPlayer(_ context.Context, uuid string) (server.PlayerData, bool, error) {
	data, ok := s.saved[uuid]

	return data, ok, nil
}

func (s *externalPlayerStore) SavePlayer(_ context.Context, data server.PlayerData) error {
	s.saved[data.UUID] = data

	return nil
}

func (s *externalPlayerStore) Close() error { return nil }

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

// samePlayerData compares two saved players. It exists because an ItemStack
// stopped being comparable with == the moment it carried item identity.
func samePlayerData(a, b server.PlayerData) bool {
	if a.UUID != b.UUID || a.Username != b.Username || a.Position != b.Position ||
		a.GameMode != b.GameMode || a.Inventory.HeldSlot != b.Inventory.HeldSlot {
		return false
	}
	for i := range a.Inventory.Slots {
		if !a.Inventory.Slots[i].Equal(b.Inventory.Slots[i]) {
			return false
		}
	}
	for i := range a.Inventory.Armor {
		if !a.Inventory.Armor[i].Equal(b.Inventory.Armor[i]) {
			return false
		}
	}

	return true
}
