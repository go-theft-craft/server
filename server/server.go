package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	"github.com/go-theft-craft/minecraft-protocol/data"
	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/server/config"
	"github.com/go-theft-craft/server/internal/server/conn"
	"github.com/go-theft-craft/server/internal/server/player"
	"github.com/go-theft-craft/server/internal/server/storage"
	"github.com/go-theft-craft/server/pkg/world"
	"github.com/go-theft-craft/server/pkg/world/gen"
	"github.com/go-theft-craft/server/pkg/world/v47"
)

// Server is the main Minecraft server that accepts TCP connections.
type Server struct {
	cfg        *config.Config
	log        *slog.Logger
	world      *world.World
	players    *player.Manager
	worldStore WorldStore
	sideStore  SideStore
	gameData   *data.Set
	generator  gen.Generator

	// dispatch delivers samples to the observer the server was built with. It
	// is nil when no observer was supplied, and every sampling path checks
	// that rather than measuring for a no-op.
	dispatch *dispatcher

	// playerStore is the bridge from the public PlayerStore an application
	// supplied to what a connection needs. It is nil when the server runs
	// without player persistence.
	playerStore conn.PlayerStore

	// migrateFrom is the data directory whose legacy JSON world files are
	// folded in at startup, or empty when the application supplied its own
	// stores and no directory to migrate.
	migrateFrom string

	// live tracks the connections that are still open, so a shutdown can
	// tell each of them why it is ending rather than dropping its socket.
	liveMu sync.Mutex
	live   map[*conn.Connection]struct{}
}

// New builds a server from options. It validates everything before doing any
// work, so an invalid port is reported without a socket ever being opened.
//
// It returns an error because the game data now comes from
// minecraft-protocol, which builds its registries rather than handing back a
// package-level value, and a server with no registries cannot serve.
func New(opts ...Option) (*Server, error) {
	b := &builder{
		settings: config.DefaultConfig(),
		log:      slog.New(slog.DiscardHandler),
	}

	for i, opt := range opts {
		if opt == nil {
			return nil, fmt.Errorf("%w: option %d is nil", ErrInvalidOption, i)
		}
		if err := opt(b); err != nil {
			return nil, err
		}
	}

	generator := b.generator
	if generator == nil {
		switch b.settings.GeneratorType {
		case config.GeneratorFlat:
			generator = gen.NewFlatGenerator(b.settings.Seed)
		default:
			generator = gen.NewDefaultGenerator(b.settings.Seed)
		}
	}

	gameData, err := v1_8.Data()
	if err != nil {
		return nil, fmt.Errorf("load java 1.8 game data: %w", err)
	}

	// The registry and the adapter are per server rather than package
	// globals, so two servers in one test binary do not share handles.
	registry, err := world.NewJavaRegistry(gameData)
	if err != nil {
		return nil, fmt.Errorf("build block state registry: %w", err)
	}
	adapter, err := v47.New(registry, gameData)
	if err != nil {
		return nil, fmt.Errorf("build protocol 47 adapter: %w", err)
	}

	dimension := b.dimension
	if dimension.Height == 0 {
		dimension = world.Overworld18()
	}

	w, err := world.NewWorld(dimension, adapter, generator)
	if err != nil {
		return nil, err
	}

	srv := &Server{
		cfg:         b.settings,
		log:         b.log,
		world:       w,
		players:     player.NewManager(b.settings.ViewDistance),
		worldStore:  b.worldStore,
		sideStore:   b.sideStore,
		migrateFrom: b.migrateFrom,
		gameData:    gameData,
		generator:   generator,
	}

	// A store is built by the application, before this function ran, so it
	// learns the world's shape and its state encoding here.
	for _, store := range []any{b.worldStore, b.sideStore, b.playerStore} {
		binder, ok := store.(StoreBinder)
		if !ok {
			continue
		}
		if err := binder.BindWorld(w); err != nil {
			return nil, fmt.Errorf("bind store: %w", err)
		}
	}

	// The world reads a column from the store before generating one.
	if b.worldStore != nil {
		w.SetLoader(storeLoader{store: b.worldStore, name: DefaultWorld})
	}

	if b.playerStore != nil {
		srv.playerStore = playerBridge{store: b.playerStore}
	}

	if b.observer != nil {
		srv.dispatch = newDispatcher(b.observer)
	}

	return srv, nil
}

