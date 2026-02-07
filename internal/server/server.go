package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"github.com/OCharnyshevich/minecraft-server/internal/server/config"
	"github.com/OCharnyshevich/minecraft-server/internal/server/conn"
	"github.com/OCharnyshevich/minecraft-server/internal/server/player"
	"github.com/OCharnyshevich/minecraft-server/internal/server/world"
	"github.com/OCharnyshevich/minecraft-server/internal/server/world/gen"
)

// Server is the main Minecraft server that accepts TCP connections.
type Server struct {
	cfg     *config.Config
	log     *slog.Logger
	world   *world.World
	players *player.Manager
}

// New creates a new Server with the given config and logger.
func New(cfg *config.Config, log *slog.Logger) *Server {
	var generator gen.Generator
	switch cfg.GeneratorType {
	case "flat":
		generator = gen.NewFlatGenerator(cfg.Seed)
	default:
		generator = gen.NewDefaultGenerator(cfg.Seed)
	}

	return &Server{
		cfg:     cfg,
		log:     log,
		world:   world.NewWorld(generator),
		players: player.NewManager(cfg.ViewDistance),
	}
}

// Start begins listening for connections and blocks until the context is cancelled.
func (s *Server) Start(ctx context.Context) error {
	addr := fmt.Sprintf(":%d", s.cfg.Port)
	lc := net.ListenConfig{}

	listener, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	defer listener.Close()

	s.log.Info("server started",
		"port", s.cfg.Port,
		"onlineMode", s.cfg.OnlineMode,
		"motd", s.cfg.MOTD,
		"generator", s.cfg.GeneratorType,
		"seed", s.cfg.Seed,
	)

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
				return nil
			}
			s.log.Error("accept connection", "error", err)
			continue
		}

		connection := conn.NewConnection(ctx, c, s.cfg, s.log, s.world, s.players)
		go connection.Handle()
	}
}
