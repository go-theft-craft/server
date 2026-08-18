package sim_test

import (
	"context"
	"errors"
	"testing"

	data "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
	"github.com/go-theft-craft/minecraft-simulation/entity"
	"github.com/go-theft-craft/minecraft-simulation/geom"
	"github.com/go-theft-craft/minecraft-simulation/movement"
	v1_8 "github.com/go-theft-craft/minecraft-simulation/profile/java/v1_8"
	simulation "github.com/go-theft-craft/minecraft-simulation/sim"

	"github.com/go-theft-craft/server/internal/sim"
)

// player is the body every test here moves.
const player = entity.ID(1)

// profile builds the rules the driver runs.
func profile(t *testing.T) simulation.Profile {
	t.Helper()

	set, err := data.Data()
	if err != nil {
		t.Fatalf("load the 1.8.9 data set: %v", err)
	}
	built, err := v1_8.New(set)
	if err != nil {
		t.Fatalf("build the 1.8.9 profile: %v", err)
	}

	return built
}

// driverOn returns a driver over a floor, with a player standing on it.
func driverOn(t *testing.T, options sim.Options) (*sim.Driver, simulation.Profile) {
	t.Helper()

	built := profile(t)
	options.Profile = built

	driver, err := sim.New(options)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := driver.Describe(
		geom.BlockPos{X: -4, Y: 0, Z: -4}, geom.BlockPos{X: 4, Y: 0, Z: 4}, "stone",
	); err != nil {
		t.Fatalf("describe the floor: %v", err)
	}
	if err := driver.Describe(
		geom.BlockPos{X: -4, Y: 1, Z: -4}, geom.BlockPos{X: 4, Y: 6, Z: 4}, "air",
	); err != nil {
		t.Fatalf("describe the air: %v", err)
	}

	state, locomotion, ok := v1_8.Spawn(built, geom.Vec3{X: 0.5, Y: 1, Z: 0.5}, 0, 0)
	if !ok {
		t.Fatal("Spawn did not recognize its own profile")
	}
	driver.Add(player, state, locomotion)

	return driver, built
}

func TestADriverNeedsAProfile(t *testing.T) {
	if _, err := sim.New(sim.Options{}); !errors.Is(err, sim.ErrDriver) {
		t.Fatalf("New without a profile returned %v", err)
	}
}

func TestAMovementIntentMovesTheBodyInTheStore(t *testing.T) {
	driver, _ := driverOn(t, sim.Options{})

	before, ok := driver.Body(player)
	if !ok {
		t.Fatal("the driver forgot the body it was given")
	}

	// Two ticks, because the first one is airborne whatever it stands on: the
	// vertical collision compares the motion a body moved with against the motion
	// it asked for, and a body handed no motion is stopped by nothing. Both
	// profiles do this and both versions' servers agree.
	var result simulation.TickResult
	for range 2 {
		if err := driver.Queue(movement.Input{Entity: player, Forward: 1}); err != nil {
			t.Fatalf("Queue: %v", err)
		}

		var err error
		result, err = driver.Tick(context.Background())
		if err != nil {
			t.Fatalf("Tick: %v", err)
		}
		if !result.Completeness.Complete {
			t.Fatalf("the tick was incomplete: %+v", result.Completeness.Missing)
		}
	}

	after, ok := driver.Body(player)
	if !ok {
		t.Fatal("the body is gone")
	}
	if after.Box == before.Box {
		t.Error("a forward intent left the body where it was")
	}
	if !after.OnGround {
		t.Error("a body walking on a floor is not on the ground")
	}

	// The authority is the store, not the return value: a caller reading the
	// driver a tick later must see what the tick decided.
	if result.Changes.BaseRevision == 0 && after.Box == before.Box {
		t.Error("the change set was never applied")
	}
}

func TestAnIntentNothingHandlesIsRefusedRatherThanIgnored(t *testing.T) {
	driver, _ := driverOn(t, sim.Options{})

	if err := driver.Queue(unknownCommand{}); !errors.Is(err, sim.ErrDriver) {
		t.Fatalf("Queue accepted a command nothing handles: %v", err)
	}

	// And the tick still runs: one refused intent is not a broken server.
	result, err := driver.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if !result.Completeness.Complete {
		t.Fatal("the tick after a refused intent was incomplete")
	}
}