// WorldStore returns the world persistence the server was built with, or nil
// if it runs without any.
func (s *Server) WorldStore() WorldStore { return s.worldStore }

// storeLoader is the adapter between the world's Loader seam, which knows
// nothing about contexts or world names, and a WorldStore, which needs both.
type storeLoader struct {
	store WorldStore
	name  string
}

func (l storeLoader) LoadChunk(pos world.ChunkPos) (*world.Chunk, error) {
	return l.store.LoadChunk(context.Background(), l.name, pos)
}

// Settings returns a copy of the effective settings. It is a copy because the
// server keeps using its own, and a caller that mutated the returned value
// would change behavior from outside with nothing to say it had.
func (s *Server) Settings() config.Config { return *s.cfg }

// Logger returns the logger the server was built with.
func (s *Server) Logger() *slog.Logger { return s.log }

// Generator returns the world generator the server was built with.
func (s *Server) Generator() gen.Generator { return s.generator }

// World returns the world the server serves. It is the same value a Store is
// handed, so an application that wants to read or write blocks outside a
// connection has one place to get it.
func (s *Server) World() *world.World { return s.world }

// Start begins listening for connections and blocks until the context is cancelled.
func (s *Server) Start(ctx context.Context) error {
	// Load saved world data (time + block overrides).
	savedWorld, err := s.Load(ctx)
	if err != nil {
		return err
	}

	addr := fmt.Sprintf(":%d", s.cfg.Port)
	lc := net.ListenConfig{}

	listener, listenErr := lc.Listen(ctx, "tcp", addr)
	err = listenErr
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	defer listener.Close()

	if s.cfg.WorldRadius > 0 {
		if savedWorld {
			s.log.Info("world already saved, skipping pre-generation")
		} else {
			total := (2*s.cfg.WorldRadius + 1) * (2*s.cfg.WorldRadius + 1)
			s.log.Info("pre-generating world", "radius", s.cfg.WorldRadius, "chunks", total)
			s.world.PreGenerateRadius(s.cfg.WorldRadius)
			s.log.Info("world pre-generation complete")
		}
	}

	s.log.Info(
		"server started",
		"port", s.cfg.Port,
		"onlineMode", s.cfg.OnlineMode,
		"motd", s.cfg.MOTD,
		"generator", s.cfg.GeneratorType,
		"seed", s.cfg.Seed,
	)

	// Start tick loop (20 TPS).
	go s.tickLoop(ctx)

	// Start auto-save goroutine.
	if s.worldStore != nil && s.cfg.AutoSaveMinutes > 0 {
		go s.autoSave(ctx)
	}

	// On cancellation, disconnect everyone with a reason first, then stop
	// accepting. A player sees a kick message instead of a dropped socket.
	go func() {
		<-ctx.Done()
		s.disconnectAll("Server shutting down")
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

		connection, err := conn.NewConnection(ctx, c, s.cfg, s.log, s.world, s.players, s.playerStore, s.gameData, s.streamOptions()...)
		if err != nil {
			s.log.Error("create connection", "error", err, "addr", c.RemoteAddr().String())
			_ = c.Close()

			continue
		}

		connection.SaveAll = s.SaveAll

		s.track(connection)

		go func() {
			defer s.untrack(connection)

			connection.Handle()
		}()
	}
}

// track registers a connection for the duration it is open.
func (s *Server) track(connection *conn.Connection) {
	s.liveMu.Lock()
	defer s.liveMu.Unlock()

	if s.live == nil {
		s.live = make(map[*conn.Connection]struct{})
	}
	s.live[connection] = struct{}{}
}

func (s *Server) untrack(connection *conn.Connection) {
	s.liveMu.Lock()
	defer s.liveMu.Unlock()

	delete(s.live, connection)
}

// disconnectAll tells every open connection why the server is stopping.
func (s *Server) disconnectAll(reason string) {
	s.liveMu.Lock()
	connections := make([]*conn.Connection, 0, len(s.live))
	for connection := range s.live {
		connections = append(connections, connection)
	}
	s.liveMu.Unlock()

	if len(connections) == 0 {
		return
	}

	s.log.Info("disconnecting players", "count", len(connections), "reason", reason)

	var group sync.WaitGroup
	for _, connection := range connections {
		group.Add(1)

		go func() {
			defer group.Done()

			connection.Disconnect(reason)
		}()
	}

	group.Wait()
}

