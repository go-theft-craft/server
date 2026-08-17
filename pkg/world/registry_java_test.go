package world

import (
	"testing"

	"github.com/go-theft-craft/minecraft-protocol/data"
	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
	v26_1 "github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"
)

// assertRoundTrips walks every handle a registry minted and asserts that the
// identity it resolves to interns back to the same handle. The counts are not
// asserted: upstream data grows, and a test that pins a count fails on a data
// bump for no reason.
func assertRoundTrips(t *testing.T, set *data.Set) {
	t.Helper()

	reg, err := NewJavaRegistry(set)
	if err != nil {
		t.Fatalf("NewJavaRegistry: %v", err)
	}
	if reg.Len() == 0 {
		t.Fatal("the registry minted no states")
	}

	for s := State(0); int(s) < reg.Len(); s++ {
		name, props, ok := reg.Lookup(s)
		if !ok {
			t.Fatalf("Lookup(%d) reported a minted handle as unknown", s)
		}
		if got := reg.Intern(name, props); got != s {
			t.Fatalf("%s round-tripped to handle %d, want %d", identity(name, props), got, s)
		}
	}

	if _, _, ok := reg.Lookup(reg.Air()); !ok {
		t.Fatal("Air() is not a minted handle")
	}
}

func TestEveryJava18StateRoundTrips(t *testing.T) {
	set, err := v1_8.Data()
	if err != nil {
		t.Fatalf("v1_8.Data: %v", err)
	}
	assertRoundTrips(t, set)
}

func TestEveryJava261StateRoundTrips(t *testing.T) {
	set, err := v26_1.Data()
	if err != nil {
		t.Fatalf("v26_1.Data: %v", err)
	}
	assertRoundTrips(t, set)
}

// TestTheTwoJavaRegistriesAgreeOnNamesAndNotOnHandles is the property the whole
// model rests on: a canonical name means the same block in both versions, and
// the handle standing for it does not survive the crossing.
func TestTheTwoJavaRegistriesAgreeOnNamesAndNotOnHandles(t *testing.T) {
	old, err := v1_8.Data()
	if err != nil {
		t.Fatalf("v1_8.Data: %v", err)
	}
	modern, err := v26_1.Data()
	if err != nil {
		t.Fatalf("v26_1.Data: %v", err)
	}

	oldReg, err := NewJavaRegistry(old)
	if err != nil {
		t.Fatalf("NewJavaRegistry(1.8): %v", err)
	}
	modernReg, err := NewJavaRegistry(modern)
	if err != nil {
		t.Fatalf("NewJavaRegistry(26.1): %v", err)
	}

	for _, name := range []string{"minecraft:stone", "minecraft:dirt", "minecraft:sand"} {
		if _, _, ok := oldReg.Lookup(oldReg.Intern(name, nil)); !ok {
			t.Fatalf("1.8 does not know %s", name)
		}
		if _, _, ok := modernReg.Lookup(modernReg.Intern(name, nil)); !ok {
			t.Fatalf("26.1 does not know %s", name)
		}
	}

	if oldReg.Len() == modernReg.Len() {
		t.Fatal("the two registries minted the same number of states; " +
			"this test no longer distinguishes them")
	}
}
