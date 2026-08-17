// Command flat runs a superflat, in-memory server with a generator supplied
// directly rather than selected by name in settings.
//
// This is the example that shows a seam being replaced: nothing here asks the
// framework to pick a generator, and a custom generator would be substituted
// exactly the same way.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-theft-craft/server/pkg/world/gen"
	"github.com/go-theft-craft/server/server"
)

const seed = 1

func main() {
	port := flag.Int("port", 25565, "server port")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	srv, err := server.New(
		server.WithLogger(log),
		server.WithPort(*port),
		server.WithGenerator(gen.NewFlatGenerator(seed)),
		server.WithMOTD("flat example"),
		server.WithWorldRadius(8),
	)
	if err != nil {
		log.Error("create server", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := srv.Start(ctx); err != nil {
		log.Error("server error", "error", err)
		os.Exit(1)
	}
}
