package conn

import (
	"sort"
	"testing"

	"github.com/go-theft-craft/server/internal/server/player"
)

func testManager(names ...string) *player.Manager {
	m := player.NewManager(8)
	for _, name := range names {
		sp := &sentPackets{}
		eid := m.AllocateEntityID()
		uuid := [16]byte{byte(eid)}
		p := player.NewPlayer(eid, "uuid-"+name, uuid, name, nil, sp.write)
		p.SetPosition(0, 4, 0, 0, 0, true)
		m.Add(p)
	}
	return m
}

func sorted(ss []string) []string {
	sort.Strings(ss)
	return ss
}

func assertMatches(t *testing.T, got, want []string) {
	t.Helper()
	g := sorted(got)
	w := sorted(want)
	if len(g) != len(w) {
		t.Errorf("got %v, want %v", g, w)
		return
	}
	for i := range g {
		if g[i] != w[i] {
			t.Errorf("got %v, want %v", g, w)
			return
		}
	}
}

func TestCompleteCommandName(t *testing.T) {
	m := testManager("Alice")
	matches := computeCompletions("/t", m)
	assertMatches(t, matches, []string{"/tp", "/time"})
}

func TestCompleteCommandNameFull(t *testing.T) {
	m := testManager("Alice")
	matches := computeCompletions("/he", m)
	assertMatches(t, matches, []string{"/help"})
}

func TestCompleteCommandNameNoMatch(t *testing.T) {
	m := testManager("Alice")
	matches := computeCompletions("/zzz", m)
	if len(matches) != 0 {
		t.Errorf("expected no matches, got %v", matches)
	}
}

func TestCompleteTpPlayerName(t *testing.T) {
	m := testManager("Alice", "Bob", "Alex")
	matches := computeCompletions("/tp Al", m)
	assertMatches(t, matches, []string{"Alice", "Alex"})
}

func TestCompleteTpPlayerNameTrailingSpace(t *testing.T) {
	m := testManager("Alice", "Bob")
	matches := computeCompletions("/tp ", m)
	assertMatches(t, matches, []string{"Alice", "Bob"})
}

func TestCompleteGamemode(t *testing.T) {
	m := testManager("Alice")
	matches := computeCompletions("/gamemode s", m)
	assertMatches(t, matches, []string{"survival", "spectator"})
}

func TestCompleteGamemodeAll(t *testing.T) {
	m := testManager("Alice")
	matches := computeCompletions("/gamemode ", m)
	assertMatches(t, matches, []string{"survival", "creative", "adventure", "spectator"})
}

func TestCompleteTimeSet(t *testing.T) {
	m := testManager("Alice")
	matches := computeCompletions("/time ", m)
	assertMatches(t, matches, []string{"set"})
}

func TestCompleteTimeSetValues(t *testing.T) {
	m := testManager("Alice")
	matches := computeCompletions("/time set ", m)
	assertMatches(t, matches, []string{"day", "night", "noon", "midnight"})
}

func TestCompleteTimeSetPartial(t *testing.T) {
	m := testManager("Alice")
	matches := computeCompletions("/time set n", m)
	assertMatches(t, matches, []string{"night", "noon"})
}

func TestCompleteChatPlayerName(t *testing.T) {
	m := testManager("Alice", "Bob")
	matches := computeCompletions("Al", m)
	assertMatches(t, matches, []string{"Alice"})
}

func TestCompleteSlash(t *testing.T) {
	m := testManager("Alice")
	matches := computeCompletions("/", m)
	// Should return all commands.
	if len(matches) != len(commands) {
		t.Errorf("expected %d matches, got %d", len(commands), len(matches))
	}
}
