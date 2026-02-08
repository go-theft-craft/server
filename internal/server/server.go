package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/go-theft-craft/server/pkg/gamedata"
	pkt "github.com/go-theft-craft/server/pkg/gamedata/versions/pc_1_8"
	"github.com/go-theft-craft/server/internal/server/config"
	"github.com/go-theft-craft/server/internal/server/conn"
	"github.com/go-theft-craft/server/internal/server/player"
	"github.com/go-theft-craft/server/internal/server/storage"
	"github.com/go-theft-craft/server/internal/server/world"
	"github.com/go-theft-craft/server/internal/server/world/gen"
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
	case config.GeneratorFlat:
		generator = gen.NewFlatGenerator(cfg.Seed)
	default:
		generator = gen.NewDefaultGenerator(cfg.Seed)
	}

	gd := pkt.New()

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
	// Load saved world data (time + block overrides).
	if s.storage != nil {
		if err := s.storage.LoadWorld(s.world); err != nil {
			s.log.Error("failed to load world data", "error", err)
		}
		if err := s.storage.LoadBlockOverrides(s.world); err != nil {
			s.log.Error("failed to load block overrides", "error", err)
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
		if s.storage != nil && s.storage.HasSavedWorld() {
			s.log.Info("world already saved, skipping pre-generation")
		} else {
			total := (2*s.cfg.WorldRadius + 1) * (2*s.cfg.WorldRadius + 1)
			s.log.Info("pre-generating world", "radius", s.cfg.WorldRadius, "chunks", total)
			s.world.PreGenerateRadius(s.cfg.WorldRadius)
			s.log.Info("world pre-generation complete")
		}
	}

	s.log.Info("server started",
		"port", s.cfg.Port,
		"onlineMode", s.cfg.OnlineMode,
		"motd", s.cfg.MOTD,
		"generator", s.cfg.GeneratorType,
		"seed", s.cfg.Seed,
	)

	// Start tick loop (20 TPS).
	go s.tickLoop(ctx)

	// Start auto-save goroutine.
	if s.storage != nil && s.cfg.AutoSaveMinutes > 0 {
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

// tickLoop runs the server tick at 20 TPS (50ms interval).
func (s *Server) tickLoop(ctx context.Context) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	var tickCount int

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tickCount++
			s.tick(tickCount)
		}
	}
}

// tick advances the world by one tick and broadcasts time every 20 ticks (~1 second).
func (s *Server) tick(tickCount int) {
	s.players.Tick()
	age, timeOfDay := s.world.Tick()

	// Broadcast time update every 20 ticks (once per second).
	if tickCount%20 == 0 {
		s.players.Broadcast(&pkt.UpdateTime{
			Age:  age,
			Time: timeOfDay,
		})
	}
}

// autoSave periodically saves world and player data.
func (s *Server) autoSave(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(s.cfg.AutoSaveMinutes) * time.Minute)
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
		s.log.Info("world saved")
	}

	if err := s.storage.SaveBlockOverrides(s.world); err != nil {
		s.log.Error("auto-save block overrides failed", "error", err)
	} else {
		s.log.Info("block overrides saved")
	}

	if err := s.storage.SaveWorldAnvil(s.world); err != nil {
		s.log.Error("auto-save anvil failed", "error", err)
	} else {
		s.log.Info("anvil region files saved")
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
