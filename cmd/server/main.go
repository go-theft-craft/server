package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-theft-craft/server/config"
	"github.com/go-theft-craft/server/internal/server/storage"
	"github.com/go-theft-craft/server/server"
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
	flag.IntVar(&cfg.CompressionThreshold, "compression-threshold", cfg.CompressionThreshold, "compress packets at or above this size (-1 disables)")
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

	// The keypair is generated in both modes. The login acceptor is
	// constructed per connection and requires one; generating it here means a
	// login never pays for key generation, and offline mode simply never puts
	// the public half on the wire.
	//
	// 1024 bits is what Java Edition uses for this exchange.
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		log.Error("generate RSA key", "error", err)
		os.Exit(1)
	}

	cfg.PrivateKey = key

	if cfg.OnlineMode {
		log.Info("online mode enabled, RSA keypair generated")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	// Released explicitly rather than deferred: the failure path below exits
	// the process, and a deferred release would not run before it did.
	srv, err := server.New(cfg, log, store)
	if err != nil {
		cancel()
		log.Error("create server", "error", err)
		os.Exit(1)
	}

	err = srv.Start(ctx)
	cancel()

	if err != nil {
		log.Error("server error", "error", err)
		os.Exit(1)
	}
}
