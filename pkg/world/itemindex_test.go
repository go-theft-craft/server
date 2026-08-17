package world

import (
	"errors"
	"testing"
)

func newTestIndex(t *testing.T, policy DuplicatePolicy) (ItemIndex, *[]*ErrDuplicate) {
	t.Helper()

	m, err := NewMinter(1)
	if err != nil {
		t.Fatalf("NewMinter: %v", err)
	}
	var seen []*ErrDuplicate

	return NewItemIndex(m, policy, func(d *ErrDuplicate) { seen = append(seen, d) }), &seen
}

func inventoryAt(player string, slot int) Location {
	return Location{Kind: LocationInventory, Player: player, Slot: slot}
}

func TestMintRecordsWhereItemsAre(t *testing.T) {
	index, _ := newTestIndex(t, DuplicateAllow)
	loc := inventoryAt("alice", 3)

	ids, err := index.Mint(4, loc, Actor{Kind: ActorServer})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if len(ids) != 4 {
		t.Fatalf("Mint(4) gave %d IDs", len(ids))
	}
	if index.Len() != 4 {
		t.Fatalf("index holds %d IDs, want 4", index.Len())
	}

	for _, id := range ids {
		where, ok := index.Where(id)
		if !ok || where != loc {
			t.Fatalf("Where(%s) = %s (ok=%v), want %s", id, where, ok, loc)
		}
	}
}

func TestWhereAnswersTheCurrentLocation(t *testing.T) {
	index, _ := newTestIndex(t, DuplicateAllow)
	from := inventoryAt("alice", 0)
	to := Location{Kind: LocationCursor, Player: "alice"}

	ids, _ := index.Mint(2, from, Actor{})
	if err := index.Move(ids, from, to, Actor{Kind: ActorPlayer, Name: "Alice"}); err != nil {
		t.Fatalf("Move: %v", err)
	}

	for _, id := range ids {
		if where, _ := index.Where(id); where != to {
			t.Fatalf("Where(%s) = %s, want %s", id, where, to)
		}
	}

	if _, ok := index.Where(NewItemID(9, 9)); ok {
		t.Fatal("an ID nobody minted has a location")
	}
}

// TestMoveRejectsAnIDThatIsLiveElsewhere is the whole point of the index: a
// move that claims an item came from somewhere it is not is a duplication in
// progress.
func TestMoveRejectsAnIDThatIsLiveElsewhere(t *testing.T) {
	index, seen := newTestIndex(t, DuplicateAllow)

	chest := Location{Kind: LocationContainer, Block: BlockPos{X: 1, Y: 2, Z: 3}, Slot: 0}
	bag := inventoryAt("alice", 5)

	ids, _ := index.Mint(1, bag, Actor{})
	// The item really moved into the chest.
	if err := index.Move(ids, bag, chest, Actor{Kind: ActorPlayer, Name: "Alice"}); err != nil {
		t.Fatalf("Move: %v", err)
	}

	// A second move claiming it is still in the bag: the source thinks it kept
	// the stack it deposited.
	err := index.Move(ids, bag, inventoryAt("alice", 6), Actor{Kind: ActorPlayer, Name: "Alice"})
	var dup *ErrDuplicate
	if !errors.As(err, &dup) {
		t.Fatalf("Move from a stale location returned %v, want ErrDuplicate", err)
	}
	if len(*seen) != 1 {
		t.Fatalf("the observer saw %d duplications, want 1", len(*seen))
	}
}

// TestErrDuplicateNamesBothLocationsAndTheActor: the first question about a
// duplication is which two places claimed the item and who was acting.
func TestErrDuplicateNamesBothLocationsAndTheActor(t *testing.T) {
	index, _ := newTestIndex(t, DuplicateAllow)

	chest := Location{Kind: LocationContainer, Block: BlockPos{X: 10, Y: 64, Z: -3}, Slot: 2}
	bag := inventoryAt("00000000-0000-3000-8000-000000000001", 5)

	ids, _ := index.Mint(1, bag, Actor{})
	_ = index.Move(ids, bag, chest, Actor{})

	err := index.Move(ids, bag, inventoryAt("00000000-0000-3000-8000-000000000001", 6),
		Actor{Kind: ActorPlayer, Name: "Fixture"})

	var dup *ErrDuplicate
	if !errors.As(err, &dup) {
		t.Fatalf("got %v, want ErrDuplicate", err)
	}
	if dup.ID != ids[0] {
		t.Errorf("names item %s, want %s", dup.ID, ids[0])
	}
	if dup.Actual != chest || dup.Expected != bag {
		t.Errorf("locations are %s and %s, want %s and %s", dup.Actual, dup.Expected, chest, bag)
	}
	if dup.By.Name != "Fixture" || dup.By.Kind != ActorPlayer {
		t.Errorf("actor is %s, want the player Fixture", dup.By)
	}
	for _, want := range []string{"container", "inventory", "Fixture"} {
		if !contains(dup.Error(), want) {
			t.Errorf("message %q does not mention %q", dup.Error(), want)
		}
	}
}

// TestTheDefaultPolicyRecordsAndAllows: refusing turns a duplication bug into
// item loss, and item loss on a false positive is worse for the player.
func TestTheDefaultPolicyRecordsAndAllows(t *testing.T) {
	index, seen := newTestIndex(t, DuplicateAllow)

	bag := inventoryAt("alice", 0)
	chest := Location{Kind: LocationContainer, Block: BlockPos{}, Slot: 0}
	elsewhere := inventoryAt("alice", 1)

	ids, _ := index.Mint(1, bag, Actor{})
	_ = index.Move(ids, bag, chest, Actor{})
	_ = index.Move(ids, bag, elsewhere, Actor{})

	if where, _ := index.Where(ids[0]); where != elsewhere {
		t.Fatalf("the write was refused: item is at %s, want %s", where, elsewhere)
	}
	if len(*seen) != 1 {
		t.Fatalf("the duplication was not recorded")
	}
}

