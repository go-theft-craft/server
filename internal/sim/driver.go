// Package sim drives the shared simulation kernel authoritatively.
//
// The server and a headless client run the same rules from the same module and
// differ in what they do with a result: the client applies it to a fork it may
// throw away when the server disagrees, and this side applies it and tells the
// players. That asymmetry is why there are two small drivers rather than one
// abstraction over both — the shared half is adapter.Drive, and it is the only
// half worth sharing.
//
// A driver owns its own store, its own kernel, and the queue a connection's
// movement packets feed. It knows nothing about packets: a connection turns a
// packet into an intent before it arrives here, and turns a domain event back
// into a packet after it leaves. Nothing in this package imports the protocol.
//
// A server with no driver attached still accepts connections and still serves
// its world. This is a component the rest of the server may use, not a stage it
// must pass through.
package sim

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/go-theft-craft/minecraft-simulation/adapter"
	"github.com/go-theft-craft/minecraft-simulation/entity"
	"github.com/go-theft-craft/minecraft-simulation/geom"
	"github.com/go-theft-craft/minecraft-simulation/movement"
	"github.com/go-theft-craft/minecraft-simulation/runtime"
	simulation "github.com/go-theft-craft/minecraft-simulation/sim"
	"github.com/go-theft-craft/minecraft-simulation/world"
)

// ErrDriver reports a driver that cannot be built or used as asked.
var ErrDriver = errors.New("sim: invalid driver")

// Options configures a driver.
type Options struct {
	// Profile is the rules to simulate. It is required: there is no default
	// version, because a server that guessed one would move its players by
	// another game's numbers.
	Profile simulation.Profile
	// Logger receives the ticks that could not be applied. A driver without one
	// still runs and still reports through its return values; what it loses is
	// the record of a tick that read a region nobody had loaded, which is the
	// failure a server notices last.
	Logger *slog.Logger
	// OnEvent receives every domain event a tick emitted, in order. It is what a
	// connection turns into an outbound packet.
	OnEvent func(simulation.DomainEvent)
	// Limits bounds the work one tick may do. The zero value means the kernel's
	// defaults.
	Limits simulation.Limits
}

// Driver simulates the server's own bodies, one tick at a time.
//
// It is safe for concurrent use: connections queue intents from their own
// goroutines and the server's tick loop steps from its own.
type Driver struct {
	profile simulation.Profile
	kernel  simulation.Kernel
	store   *runtime.Memory
	names   simulation.BlockNames
	limits  simulation.Limits
	log     *slog.Logger
	onEvent func(simulation.DomainEvent)

	mu sync.Mutex
	// queued holds the intents for the next tick, in arrival order. The order is
	// what the tick's outcomes are indexed against, so it is a slice rather than
	// a map: two intents for one body in one tick mean the second, and the tick
	// is what decides that rather than this queue.
	queued []simulation.Command
	// bodies is the scope, in insertion order, so that two ticks with the same
	// players simulate them in the same order. The kernel's digest covers the
	// scope's order, and a server whose scope wandered would produce a different
	// result for the same tick.
	bodies []entity.ID
	tick   simulation.Tick
}

// New builds a driver over an empty world.
func New(options Options) (*Driver, error) {
	if options.Profile == nil {
		return nil, fmt.Errorf("%w: no profile", ErrDriver)
	}

	kernel, err := simulation.NewKernel(options.Profile)
	if err != nil {
		return nil, fmt.Errorf("sim: build a kernel: %w", err)
	}

	names, ok := options.Profile.(simulation.BlockNames)
	if !ok {
		return nil, fmt.Errorf("%w: the profile cannot resolve block names", ErrDriver)
	}

	log := options.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &Driver{
		profile: options.Profile,
		kernel:  kernel,
		store:   runtime.NewMemory(options.Profile),
		names:   names,
		limits:  options.Limits,
		log:     log,
		onEvent: options.OnEvent,
	}, nil
}

// Describe fills a region with one block, so that the simulation can tell air it
// knows about from a region nobody has loaded.
//
// The distinction is the whole point of the world's three states: a tick that
// swept an undescribed cell is incomplete and is not applied, which is what
// stops a server from moving a player through terrain it has not loaded.
func (d *Driver) Describe(from, to geom.BlockPos, name string) error {
	ref, ok := d.names.Ref(name)
	if !ok {
		return fmt.Errorf("%w: the profile does not know the block %q", ErrDriver, name)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	for x := from.X; x <= to.X; x++ {
		for y := from.Y; y <= to.Y; y++ {
			for z := from.Z; z <= to.Z; z++ {
				if err := d.store.SetBlock(geom.BlockPos{X: x, Y: y, Z: z}, ref); err != nil {
					return fmt.Errorf("sim: describe %q: %w", name, err)
				}
			}
		}
	}

	return nil
}

// SetBlock writes one cell.
func (d *Driver) SetBlock(pos geom.BlockPos, name string) error {
	return d.Describe(pos, pos, name)
}

// Add puts a body in the world and in the scope of every tick after this one.
func (d *Driver) Add(id entity.ID, state entity.State, locomotion movement.Locomotion) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.store.SetEntity(id, state)
	d.store.SetLocomotion(id, locomotion)
	for _, known := range d.bodies {
		if known == id {
			return
		}
	}
	d.bodies = append(d.bodies, id)
}

