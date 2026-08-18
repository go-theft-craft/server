package server

import (
	"context"
	"fmt"
	"strings"
)

// The ten commands this server implements, as values.
//
// They left the connection package because a command that needed a connection
// could not be tested without a socket, a stream, a player manager, and a
// world — which is why almost none of them had a test. Every one of these runs
// against a fake Caller.
//
// They are in this package rather than a server/commands/builtin subpackage,
// which is where the plan put them, because a command has to name
// Command and the server has to default to the built-ins: the two
// together are an import cycle. Replacing one is Merge's job — an application
// that wants its own /tp merges BuiltinCommands() first and its own second.

// BuiltinCommands returns the commands a server answers to unless an
// application replaces them.
//
// It panics on a bad signature rather than returning an error, because the
// signatures are literals in this file: a failure here is a mistake in this
// file that Go cannot catch at compile time, not a runtime condition an
// application could handle.
func BuiltinCommands() Set {
	set, err := NewSet(
		help(), list(), tp(), gamemode(), timeCommand(),
		say(), me(), kill(), seed(), save(),
	)
	if err != nil {
		panic("server: built-in commands: " + err.Error())
	}

	return set
}

// noArgs is the signature of a command that takes nothing, spelled out rather
// than left empty so that "takes nothing" and "nobody declared this" are
// different states.
func noArgs() Signature {
	return Signature{Overloads: []Overload{{}}}
}

func help() Command {
	return Command{
		Name:        "help",
		Description: "Show available commands",
		Signature: Signature{Overloads: []Overload{{Params: []Param{
			{Name: "command", Type: ParamCommand, Optional: true},
		}}}},
		Run: func(ctx context.Context, caller Caller, args Args) error {
			svc := ServicesFrom(ctx)
			if svc == nil {
				return fmt.Errorf("help is not available")
			}

			// /help <command> completes command names because the parameter is
			// a word whose candidates come from the set itself, which is what
			// removed the special case the old switch had for it.
			if name := args.String("command"); name != "" {
				cmd, ok := svc.Commands().Lookup(name)
				if !ok {
					caller.Reply(Error(fmt.Sprintf("Unknown command: /%s.", name)))

					return nil
				}
				caller.Reply(Notice(cmd.Usage() + " - " + cmd.Description))

				return nil
			}

			caller.Reply(Notice("--- Available Commands ---"))
			for _, cmd := range svc.Commands().All() {
				if !cmd.Implemented() {
					continue
				}
				caller.Reply(Notice(cmd.Usage() + " - " + cmd.Description))
			}

			return nil
		},
	}
}

func list() Command {
	return Command{
		Name:        "list",
		Description: "Show online players",
		Signature:   noArgs(),
		Run: func(ctx context.Context, caller Caller, _ Args) error {
			svc := ServicesFrom(ctx)
			if svc == nil {
				return fmt.Errorf("the player list is not available")
			}
			names := svc.OnlinePlayers()
			caller.Reply(Success(fmt.Sprintf(
				"Online players (%d): %s", len(names), strings.Join(names, ", "),
			)))

			return nil
		},
	}
}

func tp() Command {
	return Command{
		Name:        "tp",
		Description: "Teleport to a player or coordinates",
		Signature: Signature{Overloads: []Overload{
			{Params: []Param{{Name: "player", Type: ParamPlayer}}},
			{Params: []Param{
				// x is not suggested: this position already offers player
				// names from the overload above, and the caller's own x in
				// that list reads like a player called 10. y and z have no
				// such competition and do suggest.
				{Name: "x", Type: ParamCoordinates, NoSuggest: true},
				{Name: "y", Type: ParamCoordinates},
				{Name: "z", Type: ParamCoordinates},
			}},
		}},
		Run: func(ctx context.Context, caller Caller, args Args) error {
			if args.Overload() == 0 {
				name := args.String("player")
				svc := ServicesFrom(ctx)
				if svc == nil {
					return fmt.Errorf("teleporting to a player is not available")
				}
				pos, ok := svc.PlayerPosition(name)
				if !ok {
					caller.Reply(Error(fmt.Sprintf("Player %q not found.", name)))

					return nil
				}
				caller.Teleport(pos.X, pos.Y, pos.Z)
				caller.Reply(Success(fmt.Sprintf("Teleported to %s.", name)))

				return nil
			}

			x, _ := args.Float("x")
			y, _ := args.Float("y")
			z, _ := args.Float("z")
			caller.Teleport(x, y, z)
			caller.Reply(Success(fmt.Sprintf("Teleported to %.1f, %.1f, %.1f.", x, y, z)))

			return nil
		},
	}
}

