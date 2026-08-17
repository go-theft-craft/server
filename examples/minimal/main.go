// Command minimal runs the smallest server this framework can build: it
// accepts logins into a generated world and persists nothing.
//
// Everything it does not do is the point. There is no storage, no
// configuration file, and no observer, and each of those is one option away.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-theft-craft/server/server"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	srv, err := server.New(
		server.WithLogger(log),
		server.WithMOTD("minimal example"),
		server.WithWorldRadius(4),
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