// unknownCommand is a command kind no phase of any profile here handles.
type unknownCommand struct{}

func (unknownCommand) CommandKind() string { return "test.unknown" }

func TestATickOverAnUndescribedRegionIsNotApplied(t *testing.T) {
	built := profile(t)

	driver, err := sim.New(sim.Options{Profile: built})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// A body and no world at all: the first sweep reads cells nobody has
	// described, which is what an unloaded region looks like from inside a tick.
	state, locomotion, ok := v1_8.Spawn(built, geom.Vec3{X: 0.5, Y: 1, Z: 0.5}, 0, 0)
	if !ok {
		t.Fatal("Spawn did not recognize its own profile")
	}
	driver.Add(player, state, locomotion)

	before, _ := driver.Body(player)
	result, err := driver.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if result.Completeness.Complete {
		t.Fatal("a tick over an undescribed region reported itself complete")
	}
	if len(result.Completeness.Missing) == 0 {
		t.Error("the incomplete result names no missing cell")
	}

	after, _ := driver.Body(player)
	if after != before {
		t.Errorf("an incomplete tick moved the body: %+v became %+v", before, after)
	}
}

func TestDomainEventsReachTheConsumer(t *testing.T) {
	var seen []string
	driver, _ := driverOn(t, sim.Options{
		OnEvent: func(event simulation.DomainEvent) { seen = append(seen, event.Kind) },
	})

	// A wall two cells away, and a body walking into it: the tick that stops
	// against it emits the collision a connection would turn into a packet.
	if err := driver.Describe(
		geom.BlockPos{X: 2, Y: 1, Z: -4}, geom.BlockPos{X: 2, Y: 3, Z: 4}, "stone",
	); err != nil {
		t.Fatalf("describe the wall: %v", err)
	}

	for range 30 {
		if err := driver.Queue(movement.Input{Entity: player, Forward: 1, Yaw: -90}); err != nil {
			t.Fatalf("Queue: %v", err)
		}
		if _, err := driver.Tick(context.Background()); err != nil {
			t.Fatalf("Tick: %v", err)
		}
	}

	var collided bool
	for _, kind := range seen {
		if kind == "movement.collided" {
			collided = true
		}
	}
	if !collided {
		t.Errorf("a body walked into a wall for thirty ticks and emitted %v", seen)
	}
}

func TestRemovingABodyStopsSimulatingIt(t *testing.T) {
	driver, _ := driverOn(t, sim.Options{})

	if err := driver.Queue(movement.Input{Entity: player, Forward: 1}); err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if err := driver.Remove(player); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if _, ok := driver.Body(player); ok {
		t.Error("the store still holds a removed body")
	}
	if _, ok := driver.Locomotion(player); ok {
		t.Error("the store still holds a removed body's locomotion")
	}

	// The intent queued for it went with it, so the next tick has nothing to do
	// and nothing to fail on.
	result, err := driver.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(result.Changes.Ops) != 0 {
		t.Errorf("a tick with no bodies produced %d operations", len(result.Changes.Ops))
	}
}

func TestTheTickNumberAdvancesAndTheScopeKeepsItsOrder(t *testing.T) {
	driver, built := driverOn(t, sim.Options{})

	second := entity.ID(2)
	state, locomotion, _ := v1_8.Spawn(built, geom.Vec3{X: 1.5, Y: 1, Z: 0.5}, 0, 0)
	driver.Add(second, state, locomotion)
	// Adding a body twice must not put it in the scope twice: the kernel walks
	// the scope in order and would simulate it against its own first result.
	driver.Add(second, state, locomotion)

	for tick := range 3 {
		result, err := driver.Tick(context.Background())
		if err != nil {
			t.Fatalf("Tick: %v", err)
		}
		if want := simulation.Tick(tick + 1); result.Tick != want {
			t.Fatalf("the driver reported tick %d, want %d", result.Tick, want)
		}

		// Two bodies, two entity writes and two locomotion writes each tick.
		if got := len(result.Changes.Ops); got != 4 {
			t.Fatalf("tick %d produced %d operations, want 4", result.Tick, got)
		}
	}
}

func TestACancelledContextDoesNotStep(t *testing.T) {
	driver, _ := driverOn(t, sim.Options{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := driver.Tick(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Tick on a cancelled context returned %v", err)
	}
}
