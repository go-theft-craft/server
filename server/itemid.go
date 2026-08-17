package server

import (
	"github.com/go-theft-craft/server/pkg/world"
)

// Item identity, re-exported.
//
// The types live in pkg/world because an ItemStack carries them, and pkg/world
// sits below this package. They are aliased here so an application composing a
// server sees one surface.
type (
	// ItemID identifies one item for the life of a server. See world.ItemID.
	ItemID = world.ItemID
	// Minter hands out IDs within one run.
	Minter = world.Minter
)

// NoItemID is the zero value, spelled out where it is meant.
const NoItemID = world.NoItemID

// ErrIDSpaceExhausted reports that no more IDs can be minted.
var ErrIDSpaceExhausted = world.ErrIDSpaceExhausted

// NewItemID assembles an ID from its epoch and counter.
func NewItemID(epoch uint32, counter uint64) ItemID { return world.NewItemID(epoch, counter) }

// NewMinter returns a minter for one run's epoch.
func NewMinter(epoch uint32) (*Minter, error) { return world.NewMinter(epoch) }

// NextEpoch is the epoch a starting run should take.
func NextEpoch(stored uint32) (uint32, error) { return world.NextEpoch(stored) }
