package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/OCharnyshevich/minecraft-server/internal/gamedata"
	pc18 "github.com/OCharnyshevich/minecraft-server/internal/gamedata/versions/pc_1_8"
	"github.com/OCharnyshevich/minecraft-server/internal/server/config"
	"github.com/OCharnyshevich/minecraft-server/internal/server/conn"
	"github.com/OCharnyshevich/minecraft-server/internal/server/player"
	"github.com/OCharnyshevich/minecraft-server/internal/server/storage"
	"github.com/OCharnyshevich/minecraft-server/internal/server/world"
	"github.com/OCharnyshevich/minecraft-server/internal/server/world/gen"
)

// Server is the main Minecraft server that accepts TCP connections.
type Server struct {
	cfg      *config.Config
	log      *slog.Logger
	world    *world.World
	players  *player.Manager
	storage  *storage.Storage
	gameData *gamedata.GameData
}

// New creates a new Server with the given config, logger, and storage.
func New(cfg *config.Config, log *slog.Logger, store *storage.Storage) *Server {
	var generator gen.Generator
	switch cfg.GeneratorType {
	case "flat":
		generator = gen.NewFlatGenerator(cfg.Seed)
	default:
		generator = gen.NewDefaultGenerator(cfg.Seed)
	}

	gd := pc18.New()

	return &Server{
		cfg:      cfg,
		log:      log,
		world:    world.NewWorld(generator),
		players:  player.NewManager(cfg.ViewDistance),
		storage:  store,
		gameData: gd,
	}
}

// Start begins listening for connections and blocks until the context is cancelled.
func (s *Server) Start(ctx context.Context) error {
	// Load saved world overrides before pre-generation.
	if s.storage != nil {
		if err := s.storage.LoadWorld(s.world); err != nil {
			s.log.Error("failed to load world data", "error", err)
		}
	}

	addr := fmt.Sprintf(":%d", s.cfg.Port)
	lc := net.ListenConfig{}

	listener, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	defer listener.Close()

	if s.cfg.WorldRadius > 0 {
		total := (2*s.cfg.WorldRadius + 1) * (2*s.cfg.WorldRadius + 1)
		s.log.Info("pre-generating world", "radius", s.cfg.WorldRadius, "chunks", total)
		s.world.PreGenerateRadius(s.cfg.WorldRadius)
		s.log.Info("world pre-generation complete")
	}

	s.log.Info("server started",
		"port", s.cfg.Port,
		"onlineMode", s.cfg.OnlineMode,
		"motd", s.cfg.MOTD,
		"generator", s.cfg.GeneratorType,
		"seed", s.cfg.Seed,
	)

	// Start auto-save goroutine.
	if s.storage != nil {
		go s.autoSave(ctx)
	}

	// Close listener when context is cancelled.
	go func() {
		<-ctx.Done()
		listener.Close()
	}()

	for {
		c, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				s.log.Info("server shutting down")
				s.saveAll()
				return nil
			}
			s.log.Error("accept connection", "error", err)
			continue
		}

		connection := conn.NewConnection(ctx, c, s.cfg, s.log, s.world, s.players, s.storage, s.gameData)
		connection.SaveAll = s.SaveAll
		go connection.Handle()
	}
}

// autoSave periodically saves world and player data.
func (s *Server) autoSave(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.saveAll()
		}
	}
}

// saveAll saves world and all connected player data.
func (s *Server) saveAll() {
	if s.storage == nil {
		return
	}

	if err := s.storage.SaveWorld(s.world); err != nil {
		s.log.Error("auto-save world failed", "error", err)
	} else {
		s.log.Info("world saved", "overrides", s.world.OverrideCount())
	}

	var saved int
	s.players.ForEach(func(p *player.Player) {
		if err := s.storage.SavePlayer(p); err != nil {
			s.log.Error("auto-save player failed", "player", p.Username, "error", err)
		} else {
			saved++
		}
	})
	s.log.Info("players saved", "count", saved)
}

// SaveAll is exposed for the /save command to trigger a manual save.
func (s *Server) SaveAll() {
	s.saveAll()
}
