package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/OCharnyshevich/minecraft-server/internal/server"
	"github.com/OCharnyshevich/minecraft-server/internal/server/config"
)

func main() {
	cfg := config.DefaultConfig()

	flag.IntVar(&cfg.Port, "port", cfg.Port, "server port")
	flag.BoolVar(&cfg.OnlineMode, "online-mode", cfg.OnlineMode, "enable Mojang authentication")
	flag.StringVar(&cfg.MOTD, "motd", cfg.MOTD, "server description")
	flag.IntVar(&cfg.MaxPlayers, "max-players", cfg.MaxPlayers, "maximum players shown in server list")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	srv := server.New(cfg, log)
	if err := srv.Start(ctx); err != nil {
		log.Error("server error", "error", err)
		os.Exit(1)
	}
}