// tickLoop runs the server tick at 20 TPS (50ms interval).
func (s *Server) tickLoop(ctx context.Context) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	// A server with no observer never reads its own resource use: the
	// measurement exists to be reported, and there is nobody to report it to.
	resources := time.NewTicker(resourceSampleInterval)
	defer resources.Stop()

	if !s.observed() {
		resources.Stop()
	}

	var tickCount int

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tickCount++
			s.tick(tickCount)
		case <-resources.C:
			s.SampleResources()
		}
	}
}

// streamOptions are the options every connection's stream is built with. An
// observed server installs the network sink here, so the observation cost is
// paid only by a server that asked for it.
func (s *Server) streamOptions() []protocol.StreamOption {
	if !s.observed() {
		return nil
	}

	return []protocol.StreamOption{protocol.WithObservationSink(NetworkSink(s.dispatch))}
}

// tick advances the world by one tick and broadcasts time every 20 ticks (~1 second).
func (s *Server) tick(tickCount int) {
	s.players.Tick()
	age, timeOfDay := s.world.Tick()

	// Broadcast time update every 20 ticks (once per second).
	if tickCount%20 == 0 {
		s.players.Broadcast(&v1_8.PlayClientboundUpdateTime{
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

// saveAll writes the world and every connected player.
//
// The snapshot is the only part that touches the world, and taking one is a
// map copy of immutable chunk pointers. Everything after it works from that
// copy, so a save cannot see the world half-written and the world does not
// wait for the save.
func (s *Server) saveAll() {
	ctx := context.Background()

	if s.worldStore != nil {
		snap := s.world.Snapshot()
		age, timeOfDay := s.world.GetTime()

		if err := s.worldStore.SaveSnapshot(ctx, DefaultWorld, snap); err != nil {
			s.log.Error("save world failed", "error", err)
		} else {
			s.log.Info("world saved", "chunks", len(snap.Chunks))
		}

		if s.sideStore != nil {
			if err := s.sideStore.SaveSnapshot(ctx, DefaultWorld, snap); err != nil {
				s.log.Error("save sidecar failed", "error", err)
			}
		}

		if err := s.worldStore.SaveLevel(ctx, DefaultWorld, LevelData{
			Age:           age,
			TimeOfDay:     timeOfDay,
			Seed:          s.cfg.Seed,
			GeneratorType: s.cfg.GeneratorType,
		}); err != nil {
			s.log.Error("save level data failed", "error", err)
		}
	}

	if s.playerStore == nil {
		return
	}

	var saved int
	s.players.ForEach(func(p *player.Player) {
		if err := s.playerStore.SavePlayer(p); err != nil {
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

// Load reads world-level state from the store and folds in any legacy JSON
// world files, reporting whether the world had been saved before.
//
// Start calls it. It is exported so an application can load a world without
// opening a socket — a migration tool, or a test.
func (s *Server) Load(ctx context.Context) (bool, error) {
	if s.worldStore == nil {
		return false, nil
	}

	saved := false

	level, found, err := s.worldStore.Level(ctx, DefaultWorld)
	if err != nil {
		s.log.Error("failed to load level data", "error", err)
	}
	if found {
		saved = true
		s.world.SetTime(level.Age, level.TimeOfDay)
		s.log.Info("loaded level data", "age", level.Age, "timeOfDay", level.TimeOfDay)
	}

	// The fold runs after the world can load its regions, so an override lands
	// on top of what the region holds rather than under it. On any world the
	// pre-M11.3 server wrote, the JSON files are the truth and the regions are
	// a stale copy nothing ever read back.
	if s.migrateFrom == "" {
		return saved, nil
	}

	report, err := storage.Migrate(s.migrateFrom, s.world, s.log)
	if err != nil {
		return saved, fmt.Errorf("migrate legacy world files: %w", err)
	}
	if !report.Ran {
		return saved, nil
	}

	if report.HasTime {
		s.world.SetTime(report.Age, report.TimeOfDay)
	}
	// Written through immediately: the fold is only in memory until it is, and
	// the source files have already been renamed.
	s.saveAll()

	return true, nil
}
