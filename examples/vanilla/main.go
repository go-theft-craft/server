// Command vanilla runs the server the way this repository ships it: every
// flag, a configuration file under the data directory, file-backed
// persistence, and a generated RSA keypair.
//
// It is the example that composes everything, which is why the other two
// exist: what this one leaves in, they take out.
package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/go-theft-craft/server/config"
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

	store, err := server.FileStore(dataDir, log)
	if err != nil {
		log.Error("create store", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	// Load config from file, then merge with CLI flags.
	// CLI flags take precedence when explicitly set.
	//
	// The file is read here rather than by the store, because which settings
	// an application takes from disk is the application's business: the
	// framework is handed the result.
	fileCfg := config.DefaultConfig()
	if err := loadConfig(dataDir, fileCfg); err != nil {
		log.Error("load config", "error", err)
		os.Exit(1)
	}

	explicitFlags := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) {
		explicitFlags[f.Name] = true
	})
	config.Merge(cfg, fileCfg, explicitFlags)

	// Save effective config back to file.
	if err := saveConfig(dataDir, cfg); err != nil {
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

	if cfg.OnlineMode {
		log.Info("online mode enabled, RSA keypair generated")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	// Released explicitly rather than deferred: the failure path below exits
	// the process, and a deferred release would not run before it did.
	srv, err := server.New(
		server.WithSettings(cfg),
		server.WithLogger(log),
		server.WithWorldStore(store.World()),
		server.WithSideStore(store.Side()),
		server.WithPlayerStore(store.Players()),
		server.WithPrivateKey(key),
	)
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

// configPath is where this example keeps its settings, the same file the
// file store's own directory layout uses.
func configPath(dataDir string) string { return filepath.Join(dataDir, "config.json") }

// loadConfig reads config.json into cfg. A missing file leaves cfg alone,
// because a first run has nothing to read and is not an error.
func loadConfig(dataDir string, cfg *config.Config) error {
	data, err := os.ReadFile(configPath(dataDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}

		return err
	}

	return json.Unmarshal(data, cfg)
}

// saveConfig writes the effective settings back, so the file records what the
// server actually ran with rather than what was last edited by hand.
//
// It writes to a temporary file and renames, so an interrupted run leaves the
// previous settings intact rather than a half-written file.
func saveConfig(dataDir string, cfg *config.Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	path := configPath(dataDir)
	tmp := path + ".tmp"

	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)

		return err
	}

	return nil
}
