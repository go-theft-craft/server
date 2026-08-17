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
	// ItemIndex tracks where every identified item is, and is the write path
	// rather than an observer of it: a move that claims an item came from
	// somewhere it is not is a duplication caught as it happens.
	ItemIndex = world.ItemIndex
	// Location is where an item is.
	Location = world.Location
	// LocationKind is what sort of place that is.
	LocationKind = world.LocationKind
	// Actor is who caused a movement.
	Actor = world.Actor
	// ActorKind is what sort of actor that is.
	ActorKind = world.ActorKind
	// ErrDuplicate reports an ID that was live somewhere other than where a
	// move said it came from.
	ErrDuplicate = world.ErrDuplicate
	// DuplicatePolicy is what the index does when it detects one.
	DuplicatePolicy = world.DuplicatePolicy
)

// The places an item can be.
const (
	LocationNowhere   = world.LocationNowhere
	LocationInventory = world.LocationInventory
	LocationCursor    = world.LocationCursor
	LocationCrafting  = world.LocationCrafting
	LocationContainer = world.LocationContainer
	LocationEntity    = world.LocationEntity
	LocationWorld     = world.LocationWorld
)

// The kinds of actor.
const (
	ActorServer    = world.ActorServer
	ActorPlayer    = world.ActorPlayer
	ActorReconcile = world.ActorReconcile
)

// The two duplicate policies. The default records and allows: refusing turns
// a duplication bug into item loss, and item loss on a false positive is worse
// for the player than an extra item.
const (
	DuplicateAllow  = world.DuplicateAllow
	DuplicateRefuse = world.DuplicateRefuse
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
