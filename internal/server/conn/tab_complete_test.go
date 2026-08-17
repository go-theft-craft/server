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
	matches := computeCompletions("/t", m, player.Position{})
	assertMatches(t, matches, []string{"/tp", "/time"})
}

func TestCompleteCommandNameFull(t *testing.T) {
	m := testManager("Alice")
	matches := computeCompletions("/he", m, player.Position{})
	assertMatches(t, matches, []string{"/help"})
}

func TestCompleteCommandNameNoMatch(t *testing.T) {
	m := testManager("Alice")
	matches := computeCompletions("/zzz", m, player.Position{})
	if len(matches) != 0 {
		t.Errorf("expected no matches, got %v", matches)
	}
}

func TestCompleteTpPlayerName(t *testing.T) {
	m := testManager("Alice", "Bob", "Alex")
	matches := computeCompletions("/tp Al", m, player.Position{})
	assertMatches(t, matches, []string{"Alice", "Alex"})
}

func TestCompleteTpPlayerNameTrailingSpace(t *testing.T) {
	m := testManager("Alice", "Bob")
	matches := computeCompletions("/tp ", m, player.Position{})
	assertMatches(t, matches, []string{"Alice", "Bob"})
}

func TestCompleteGamemode(t *testing.T) {
	m := testManager("Alice")
	matches := computeCompletions("/gamemode s", m, player.Position{})
	assertMatches(t, matches, []string{"survival", "spectator"})
}

func TestCompleteGamemodeAll(t *testing.T) {
	m := testManager("Alice")
	matches := computeCompletions("/gamemode ", m, player.Position{})
	assertMatches(t, matches, []string{"survival", "creative", "adventure", "spectator"})
}

func TestCompleteTimeSet(t *testing.T) {
	m := testManager("Alice")
	matches := computeCompletions("/time ", m, player.Position{})
	assertMatches(t, matches, []string{"set"})
}

func TestCompleteTimeSetValues(t *testing.T) {
	m := testManager("Alice")
	matches := computeCompletions("/time set ", m, player.Position{})
	assertMatches(t, matches, []string{"day", "night", "noon", "midnight"})
}

func TestCompleteTimeSetPartial(t *testing.T) {
	m := testManager("Alice")
	matches := computeCompletions("/time set n", m, player.Position{})
	assertMatches(t, matches, []string{"night", "noon"})
}

func TestCompleteChatPlayerName(t *testing.T) {
	m := testManager("Alice", "Bob")
	matches := computeCompletions("Al", m, player.Position{})
	assertMatches(t, matches, []string{"Alice"})
}

func TestCompleteSlash(t *testing.T) {
	m := testManager("Alice")
	matches := computeCompletions("/", m, player.Position{})
	// Should return all commands.
	if len(matches) != len(commands) {
		t.Errorf("expected %d matches, got %d", len(commands), len(matches))
	}
}

// The suggestion list the 1.8 client draws above the chat box only appears
// when the server returns more than one candidate, so an argument position
// that returns nothing shows no suggestions at all. These cover the positions
// that used to return nothing.

func TestCompleteTpCoordinates(t *testing.T) {
	m := testManager("Alice")
	pos := player.Position{X: 12.7, Y: 64.2, Z: -3.4}

	assertMatches(t, computeCompletions("/tp 12 ", m, pos), []string{"64"})
	assertMatches(t, computeCompletions("/tp 12 64 ", m, pos), []string{"-4"})
}

// A coordinate that cannot extend what the player typed is not offered: the
// client would otherwise replace their input with an unrelated number.
func TestCompleteTpCoordinateMustExtendTheInput(t *testing.T) {
	m := testManager("Alice")
	pos := player.Position{X: 12.7, Y: 64.2, Z: -3.4}

	if got := computeCompletions("/tp 12 9", m, pos); len(got) != 0 {
		t.Errorf("got %v, want no match — 64 does not start with 9", got)
	}
}

func TestCompleteHelpTakesACommandName(t *testing.T) {
	m := testManager("Alice")

	assertMatches(t, computeCompletions("/help t", m, player.Position{}), []string{"tp", "time"})
}

func TestCompleteSaveHasNoArguments(t *testing.T) {
	m := testManager("Alice")

	if got := computeCompletions("/save ", m, player.Position{}); len(got) != 0 {
		t.Errorf("got %v, want no match — /save takes no arguments", got)
	}
}
