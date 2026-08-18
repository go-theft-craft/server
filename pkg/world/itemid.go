package world

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
)

// Item identity.
//
// An ItemID names one item for the life of a server, across restarts. It is
// split rather than sequential so that a restart cannot reissue an ID the
// previous run already gave out: the epoch is persisted with the world and
// advanced once per start, and the counter runs within that start.
//
// The epoch is *persisted*, not derived from the clock. A clock that moves
// backwards — a correction, a container without a battery-backed one, a
// restored snapshot — would mint colliding IDs, and collision is the one
// failure that makes the whole structure worthless.

const (
	// epochBits is 16,777,215 starts. A server restarting every second
	// exhausts it in about six months.
	epochBits = 24
	// counterBits is 1,099,511,627,775 items. Minting a million a second
	// exhausts it in about twelve days.
	counterBits = 40

	maxEpoch   = 1<<epochBits - 1
	maxCounter = 1<<counterBits - 1
)

// ItemID identifies one item for the life of a server. The high 24 bits are
// the run epoch; the low 40 bits are a counter within that run.
//
// The zero value is not a valid ID: counters start at 1, so a stack that
// forgot to carry identity reads as unidentified rather than as item zero of
// the first run.
type ItemID uint64

// NoItemID is the zero value, spelled out where it is meant.
const NoItemID ItemID = 0

// NewItemID assembles an ID from its two halves.
func NewItemID(epoch uint32, counter uint64) ItemID {
	return ItemID(uint64(epoch&maxEpoch)<<counterBits | counter&maxCounter)
}

// Epoch is the run that minted the ID.
func (id ItemID) Epoch() uint32 { return uint32(id >> counterBits) }

// Counter is the ID's position within its run.
func (id ItemID) Counter() uint64 { return uint64(id) & maxCounter }

// Valid reports whether the ID names anything.
func (id ItemID) Valid() bool { return id != NoItemID }

// String renders an ID as epoch:counter, which is what a log line wants.
func (id ItemID) String() string {
	if !id.Valid() {
		return "none"
	}

	return fmt.Sprintf("%d:%d", id.Epoch(), id.Counter())
}

// ErrIDSpaceExhausted reports that no more IDs can be minted.
//
// Both exits — the counter within a run and the epoch across runs — refuse to
// mint, and the server keeps serving. A server that will not start is worse
// than an audit gap.
var ErrIDSpaceExhausted = errors.New("world: item ID space exhausted")

// Minter hands out IDs within one run.
type Minter struct {
	epoch uint32
	next  atomic.Uint64
}

// NewMinter returns a minter for one run's epoch.
func NewMinter(epoch uint32) (*Minter, error) {
	if epoch > maxEpoch {
		return nil, fmt.Errorf("%w: epoch %d past %d", ErrIDSpaceExhausted, epoch, maxEpoch)
	}

	return &Minter{epoch: epoch}, nil
}

// Epoch is the run this minter is issuing for.
func (m *Minter) Epoch() uint32 { return m.epoch }

// Mint returns the next ID. It is safe under concurrent callers.
func (m *Minter) Mint() (ItemID, error) {
	counter := m.next.Add(1)
	if counter > maxCounter {
		return NoItemID, fmt.Errorf("%w: counter past %d in epoch %d",
			ErrIDSpaceExhausted, uint64(maxCounter), m.epoch)
	}

	return NewItemID(m.epoch, counter), nil
}

// MintN returns n consecutive IDs, or an error and none of them.
func (m *Minter) MintN(n int) ([]ItemID, error) {
	if n <= 0 {
		return nil, nil
	}

	// Reserved in one step, so two callers never interleave a run of IDs.
	last := m.next.Add(uint64(n))
	if last > maxCounter {
		return nil, fmt.Errorf("%w: counter past %d in epoch %d",
			ErrIDSpaceExhausted, uint64(maxCounter), m.epoch)
	}

	out := make([]ItemID, n)
	for i := range out {
		out[i] = NewItemID(m.epoch, last-uint64(n)+uint64(i)+1)
	}

	return out, nil
}

// NextEpoch is the epoch a starting run should take, given the last one the
// world recorded.
//
// It reports ErrIDSpaceExhausted rather than wrapping: wrapping would reissue
// an epoch whose IDs are still in the world.
func NextEpoch(stored uint32) (uint32, error) {
	if stored >= maxEpoch {
		return 0, fmt.Errorf("%w: epoch %d is the last one", ErrIDSpaceExhausted, stored)
	}

	return stored + 1, nil
}

// ParseItemID reads back what String wrote.
//
// It exists because block identity is persisted in the sidecar as text: a
// sidecar is JSON a person may have to read during an incident, and
// "epoch:counter" is what every log line in this server already says.
func ParseItemID(s string) (ItemID, error) {
	if s == "none" || s == "" {
		return NoItemID, nil
	}

	epoch, counter, ok := strings.Cut(s, ":")
	if !ok {
		return NoItemID, fmt.Errorf("world: %q is not an item ID", s)
	}

	e, err := strconv.ParseUint(epoch, 10, 32)
	if err != nil {
		return NoItemID, fmt.Errorf("world: item ID %q has no epoch: %w", s, err)
	}
	c, err := strconv.ParseUint(counter, 10, 64)
	if err != nil {
		return NoItemID, fmt.Errorf("world: item ID %q has no counter: %w", s, err)
	}
	if e > maxEpoch || c > maxCounter {
		return NoItemID, fmt.Errorf("world: item ID %q is out of range", s)
	}

	return NewItemID(uint32(e), c), nil
}
