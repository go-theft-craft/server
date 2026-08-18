package conn

import (
	"testing"

	"github.com/go-theft-craft/server/internal/server/player"
)

// The pinned completion table.
//
// Every case here was written by running the implementation that exists today,
// before any of it moves. That is the point: the `switch cmdName` block in
// tab_complete.go is the only record of what this server suggests, and writing
// the table after the rewrite would be writing down whatever the rewrite
// happens to do rather than what it was supposed to preserve.
//
// A suggestion that changes is a change to what a player sees. If a case here
// starts failing, the commit that changes it has to say why.
//
// The fixture is fixed: two players named Alice and Bob, and a caller standing
// at 10.7, 65.2, -3.4 — so the floor of each axis is 10, 65, and -4, and the
// negative one is deliberate.
type completionCase struct {
	input string
	want  []string
}

var completionTable = []completionCase{
	{input: "/", want: []string{"/gamemode", "/help", "/kill", "/list", "/me", "/save", "/say", "/seed", "/time", "/tp"}},
	{input: "/g", want: []string{"/gamemode"}},
	{input: "/h", want: []string{"/help"}},
	{input: "/k", want: []string{"/kill"}},
	{input: "/l", want: []string{"/list"}},
	{input: "/m", want: []string{"/me"}},
	{input: "/s", want: []string{"/save", "/say", "/seed"}},
	{input: "/t", want: []string{"/time", "/tp"}},
	{input: "/help", want: []string{"/help"}},
	{input: "/list", want: []string{"/list"}},
	{input: "/tp", want: []string{"/tp"}},
	{input: "/gamemode", want: []string{"/gamemode"}},
	{input: "/time", want: []string{"/time"}},
	{input: "/say", want: []string{"/say"}},
	{input: "/me", want: []string{"/me"}},
	{input: "/kill", want: []string{"/kill"}},
	{input: "/seed", want: []string{"/seed"}},
	{input: "/save", want: []string{"/save"}},
	{input: "/zzz", want: nil},
	{input: "/tp ", want: []string{"Alice", "Bob"}},
	{input: "/help ", want: []string{"gamemode", "help", "kill", "list", "me", "save", "say", "seed", "time", "tp"}},
	{input: "/help A", want: nil},
	{input: "/help x ", want: nil},
	{input: "/help x y", want: nil},
	{input: "/list ", want: nil},
	{input: "/list A", want: nil},
	{input: "/list x ", want: nil},
	{input: "/list x y", want: nil},
	{input: "/tp A", want: []string{"Alice"}},
	{input: "/tp x ", want: []string{"65"}},
	{input: "/tp x y", want: nil},
	{input: "/gamemode ", want: []string{"adventure", "creative", "spectator", "survival"}},
	{input: "/gamemode A", want: []string{"adventure"}},
	{input: "/gamemode x ", want: nil},
	{input: "/gamemode x y", want: nil},
	{input: "/time ", want: []string{"set"}},
	{input: "/time A", want: nil},
	{input: "/time x ", want: []string{"day", "midnight", "night", "noon"}},
	{input: "/time x y", want: nil},
	{input: "/say ", want: []string{"Alice", "Bob"}},
	{input: "/say A", want: []string{"Alice"}},
	{input: "/say x ", want: []string{"Alice", "Bob"}},
	{input: "/say x y", want: nil},
	{input: "/me ", want: []string{"Alice", "Bob"}},
	{input: "/me A", want: []string{"Alice"}},
	{input: "/me x ", want: []string{"Alice", "Bob"}},
	{input: "/me x y", want: nil},
	{input: "/kill ", want: nil},
	{input: "/kill A", want: nil},
	{input: "/kill x ", want: nil},
	{input: "/kill x y", want: nil},
	{input: "/seed ", want: nil},
	{input: "/seed A", want: nil},
	{input: "/seed x ", want: nil},
	{input: "/seed x y", want: nil},
	{input: "/save ", want: nil},
	{input: "/save A", want: nil},
	{input: "/save x ", want: nil},
	{input: "/save x y", want: nil},
	{input: "/tp Al", want: []string{"Alice"}},
	{input: "/tp Alice ", want: []string{"65"}},
	{input: "/tp 1 ", want: []string{"65"}},
	{input: "/tp 1 2 ", want: []string{"-4"}},
	{input: "/tp 1 2 3 ", want: nil},
	{input: "/tp 1 6", want: []string{"65"}},
	{input: "/tp 1 9", want: nil},
	{input: "/time set ", want: []string{"day", "midnight", "night", "noon"}},
	{input: "/time set d", want: []string{"day"}},
	{input: "/time set n", want: []string{"night", "noon"}},
	{input: "/time set zzz", want: nil},
	{input: "/gamemode s", want: []string{"spectator", "survival"}},
	{input: "/gamemode cr", want: []string{"creative"}},
	{input: "/gamemode zzz", want: nil},
	{input: "/help t", want: []string{"time", "tp"}},
	{input: "/help zzz", want: nil},
	{input: "Al", want: []string{"Alice"}},
	{input: "hello Al", want: []string{"Alice"}},
	{input: "", want: []string{"Alice", "Bob"}},
	{input: "hello ", want: []string{"Alice", "Bob"}},
}

func TestCompletionTable(t *testing.T) {
	m := testManager("Alice", "Bob")
	pos := player.Position{X: 10.7, Y: 65.2, Z: -3.4}

	for _, tc := range completionTable {
		t.Run(tc.input, func(t *testing.T) {
			assertMatches(t, computeCompletions(tc.input, m, pos), tc.want)
		})
	}
}
