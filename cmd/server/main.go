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
	"github.com/OCharnyshevich/minecraft-server/internal/server/storage"
)

func main() {
	cfg := config.DefaultConfig()

	var dataDir string
	flag.StringVar(&dataDir, "data-dir", "data", "directory for persistent data")
	flag.IntVar(&cfg.Port, "port", cfg.Port, "server port")
	flag.BoolVar(&cfg.OnlineMode, "online-mode", cfg.OnlineMode, "enable Mojang authentication")
	flag.StringVar(&cfg.MOTD, "motd", cfg.MOTD, "server description")
	flag.IntVar(&cfg.MaxPlayers, "max-players", cfg.MaxPlayers, "maximum players shown in server list")
	flag.IntVar(&cfg.ViewDistance, "view-distance", cfg.ViewDistance, "entity view distance in chunks")
	flag.Int64Var(&cfg.Seed, "seed", cfg.Seed, "world generation seed")
	flag.StringVar(&cfg.GeneratorType, "generator", cfg.GeneratorType, "world generator type (default, flat)")
	flag.IntVar(&cfg.WorldRadius, "world-radius", cfg.WorldRadius, "world radius in chunks (0 = infinite)")
	flag.IntVar(&cfg.AutoSaveMinutes, "auto-save", cfg.AutoSaveMinutes, "auto-save interval in minutes (0 = disabled)")
	flag.IntVar(&cfg.MaxBuildHeight, "max-build-height", cfg.MaxBuildHeight, "maximum Y axis (default 256)")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Create storage manager.
	store, err := storage.New(dataDir, log)
	if err != nil {
		log.Error("create storage", "error", err)
		os.Exit(1)
	}

	// Load config from file, then merge with CLI flags.
	// CLI flags take precedence when explicitly set.
	fileCfg := config.DefaultConfig()
	if err := store.LoadConfig(fileCfg); err != nil {
		log.Error("load config", "error", err)
		os.Exit(1)
	}

	explicitFlags := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) {
		explicitFlags[f.Name] = true
	})
	config.Merge(cfg, fileCfg, explicitFlags)

	// Save effective config back to file.
	if err := store.SaveConfig(cfg); err != nil {
		log.Error("save config", "error", err)
	}

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

	srv := server.New(cfg, log, store)
	if err := srv.Start(ctx); err != nil {
		log.Error("server error", "error", err)
		os.Exit(1)
	}
}
