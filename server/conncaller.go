package server

import (
	"context"

	"github.com/go-theft-craft/server/internal/server/conn"
)

// The bridge between a connection and a command.
//
// It lives here because this is the only package that sees both: Caller and
// Message are public types of this package, and internal/server/conn cannot
// import it. The connection exposes the handful of things a command does to a
// player — reply, broadcast, teleport, change mode, die — and this turns them
// into the interface a command is written against.

// connCaller is a player, as a command sees them.
type connCaller struct{ c *conn.Connection }

var _ Caller = connCaller{}

func (p connCaller) Name() string { return p.c.Username() }
func (p connCaller) UUID() string { return p.c.PlayerUUID() }

func (p connCaller) Position() Position {
	x, y, z, yaw, pitch := p.c.PlayerPosition()

	return Position{X: x, Y: y, Z: z, Yaw: yaw, Pitch: pitch}
}

// Permission is the level a player runs at.
//
// Everyone is level 0. This server has no operator list, and inventing one here
// would be a framework milestone silently locking people out of their own
// worlds; an application that has one supplies an Authorizer.
func (p connCaller) Permission() PermissionLevel { return PermissionEveryone }

func (p connCaller) Reply(m Message) {
	if m.Translate != "" {
		p.c.SendTranslated(m.Translate, m.With)

		return
	}
	p.c.SendMessage(m.Text, m.Color)
}

func (p connCaller) Broadcast(m Message) {
	if m.Translate != "" {
		p.c.BroadcastTranslated(m.Translate, m.With)

		return
	}
	p.c.BroadcastMessage(m.Text, m.Color)
}

func (p connCaller) Teleport(x, y, z float64) { p.c.TeleportSelf(x, y, z) }

func (p connCaller) SetGameMode(name string) (string, bool) { return p.c.SetGameModeByName(name) }

func (p connCaller) Kill() { p.c.KillPlayer() }

// dispatchFor is the seam a connection calls when a chat line starts with "/".
func (s *Server) dispatchFor(c *conn.Connection, line string) bool {
	return s.Dispatch(context.Background(), connCaller{c: c}, line)
}

// completeFor is the seam a connection calls for a tab-complete.
func (s *Server) completeFor(c *conn.Connection, line string) []string {
	return s.Complete(context.Background(), connCaller{c: c}, line)
}