func gamemode() Command {
	return Command{
		Name:        "gamemode",
		Description: "Change game mode",
		Signature: Signature{Overloads: []Overload{{Params: []Param{{
			Name: "mode", Type: ParamWord,
			// The long names are what is suggested. The short forms and the
			// numbers still work, because they always did and taking them away
			// is a change to what a player can type; they are resolved by the
			// caller, which is where knowing that "sp" is spectator belongs.
			Choices: []string{"survival", "creative", "adventure", "spectator"},
			Also:    []string{"s", "c", "a", "sp", "0", "1", "2", "3"},
		}}}}},
		Run: func(_ context.Context, caller Caller, args Args) error {
			resolved, ok := caller.SetGameMode(args.String("mode"))
			if !ok {
				caller.Reply(Error("Unknown game mode. Use: survival, creative, adventure, spectator"))

				return nil
			}
			caller.Reply(Success(fmt.Sprintf("Game mode set to %s.", resolved)))

			return nil
		},
	}
}

// timeCommand is not called time, because that is a package this file could
// plausibly want.
func timeCommand() Command {
	return Command{
		Name:        "time",
		Description: "Set world time",
		Signature: Signature{Overloads: []Overload{{Params: []Param{
			{Name: "set", Type: ParamWord, Choices: []string{"set"}},
			{Name: "value", Type: ParamDuration},
		}}}},
		Run: func(ctx context.Context, caller Caller, args Args) error {
			svc := ServicesFrom(ctx)
			if svc == nil {
				return fmt.Errorf("setting the time is not available")
			}
			ticks, ok := DurationTicks(args.String("value"))
			if !ok {
				caller.Reply(Error("Usage: /time set <day|night|noon|midnight|number>"))

				return nil
			}
			svc.SetTimeOfDay(ticks)
			caller.Reply(Success(fmt.Sprintf("Time set to %d.", ticks)))

			return nil
		},
	}
}

func say() Command {
	return Command{
		Name:        "say",
		Description: "Broadcast an announcement",
		Signature: Signature{Overloads: []Overload{{Params: []Param{
			{Name: "message", Type: ParamMessage},
		}}}},
		Run: func(_ context.Context, caller Caller, args Args) error {
			caller.Broadcast(Message{
				Text:  "[Server] " + args.String("message"),
				Color: "light_purple",
			})

			return nil
		},
	}
}

func me() Command {
	return Command{
		Name:        "me",
		Description: "Send an action message",
		Signature: Signature{Overloads: []Overload{{Params: []Param{
			{Name: "action", Type: ParamMessage},
		}}}},
		Run: func(_ context.Context, caller Caller, args Args) error {
			// The one translated component this server sends. The client
			// renders "* Alice waves", which is why Message carries Translate
			// at all.
			caller.Broadcast(Message{
				Translate: "chat.type.emote",
				With:      []string{caller.Name(), args.String("action")},
			})

			return nil
		},
	}
}

func kill() Command {
	return Command{
		Name:        "kill",
		Description: "Kill yourself",
		Signature:   noArgs(),
		Run: func(_ context.Context, caller Caller, _ Args) error {
			caller.Kill()

			return nil
		},
	}
}

func seed() Command {
	return Command{
		Name:        "seed",
		Description: "Show world seed",
		Signature:   noArgs(),
		Run: func(ctx context.Context, caller Caller, _ Args) error {
			svc := ServicesFrom(ctx)
			if svc == nil {
				return fmt.Errorf("the seed is not available")
			}
			caller.Reply(Success(fmt.Sprintf("Seed: [%d]", svc.Seed())))

			return nil
		},
	}
}

func save() Command {
	return Command{
		Name:        "save",
		Description: "Save world and player data",
		Signature:   noArgs(),
		Run: func(ctx context.Context, caller Caller, _ Args) error {
			svc := ServicesFrom(ctx)
			if svc == nil {
				caller.Reply(Error("Save is not available."))

				return nil
			}

			caller.Reply(Success("Saving world and player data..."))
			// Off this goroutine, as it always was: a save walks the resident
			// world and the player who asked for it should not be frozen while
			// it does. The context is deliberately not the dispatch's — that
			// one ends when the command returns.
			go func() {
				if err := svc.Save(context.WithoutCancel(ctx)); err != nil {
					caller.Reply(Error("Save failed: " + err.Error()))

					return
				}
				caller.Reply(Success("Save complete."))
			}()

			return nil
		},
	}
}
