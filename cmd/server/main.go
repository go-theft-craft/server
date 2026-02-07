package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
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
	flag.IntVar(&cfg.ViewDistance, "view-distance", cfg.ViewDistance, "entity view distance in chunks")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if cfg.OnlineMode {
		key, err := rsa.GenerateKey(rand.Reader, 1024)
		if err != nil {
			log.Error("generate RSA key", "error", err)
			os.Exit(1)
		}
		cfg.PrivateKey = key
		cfg.PublicKeyDER, err = x509.MarshalPKIXPublicKey(&key.PublicKey)
		if err != nil {
			log.Error("marshal public key", "error", err)
			os.Exit(1)
		}
		log.Info("online mode enabled, RSA keypair generated")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	srv := server.New(cfg, log)
	if err := srv.Start(ctx); err != nil {
		log.Error("server error", "error", err)
		os.Exit(1)
	}
}