// Remove drops a body and everything queued for it.
//
// The removal goes through a change set rather than through a setter, because
// the store has no other way to forget a body: every write to it names the
// revision it was computed against, and that is what stops a stale write from
// landing. A driver that reached around it would be the one writer in the server
// that could.
func (d *Driver) Remove(id entity.ID) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, ok := d.store.Entities().Entity(id); ok {
		err := d.store.Apply(simulation.ChangeSet{
			BaseRevision: d.store.Revision(),
			Ops:          []simulation.Op{{Kind: simulation.OpRemoveEntity, Entity: id}},
		})
		if err != nil {
			return fmt.Errorf("sim: remove entity %d: %w", id, err)
		}
	}

	kept := d.bodies[:0]
	for _, known := range d.bodies {
		if known != id {
			kept = append(kept, known)
		}
	}
	d.bodies = kept

	filtered := d.queued[:0]
	for _, command := range d.queued {
		if input, ok := command.(movement.Input); ok && input.Entity == id {
			continue
		}
		filtered = append(filtered, command)
	}
	d.queued = filtered

	return nil
}

// Queue records an intent for the next tick.
//
// A command whose kind no phase of the profile handles is refused here rather
// than passed to a tick that would ignore it. The tick would still produce a
// result, and a connection reading that result would see its intent neither
// applied nor rejected — which is the failure mode worth spending an error on.
func (d *Driver) Queue(command simulation.Command) error {
	if _, ok := command.(movement.Input); !ok {
		return fmt.Errorf("%w: nothing here handles the command kind %q",
			ErrDriver, command.CommandKind())
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	d.queued = append(d.queued, command)

	return nil
}

// Tick simulates one tick and applies it if it was complete.
//
// An incomplete tick is not an error. It means the tick read a cell nobody has
// described, its work is dropped, and the caller is expected to load what the
// result names as missing before driving again. It is logged, because a server
// whose players stopped moving deserves to be told why.
func (d *Driver) Tick(ctx context.Context) (simulation.TickResult, error) {
	d.mu.Lock()
	source := &tickSource{
		tick:     d.tick + 1,
		commands: d.queued,
		limits:   d.limits,
		scope:    simulation.Scope{Entities: append([]entity.ID(nil), d.bodies...)},
	}
	d.queued = nil
	d.mu.Unlock()

	sink := &tickSink{driver: d}

	result, err := adapter.Drive(ctx, d.kernel, d.store, source, sink)
	if err != nil {
		return result, fmt.Errorf("sim: %w", err)
	}

	d.mu.Lock()
	d.tick = source.tick
	d.mu.Unlock()

	if !result.Completeness.Complete {
		d.log.Warn("a tick read a region nobody has described, so it was dropped",
			"tick", result.Tick, "missing", len(result.Completeness.Missing))
	}

	return result, nil
}

// Body returns what the server believes about a body.
func (d *Driver) Body(id entity.ID) (entity.State, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.store.Entities().Entity(id)
}

// Locomotion returns a body's movement state.
func (d *Driver) Locomotion(id entity.ID) (movement.Locomotion, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.store.Locomotion().Locomotion(id)
}

// Blocks returns the world the driver simulates over, for a caller that needs to
// read it. The view is a snapshot in the sense the store gives one: it answers
// consistently for as long as nobody writes.
func (d *Driver) Blocks() world.View {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.store.Blocks()
}

// tickSource is one tick's contribution, frozen before the tick runs.
type tickSource struct {
	tick     simulation.Tick
	commands []simulation.Command
	limits   simulation.Limits
	scope    simulation.Scope
}

func (s *tickSource) Tick() simulation.Tick          { return s.tick }
func (s *tickSource) Commands() []simulation.Command { return s.commands }
func (s *tickSource) Limits() simulation.Limits      { return s.limits }
func (s *tickSource) Scope() simulation.Scope        { return s.scope }

// tickSink applies an authoritative result. There is no fork here and nothing to
// reconcile: what the tick decided is what happened.
type tickSink struct{ driver *Driver }

func (s *tickSink) Apply(changes simulation.ChangeSet) error {
	s.driver.mu.Lock()
	defer s.driver.mu.Unlock()

	if err := s.driver.store.Apply(changes); err != nil {
		return fmt.Errorf("sim: apply: %w", err)
	}

	return nil
}

func (s *tickSink) Observe(result simulation.TickResult) {
	if s.driver.onEvent == nil {
		return
	}
	// Events are published from every result, complete or not: a tick that could
	// not be applied still says what it saw, and a consumer that only heard about
	// applied ticks would think nothing happened.
	for _, event := range result.Domain {
		s.driver.onEvent(event)
	}
}
