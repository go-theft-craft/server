package server

import (
	"fmt"
	"path/filepath"

	"github.com/go-theft-craft/server/internal/server/conn"
	"github.com/go-theft-craft/server/internal/server/player"
	"github.com/go-theft-craft/server/internal/server/storage"
	"github.com/go-theft-craft/server/pkg/world"
)

// FilePlayerStore keeps one JSON file per player under dir/players.
func FilePlayerStore(dir string) (PlayerStore, error) {
	root := filepath.Join(dir, "players")
	if err := storage.EnsureDir(root); err != nil {
		return nil, fmt.Errorf("create player directory: %w", err)
	}

	return filePlayerStore{dir: root}, nil
}

type filePlayerStore struct{ dir string }

func (s filePlayerStore) path(uuid string) string {
	return filepath.Join(s.dir, uuid+".json")
}

func (s filePlayerStore) LoadPlayer(uuid string) (*PlayerData, error) {
	var data PlayerData

	found, err := storage.ReadJSON(s.path(uuid), &data)
	if err != nil {
		return nil, fmt.Errorf("read player %s: %w", uuid, err)
	}
	if !found {
		return nil, nil
	}

	return &data, nil
}

func (s filePlayerStore) SavePlayer(data PlayerData) error {
	if data.UUID == "" {
		return fmt.Errorf("save player: no UUID")
	}

	return storage.WriteJSONAtomic(s.path(data.UUID), data)
}

// playerBridge adapts the public PlayerStore, which speaks PlayerData, to what
// a connection needs, which is the runtime player.
//
// The conversion lives here because this is the only package that sees both:
// PlayerData is public and internal/server/conn cannot import it, and the
// runtime player is internal and an application implementing PlayerStore must
// never see it.
type playerBridge struct{ store PlayerStore }

var _ conn.PlayerStore = playerBridge{}

func (b playerBridge) LoadPlayer(p *player.Player) (bool, error) {
	data, err := b.store.LoadPlayer(p.UUID)
	if err != nil || data == nil {
		return false, err
	}

	var slots [36]player.Slot
	var armor [4]player.Slot
	for i, s := range data.Inventory.Slots {
		slots[i] = player.Slot{BlockID: s.ID, ItemCount: s.Count, ItemDamage: s.Damage}
	}
	for i, s := range data.Inventory.Armor {
		armor[i] = player.Slot{BlockID: s.ID, ItemCount: s.Count, ItemDamage: s.Damage}
	}

	p.ApplyData(player.Position{
		X:     data.Position.X,
		Y:     data.Position.Y,
		Z:     data.Position.Z,
		Yaw:   data.Position.Yaw,
		Pitch: data.Position.Pitch,
	}, data.GameMode, slots, armor, data.Inventory.HeldSlot)

	return true, nil
}

func (b playerBridge) SavePlayer(p *player.Player) error {
	return b.store.SavePlayer(snapshotPlayer(p))
}

// snapshotPlayer is what a player carries between logins.
func snapshotPlayer(p *player.Player) PlayerData {
	pos := p.GetPosition()

	data := PlayerData{
		UUID:     p.UUID,
		Username: p.Username,
		Position: Position{X: pos.X, Y: pos.Y, Z: pos.Z, Yaw: pos.Yaw, Pitch: pos.Pitch},
		GameMode: p.GetGameMode(),
		Inventory: Inventory{
			HeldSlot: p.Inventory.GetHeldSlot(),
		},
	}

	p.Inventory.ReadSlots(func(slots [36]player.Slot, armor [4]player.Slot) {
		for i, s := range slots {
			data.Inventory.Slots[i] = world.ItemStack{ID: s.BlockID, Count: s.ItemCount, Damage: s.ItemDamage}
		}
		for i, s := range armor {
			data.Inventory.Armor[i] = world.ItemStack{ID: s.BlockID, Count: s.ItemCount, Damage: s.ItemDamage}
		}
	})

	return data
}
