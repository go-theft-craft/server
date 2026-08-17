package world

import (
	"math/rand/v2"
	"testing"
)

// The identity invariant: when identity is on, len(IDs) == int(ItemCount) on
// every stack, after any sequence of splits and merges.

// ids builds a stack of n items with consecutive identities.
func ids(t *testing.T, epoch uint32, n int) ItemStack {
	t.Helper()

	m, err := NewMinter(epoch)
	if err != nil {
		t.Fatalf("NewMinter: %v", err)
	}
	minted, err := m.MintN(n)
	if err != nil {
		t.Fatalf("MintN: %v", err)
	}

	return ItemStack{BlockID: 1, ItemCount: int8(n), IDs: minted}
}

func holds(t *testing.T, s ItemStack, what string) {
	t.Helper()

	if s.IsEmpty() {
		if len(s.IDs) != 0 {
			t.Fatalf("%s: an empty stack carries %d IDs", what, len(s.IDs))
		}

		return
	}
	if len(s.IDs) != int(s.ItemCount) {
		t.Fatalf("%s: %d IDs for %d items", what, len(s.IDs), s.ItemCount)
	}
}

// TestSplitIsDeterministicAboutWhichIDsGoWhere: a replay of the same click
// sequence has to produce the same assignment, so the first n IDs go with the
// first stack.
func TestSplitIsDeterministicAboutWhichIDsGoWhere(t *testing.T) {
	stack := ids(t, 1, 10)

	taken, rest := stack.Split(4)

	holds(t, taken, "taken")
	holds(t, rest, "rest")
	for i := range 4 {
		if taken.IDs[i] != stack.IDs[i] {
			t.Fatalf("taken[%d] = %s, want %s", i, taken.IDs[i], stack.IDs[i])
		}
	}
	for i := range 6 {
		if rest.IDs[i] != stack.IDs[4+i] {
			t.Fatalf("rest[%d] = %s, want %s", i, rest.IDs[i], stack.IDs[4+i])
		}
	}

	// And the source is untouched: a split that mutated its input would make a
	// caller that kept a copy see items in two places.
	if len(stack.IDs) != 10 {
		t.Fatalf("the source stack now has %d IDs", len(stack.IDs))
	}
	again, _ := stack.Split(4)
	if !again.Equal(taken) {
		t.Fatal("splitting the same stack twice gave different halves")
	}
}

func TestSplitAtTheEdges(t *testing.T) {
	stack := ids(t, 1, 5)

	if taken, rest := stack.Split(0); !taken.IsEmpty() || !rest.Equal(stack) {
		t.Errorf("Split(0) = %+v, %+v; want nothing and everything", taken, rest)
	}
	if taken, rest := stack.Split(5); !taken.Equal(stack) || !rest.IsEmpty() {
		t.Errorf("Split(5) = %+v, %+v; want everything and nothing", taken, rest)
	}
	if taken, rest := stack.Split(99); !taken.Equal(stack) || !rest.IsEmpty() {
		t.Errorf("Split(99) = %+v, %+v; want everything and nothing", taken, rest)
	}
}

func TestMergeRefusesADifferentItem(t *testing.T) {
	stone := ids(t, 1, 4)
	dirt := ItemStack{BlockID: 3, ItemCount: 2, IDs: []ItemID{NewItemID(2, 1), NewItemID(2, 2)}}

	merged, rest := stone.Merge(dirt, 2)
	if !merged.Equal(stone) || !rest.Equal(dirt) {
		t.Fatal("a merge of different items moved something")
	}
}

func TestMergeOntoAnEmptyStackTakesTheItem(t *testing.T) {
	source := ids(t, 1, 6)

	merged, rest := EmptyStack.Merge(source, 4)

	holds(t, merged, "merged")
	holds(t, rest, "rest")
	if merged.BlockID != source.BlockID || merged.ItemCount != 4 {
		t.Fatalf("merged = %+v, want 4 of block %d", merged, source.BlockID)
	}
	if rest.ItemCount != 2 {
		t.Fatalf("rest = %+v, want 2 left", rest)
	}
}

// TestAStackWithIdentityOffCarriesNoIDs: identity is opt-in, and a stack that
// never had it must not grow one through a split or a merge.
func TestAStackWithIdentityOffCarriesNoIDs(t *testing.T) {
	plain := ItemStack{BlockID: 1, ItemCount: 10}

	taken, rest := plain.Split(3)
	if taken.IDs != nil || rest.IDs != nil {
		t.Fatalf("splitting an unidentified stack produced IDs: %v / %v", taken.IDs, rest.IDs)
	}

	merged, remainder := taken.Merge(rest, 7)
	if merged.IDs != nil || remainder.IDs != nil {
		t.Fatalf("merging unidentified stacks produced IDs: %v / %v", merged.IDs, remainder.IDs)
	}
	if merged.ItemCount != 10 {
		t.Fatalf("merged count = %d, want 10", merged.ItemCount)
	}
}

// TestSplitAndMergePreserveTheIDInvariant runs random sequences: the only
// mechanism that covers combinations a hand-written test would not think to
// make.
func TestSplitAndMergePreserveTheIDInvariant(t *testing.T) {
	const sequences, steps = 2000, 20

	rng := rand.New(rand.NewPCG(1, 2))

	for seq := range sequences {
		// Two stacks of the same item, so a merge is always legal.
		left := ids(t, 1, 1+rng.IntN(64))
		right := ids(t, 2, 1+rng.IntN(64))

		// Every ID minted into this sequence, to check none is ever in two
		// places and none goes missing.
		total := len(left.IDs) + len(right.IDs)

		for range steps {
			if rng.IntN(2) == 0 {
				n := rng.IntN(int(max(left.ItemCount, 1)) + 1)
				taken, rest := left.Split(n)
				left, right = rest, mergeAll(t, right, taken)
			} else {
				n := rng.IntN(int(max(right.ItemCount, 1)) + 1)
				taken, rest := right.Split(n)
				right, left = rest, mergeAll(t, left, taken)
			}

			holds(t, left, "left")
			holds(t, right, "right")

			seen := map[ItemID]bool{}
			for _, s := range []ItemStack{left, right} {
				for _, id := range s.IDs {
					if seen[id] {
						t.Fatalf("sequence %d: %s is in two places", seq, id)
					}
					seen[id] = true
				}
			}
			if len(seen) != total {
				t.Fatalf("sequence %d: %d IDs live, started with %d", seq, len(seen), total)
			}
		}
	}
}

// mergeAll moves everything from src onto dst, which is what a click that
// deposits a whole stack does.
func mergeAll(t *testing.T, dst, src ItemStack) ItemStack {
	t.Helper()

	if src.IsEmpty() {
		return dst
	}
	merged, rest := dst.Merge(src, int(src.ItemCount))
	if !rest.IsEmpty() {
		t.Fatalf("merging a whole stack left %+v behind", rest)
	}

	return merged
}