func TestRefusePolicyRejectsTheWrite(t *testing.T) {
	index, seen := newTestIndex(t, DuplicateRefuse)

	bag := inventoryAt("alice", 0)
	chest := Location{Kind: LocationContainer, Block: BlockPos{}, Slot: 0}

	ids, _ := index.Mint(1, bag, Actor{})
	_ = index.Move(ids, bag, chest, Actor{})
	_ = index.Move(ids, bag, inventoryAt("alice", 1), Actor{})

	if where, _ := index.Where(ids[0]); where != chest {
		t.Fatalf("the write went through under DuplicateRefuse: item is at %s", where)
	}
	if len(*seen) != 1 {
		t.Fatal("the duplication was not recorded")
	}
}

// TestRetireMakesAnIDReusableByNobody: the minter only counts forward, so a
// retired ID is gone rather than freed.
func TestRetireMakesAnIDReusableByNobody(t *testing.T) {
	index, _ := newTestIndex(t, DuplicateAllow)
	loc := inventoryAt("alice", 0)

	ids, _ := index.Mint(3, loc, Actor{})
	if err := index.Retire(ids[:2], loc, Actor{Kind: ActorPlayer}); err != nil {
		t.Fatalf("Retire: %v", err)
	}

	for _, id := range ids[:2] {
		if _, ok := index.Where(id); ok {
			t.Fatalf("%s still has a location after being retired", id)
		}
	}
	if index.Len() != 1 {
		t.Fatalf("index holds %d IDs after retiring two of three", index.Len())
	}

	next, _ := index.Mint(1, loc, Actor{})
	for _, old := range ids {
		if next[0] == old {
			t.Fatalf("%s was reissued", old)
		}
	}
}

func TestRetiringFromTheWrongPlaceIsADuplication(t *testing.T) {
	index, seen := newTestIndex(t, DuplicateAllow)

	bag := inventoryAt("alice", 0)
	ids, _ := index.Mint(1, bag, Actor{})
	_ = index.Move(ids, bag, Location{Kind: LocationEntity, Entity: 7}, Actor{})

	if err := index.Retire(ids, bag, Actor{Kind: ActorPlayer}); err == nil {
		t.Fatal("retiring an item from a place it is not was accepted silently")
	}
	if len(*seen) != 1 {
		t.Fatal("the retirement mismatch was not recorded")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		len(needle) == 0 || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}

	return -1
}

// TestThePartialDepositBugIsDetected reconstructs the M3 tryAddToSection bug:
// part of a stack was deposited into a container and the source stack was left
// intact, so the same items existed in both places.
//
// The bug itself was explained and fixed in e67ec09. This asserts that the
// instrument would have caught it — which is the only way to test a detector
// that has no open bug to find.
func TestThePartialDepositBugIsDetected(t *testing.T) {
	index, seen := newTestIndex(t, DuplicateAllow)

	bag := inventoryAt("00000000-0000-3000-8000-000000000001", 9)
	chest := Location{Kind: LocationContainer, Block: BlockPos{X: 4, Y: 64, Z: 4}, Slot: 0}
	alice := Actor{Kind: ActorPlayer, UUID: "00000000-0000-3000-8000-000000000001", Name: "Fixture"}

	// A stack of eight in the player's bag.
	stack := ItemStack{BlockID: 1, ItemCount: 8}
	var err error
	stack.IDs, err = index.Mint(8, bag, alice)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	// Five of them are shift-clicked into the chest, and the move is recorded.
	deposited, _ := stack.Split(5)
	if err := index.Move(deposited.IDs, bag, chest, alice); err != nil {
		t.Fatalf("Move: %v", err)
	}

	// The bug: the source slot was never reduced, so the player still holds
	// all eight — the five in the chest among them. The next thing that
	// happens to the source stack claims those five are still in the bag.
	stillInBag := stack
	err = index.Move(stillInBag.IDs, bag, Location{Kind: LocationCursor, Player: alice.UUID}, alice)

	var dup *ErrDuplicate
	if !errors.As(err, &dup) {
		t.Fatal("the partial-deposit duplication was not detected")
	}
	if len(*seen) != 5 {
		t.Fatalf("%d of the 5 duplicated items were detected", len(*seen))
	}
	if dup.Actual.Kind != LocationContainer {
		t.Errorf("the duplication names %s, want the container the items really went to", dup.Actual)
	}
}

// And the same sequence done correctly reports nothing, so the detector is not
// simply always firing.
func TestACorrectDepositIsNotADuplication(t *testing.T) {
	index, seen := newTestIndex(t, DuplicateAllow)

	bag := inventoryAt("alice", 9)
	chest := Location{Kind: LocationContainer, Block: BlockPos{X: 4, Y: 64, Z: 4}, Slot: 0}
	alice := Actor{Kind: ActorPlayer, UUID: "alice", Name: "Alice"}

	stack := ItemStack{BlockID: 1, ItemCount: 8}
	stack.IDs, _ = index.Mint(8, bag, alice)

	deposited, remaining := stack.Split(5)
	if err := index.Move(deposited.IDs, bag, chest, alice); err != nil {
		t.Fatalf("Move: %v", err)
	}
	// The source really did shrink, so what moves next is only what is left.
	if err := index.Move(remaining.IDs, bag, Location{Kind: LocationCursor, Player: "alice"}, alice); err != nil {
		t.Fatalf("Move: %v", err)
	}

	if len(*seen) != 0 {
		t.Fatalf("a correct deposit reported %d duplications", len(*seen))
	}
}
