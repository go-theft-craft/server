// Command custom-command runs a server with a command this program defines.
//
// It exists to prove the extension point from outside the framework's module,
// the way only a separate module can: `examples/` has its own go.mod and a
// replace for the parent, so what this file can reach is exactly what any
// consumer can reach. If a command can be written here, it can be written
// anywhere.
//
// It also shows the composition that makes the stub list useful rather than in
// the way: vanilla.Stubs() first, so every 1.8 name resolves and says it is not
// implemented; the built-ins second, so the ones this server does implement
// win; and this program's own command last, so it can replace either.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-theft-craft/server/config"
	"github.com/go-theft-craft/server/server"
	"github.com/go-theft-craft/server/server/commands/vanilla"
)

func main() {
	cfg := config.DefaultConfig()

	flag.IntVar(&cfg.Port, "port", cfg.Port, "server port")
	flag.StringVar(&cfg.GeneratorType, "generator", config.GeneratorFlat, "world generator type")
	flag.IntVar(&cfg.WorldRadius, "world-radius", cfg.WorldRadius, "world radius in chunks (0 = infinite)")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	mine, err := server.NewSet(ping(), missing())
	if err != nil {
		log.Error("build commands", "error", err)
		os.Exit(1)
	}

	// Order is the whole point. A later set replaces an earlier one for any
	// name both answer to, so the stubs never shadow anything real.
	commands := server.Merge(vanilla.Stubs(), server.BuiltinCommands(), mine)
	log.Info("commands registered",
		"total", len(commands.All()),
		"vanilla", vanilla.Describe(commands))

	srv, err := server.New(
		server.WithSettings(cfg),
		server.WithLogger(log),
		server.WithCommands(commands),
	)
	if err != nil {
		log.Error("create server", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := srv.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

// ping is a command written entirely outside the framework's module.
//
// Everything it needs is exported: the signature types, the Caller, the Args,
// and the Services on the context. It never sees a connection.
func ping() server.Command {
	return server.Command{
		Name:        "ping",
		Aliases:     []string{"pong"},
		Description: "Reply with who you are and where you are standing",
		Signature: server.Signature{Overloads: []server.Overload{
			{},
			{Params: []server.Param{{Name: "player", Type: server.ParamPlayer}}},
		}},
		Run: func(ctx context.Context, caller server.Caller, args server.Args) error {
			if args.Overload() == 0 {
				pos := caller.Position()
				caller.Reply(server.Success(fmt.Sprintf(
					"pong, %s — you are at %.0f, %.0f, %.0f", caller.Name(), pos.X, pos.Y, pos.Z,
				)))

				return nil
			}

			name := args.String("player")
			svc := server.ServicesFrom(ctx)
			if svc == nil {
				return errors.New("the player list is not available")
			}
			pos, online := svc.PlayerPosition(name)
			if !online {
				caller.Reply(server.Error(fmt.Sprintf("%s is not online.", name)))

				return nil
			}
			caller.Reply(server.Success(fmt.Sprintf(
				"%s is at %.0f, %.0f, %.0f", name, pos.X, pos.Y, pos.Z,
			)))

			return nil
		},
	}
}

// missing reports what this server still owes somebody expecting a vanilla one.
//
// It is the question vanilla.Stubs() exists to answer, asked from inside the
// game rather than from a build log.
func missing() server.Command {
	return server.Command{
		Name:        "missing",
		Description: "List the vanilla commands this server has not implemented",
		Signature:   server.Signature{Overloads: []server.Overload{{}}},
		Run: func(ctx context.Context, caller server.Caller, _ server.Args) error {
			svc := server.ServicesFrom(ctx)
			if svc == nil {
				return errors.New("the command set is not available")
			}
			absent := vanilla.Missing(svc.Commands())

			caller.Reply(server.Notice(vanilla.Describe(svc.Commands())))
			for _, name := range absent {
				caller.Reply(server.Notice("  /" + name))
			}

			return nil
		},
	}
}
