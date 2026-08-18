package server

import (
	"context"
	"errors"
	"fmt"
	"strings"

	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/server/internal/server/player"
)

// Dispatch and completion.
//
// Both read the same Signature, which is the claim this milestone makes: one
// declaration drives execution and suggestion, so they cannot disagree. Before
// this, execution parsed words in each handler and completion knew the argument
// shapes in a switch statement of its own, and nothing in the build would have
// noticed the two drifting apart.

// servicesKey is the context key Services travels under.
type servicesKey struct{}

// Services is what a command does to the server rather than to its caller.
//
// It is an interface rather than a struct of function values because /save's
// implementation changes with the storage seam and the commands must not: it
// was SaveAll before M11.3 and three stores after it, and neither the interface
// nor any command noticed.
type Services interface {
	// Seed is the world's seed.
	Seed() int64
	// OnlinePlayers is every connected player's name.
	OnlinePlayers() []string
	// PlayerPosition is where a named player is, if they are online.
	PlayerPosition(name string) (Position, bool)
	// SetTimeOfDay sets the world's time and tells every client.
	SetTimeOfDay(ticks int64)
	// Save writes the world and the players.
	Save(ctx context.Context) error
	// Commands is the set this server dispatches, which /help reads.
	Commands() Set
}

// WithServices puts services on a context for a command to find.
func WithServices(ctx context.Context, s Services) context.Context {
	return context.WithValue(ctx, servicesKey{}, s)
}

// ServicesFrom is the services a command was dispatched with, or nil.
//
// A command that needs them and finds none should say so rather than panic: a
// command running against a fake caller in a test is exactly that case, and it
// is a case worth supporting.
func ServicesFrom(ctx context.Context) Services {
	s, _ := ctx.Value(servicesKey{}).(Services)

	return s
}

// Dispatch runs one command line and reports whether it was one.
//
// A line that does not start with "/" is not a command and comes back false, so
// the chat path can pass everything through here and act on the answer.
func (s *Server) Dispatch(ctx context.Context, caller Caller, line string) bool {
	if !strings.HasPrefix(line, "/") {
		return false
	}

	name, words := splitLine(line)
	if name == "" {
		return true
	}

	set := s.Commands()
	cmd, known := set.Lookup(name)
	// Unknown and unimplemented are different answers. Unknown means a typo;
	// unimplemented means a to-do, and a server builder needs to tell them
	// apart — so does a player, who should stop retyping a command that exists.
	if !known || !s.authorize(caller, cmd) {
		caller.Reply(Error(fmt.Sprintf("Unknown command: /%s. Type /help for a list of commands.", name)))

		return true
	}
	if !cmd.Implemented() {
		caller.Reply(Error(fmt.Sprintf("/%s is not implemented by this server.", cmd.Name)))

		return true
	}

	args, err := parse(cmd, words)
	if err != nil {
		caller.Reply(parseMessage(err))

		return true
	}

	if err := cmd.Run(WithServices(ctx, s.servicesFor(ctx)), caller, args); err != nil {
		if errors.Is(err, ErrNotImplemented) {
			caller.Reply(Error(fmt.Sprintf("/%s is not implemented by this server.", cmd.Name)))

			return true
		}
		caller.Reply(Error(err.Error()))
	}

	return true
}

func parseMessage(err error) Message {
	var parseErr *ParseError
	if errors.As(err, &parseErr) {
		return parseErr.Message()
	}

	return Error(err.Error())
}

// Commands is the command set this server dispatches.
//
// A server built without WithCommands gets the built-ins, because a server that
// silently answered nothing would look broken rather than configured.
func (s *Server) Commands() Set { return s.commands }

// authorize asks the server's authorizer whether this caller may run this
// command.
func (s *Server) authorize(caller Caller, cmd *Command) bool {
	if s.authorizer == nil {
		return true
	}

	return s.authorizer(caller, cmd)
}

// serverServices is the Services implementation backed by a running server.
type serverServices struct{ srv *Server }

var _ Services = serverServices{}

func (s serverServices) Seed() int64 { return s.srv.cfg.Seed }

func (s serverServices) OnlinePlayers() []string {
	var names []string
	s.srv.players.ForEach(func(p *player.Player) { names = append(names, p.Username) })

	return names
}

func (s serverServices) PlayerPosition(name string) (Position, bool) {
	p := s.srv.players.GetByName(name)
	if p == nil {
		return Position{}, false
	}
	pos := p.GetPosition()

	return Position{X: pos.X, Y: pos.Y, Z: pos.Z, Yaw: pos.Yaw, Pitch: pos.Pitch}, true
}

func (s serverServices) SetTimeOfDay(ticks int64) {
	s.srv.world.SetTimeOfDay(ticks)
	age, _ := s.srv.world.GetTime()
	s.srv.players.Broadcast(&v1_8.PlayClientboundUpdateTime{Age: age, Time: ticks})
}

// Save is what /save calls. Its implementation is the three stores today and
// was one before M11.3, and neither this interface nor any command noticed —
// which is why it is an interface and not a function value on the caller.
func (s serverServices) Save(context.Context) error {
	s.srv.SaveAll()

	return nil
}

func (s serverServices) Commands() Set { return s.srv.commands }

func (s *Server) services() Services { return serverServices{srv: s} }

// servicesFor is what a command will see: whatever the caller already put on
// the context, or this server.
//
// The context wins so that something dispatching on a server's behalf — a
// test with a fake, a proxy with its own view of who is online — can say what
// a command sees without having to build a server that agrees.
func (s *Server) servicesFor(ctx context.Context) Services {
	if svc := ServicesFrom(ctx); svc != nil {
		return svc
	}

	return s.services()
}
