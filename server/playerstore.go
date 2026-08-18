package server

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/go-theft-craft/server/internal/server/conn"
	"github.com/go-theft-craft/server/internal/server/player"
	"github.com/go-theft-craft/server/internal/server/storage"
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

func (s filePlayerStore) LoadPlayer(_ context.Context, uuid string) (PlayerData, bool, error) {
	var data PlayerData

	found, err := storage.ReadJSON(s.path(uuid), &data)
	if err != nil {
		return PlayerData{}, false, fmt.Errorf("read player %s: %w", uuid, err)
	}

	return data, found, nil
}

func (s filePlayerStore) SavePlayer(_ context.Context, data PlayerData) error {
	if data.UUID == "" {
		return fmt.Errorf("save player: no UUID")
	}

	return storage.WriteJSONAtomic(s.path(data.UUID), data)
}

func (s filePlayerStore) Close() error { return nil }

// playerBridge adapts the public PlayerStore, which speaks PlayerData, to what
// a connection needs, which is the runtime player.
//
// The conversion lives here because this is the only package that sees both:
// PlayerData is public and internal/server/conn cannot import it, and the
// runtime player is internal and an application implementing PlayerStore must
// never see it.
type playerBridge struct {
	store PlayerStore
	// srv is what a loaded inventory is reconciled against. It is nil in a
	// server built without item identity, and reconcileInventory tolerates it.
	srv *Server
}

var _ conn.PlayerStore = playerBridge{}

func (b playerBridge) LoadPlayer(p *player.Player) (bool, error) {
	data, found, err := b.store.LoadPlayer(context.Background(), p.UUID)
	if err != nil || !found {
		return false, err
	}

	// A saved slot and a runtime one are the same type now, so this carries
	// the stack through whole — identity included.
	slots := data.Inventory.Slots
	armor := data.Inventory.Armor

	// Squared with the index before the player can click anything: an ID that
	// came off disk is claimed where it was found, an item that came off disk
	// without one gets it minted here, and either way the first click sees an
	// index that already agrees with the inventory in front of it.
	b.srv.reconcileInventory(p.UUID, slots[:], armor[:])

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
	return b.store.SavePlayer(context.Background(), snapshotPlayer(p))
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
		data.Inventory.Slots = slots
		data.Inventory.Armor = armor
	})

	return data
}
