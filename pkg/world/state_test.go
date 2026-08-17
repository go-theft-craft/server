package world

import "testing"

// buildRegistry mints the named identities in order and freezes the result.
func buildRegistry(t *testing.T, names []string) *stateRegistry {
	t.Helper()

	r := newStateRegistry()
	for _, n := range names {
		if _, err := r.define(n, nil, true); err != nil {
			t.Fatalf("define(%q): %v", n, err)
		}
	}
	r.air = r.Intern(airName, nil)
	r.freeze()

	return r
}

func TestInterningIsStableWithinARegistry(t *testing.T) {
	r := buildRegistry(t, []string{"air", "stone", "dirt"})

	first := r.Intern("minecraft:stone", nil)
	second := r.Intern("minecraft:stone", nil)
	if first != second {
		t.Fatalf("interning the same identity twice gave %d then %d", first, second)
	}

	// The unnamespaced name the game data publishes is the same identity.
	if got := r.Intern("stone", nil); got != first {
		t.Fatalf("Intern(%q) = %d, want %d", "stone", got, first)
	}

	name, props, ok := r.Lookup(first)
	if !ok {
		t.Fatalf("Lookup(%d) reported the handle as unknown", first)
	}
	if name != "minecraft:stone" || len(props) != 0 {
		t.Fatalf("Lookup(%d) = %q, %v; want %q with no properties", first, name, props, "minecraft:stone")
	}
}

// TestTwoRegistriesMayDisagreeOnHandleValues exists so nobody writes a handle
// to disk after observing that the values looked stable.
func TestTwoRegistriesMayDisagreeOnHandleValues(t *testing.T) {
	a := buildRegistry(t, []string{"air", "stone", "dirt"})
	b := buildRegistry(t, []string{"air", "dirt", "stone"})

	if a.Intern("stone", nil) == b.Intern("stone", nil) {
		t.Fatal("two registries built in a different order agreed on a handle; " +
			"the test is no longer proving that handles are process-local")
	}
}

func TestLookupOfAnUnknownHandleReports(t *testing.T) {
	r := buildRegistry(t, []string{"air", "stone"})

	if _, _, ok := r.Lookup(State(r.Len())); ok {
		t.Fatal("Lookup of a handle past the end reported ok")
	}
}

func TestAirIsAlwaysPresent(t *testing.T) {
	r := buildRegistry(t, []string{"air", "stone"})

	name, _, ok := r.Lookup(r.Air())
	if !ok || name != airName {
		t.Fatalf("Air() resolves to %q (ok=%v), want %q", name, ok, airName)
	}
}

func TestInterningAnUnknownIdentityPanicsOnAFrozenRegistry(t *testing.T) {
	r := buildRegistry(t, []string{"air", "stone"})

	defer func() {
		if recover() == nil {
			t.Fatal("interning an unknown identity on a frozen registry did not panic")
		}
	}()
	r.Intern("minecraft:no_such_block", nil)
}

func TestPropertiesAreOrderIndependent(t *testing.T) {
	r := newStateRegistry()
	if _, err := r.define("air", nil, true); err != nil {
		t.Fatalf("define air: %v", err)
	}
	want, err := r.define("lever", Properties{{Key: "face", Value: "wall"}, {Key: "powered", Value: "true"}}, true)
	if err != nil {
		t.Fatalf("define lever: %v", err)
	}
	r.air = r.Intern(airName, nil)
	r.freeze()

	got := r.Intern("lever", Properties{{Key: "powered", Value: "true"}, {Key: "face", Value: "wall"}})
	if got != want {
		t.Fatalf("the same properties in a different order gave %d, want %d", got, want)
	}
}
