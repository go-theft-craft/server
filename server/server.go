package server

import (
	"context"
	"encoding/json"
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
	registry   gen.Registry
	genName    string
	genVersion int
	genParams  json.RawMessage

	// level is the world's metadata as it stood when New read it, and
	// hasLevel says whether there was any. Start writes the generator record
	// into a world that had none, which is the migration path for every world
	// that exists today.
	level    LevelData
	hasLevel bool

	// recorder writes provenance records off the tick, and index is the
	// write path for item identity. Both are nil unless an application asked
	// for them: everything in M11.5 is off by default.
	recorder *Recorder
	index    ItemIndex

	// blocks is the sparse table of identified blocks, and reconcile is the
	// pass that squares stored identity with what is actually in the world.
	// Both are nil unless item identity is on, and every method on them
	// tolerates that.
	blocks    *storage.BlockIdentity
	reconcile *storage.Reconciler

	// reconciled accumulates what every chunk load reconciled, so the counts
	// can be reported as one line rather than one per column.
	reconciled reconcileCounts

	// minter hands out item IDs for this run. Its epoch is the world's stored
	// one plus one, and the advance is written back at the first save.
	minter *Minter

	worldStore WorldStore
	sideStore  SideStore
	gameData   *data.Set
	generator  gen.Generator

	// dispatch delivers samples to the observer the server was built with. It
	// is nil when no observer was supplied, and every sampling path checks
	// that rather than measuring for a no-op.
	dispatch *dispatcher

	// chunkDetail labels chunk samples with exact coordinates instead of the
	// region they fall in. See WithChunkDetail for what it costs.
	chunkDetail bool

	// ticks accumulates the events that happen too often to sample one at a
	// time. See tickstats.go.
	ticks *tickStats

	// commands is what this server answers to, and authorizer decides who may
	// run each one. A nil authorizer grants everything, which is what this
	// server did before it had one.
	commands   Set
	authorizer Authorizer

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

	gameData, err := v1_8.Data()
	if err != nil {
		return nil, fmt.Errorf("load java 1.8 game data: %w", err)
	}

	// The state registry and the adapter are per server rather than package
	// globals, so two servers in one test binary do not share handles.
	states, err := world.NewJavaRegistry(gameData)
	if err != nil {
		return nil, fmt.Errorf("build block state registry: %w", err)
	}
	adapter, err := v47.New(states, gameData)
	if err != nil {
		return nil, fmt.Errorf("build protocol 47 adapter: %w", err)
	}

	dimension := b.dimension
	if dimension.Height == 0 {
		dimension = world.Overworld18()
	}

	generators := b.registry
	if generators == nil {
		generators = gen.DefaultRegistry()
	}

	// What generated a world is the world's own record. Reading it before the
	// generator is built is what lets an existing world keep its terrain when
	// the configuration says something else.
	level, hasLevel := readLevel(b)

	generator, genName, genVersion, genParams, err := buildGenerator(b, generators, states, level, b.log)
	if err != nil {
		return nil, err
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
		registry:    generators,
		genName:     genName,
		genVersion:  genVersion,
		genParams:   genParams,
		hasLevel:    hasLevel,
		level:       level,
		chunkDetail: b.chunkDetail,
		ticks:       newTickStats(),
		commands:    commandSet(b),
		authorizer:  b.authorizer,
	}

	// The epoch advances once per start. Exhaustion refuses to mint and keeps
	// serving: a server that will not start is worse than an audit gap.
	epoch, err := NextEpoch(level.ItemEpoch)
	if err != nil {
		b.log.Error("item identity is unavailable for this run", "error", err)
		epoch = level.ItemEpoch
	}
	if srv.minter, err = NewMinter(epoch); err != nil {
		b.log.Error("item identity is unavailable for this run", "error", err)
	}

	srv.recorder = NewRecorder(b.provenance, b.log, b.provenanceOverflow)
	if b.itemIdentity && srv.minter != nil {
		// A placed block's identity rides on the same switch as an item's.
		// There is no separate one because there is no useful configuration
		// where a chain that runs through an inventory should stop at a block.
		srv.blocks = storage.NewBlockIdentity()
		// The index tells the recorder about a duplication rather than
		// depending on it, which is what keeps the detector usable with the
		// records going nowhere but the log.
		srv.index = world.NewItemIndex(srv.minter, b.duplicatePolicy, func(d *ErrDuplicate) {
			b.log.Error("item duplication detected", "error", d)
			srv.recorder.RecordDuplicate(d)
		})
		// A dropped item is the one place items live outside a window, so the
		// player manager is on the write path as much as the click paths are.
		srv.players.SetItemIndex(srv.index, b.log)

		srv.reconcile = &storage.Reconciler{
			Index:  srv.index,
			Blocks: srv.blocks,
			Sink:   reconcileSink{recorder: srv.recorder},
			Log:    b.log,
			Dim:    dimension,
			Air:    w.Air(),
		}
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

	// The world reads a column from the store before generating one, and the
	// sidecar rides along: block identity has to be resident before the first
	// click that touches one of those blocks, and a chunk load is the only
	// event that says which ones are.
	if b.worldStore != nil {
		w.SetLoader(storeLoader{
			store:  b.worldStore,
			side:   b.sideStore,
			blocks: srv.blocks,
			server: srv,
			log:    b.log,
			name:   DefaultWorld,
		})
	}

	// The sidecar writes what the table holds. A store that does not implement
	// SidecarWriter keeps writing generation stamps and block identity is not
	// persisted, which is a property of that store rather than a failure.
	if writer, ok := b.sideStore.(SidecarWriter); ok && srv.blocks != nil {
		writer.SetSidecarSource(srv.sidecarFor)
	}

	if b.playerStore != nil {
		srv.playerStore = playerBridge{store: b.playerStore, srv: srv}
	}

	if b.observer != nil {
		srv.dispatch = newDispatcher(b.observer)
	}

	// Instrumentation is wired after the dispatcher, so a server with no
	// observer hands out no measure functions at all and the seams below stay
	// nil rather than pointing at a closure that returns a no-op.
	if srv.observed() {
		w.SetMeasure(srv.measureChunk)
		for _, store := range []any{b.worldStore, b.sideStore} {
			if m, ok := store.(StoreMeasurer); ok {
				m.SetMeasure(srv.measureChunk)
			}
		}
	}

	return srv, nil
}

// commandSet is what the server dispatches: the application's set, or the
// built-ins. A server that silently answered nothing would look broken rather
// than configured.
func commandSet(b *builder) Set {
	if b.hasCommands {
		return b.commands
	}

	return BuiltinCommands()
}

// WorldStore returns the world persistence the server was built with, or nil
// if it runs without any.
func (s *Server) WorldStore() WorldStore { return s.worldStore }

// storeLoader is the adapter between the world's Loader seam, which knows
// nothing about contexts or world names, and a WorldStore, which needs both.
type storeLoader struct {
	store  WorldStore
	side   SideStore
	blocks *storage.BlockIdentity
	server *Server
	log    *slog.Logger
	name   string
}

func (l storeLoader) LoadChunk(pos world.ChunkPos) (*world.Chunk, error) {
	c, err := l.store.LoadChunk(context.Background(), l.name, pos)
	if err != nil || c == nil {
		return c, err
	}

	stale := l.loadIdentity(pos, c)
	// Reconciled here, on the column the world is about to publish, because
	// this is the last moment it is private: the pass writes into containers,
	// and asking the world for a chunk it is still loading would ask it to
	// load the chunk again.
	l.server.recordReconciliation(l.server.reconcile.Chunk(pos, c, stale))

	return c, nil
}

// loadIdentity brings a chunk's block identity resident alongside its blocks.
//
// A stamp mismatch is loaded rather than discarded, and reported. The
// generation is a per-run counter and the world file does not carry it, so a
// sidecar written by a previous run can never match the stamp a fresh load
// gives its column: dropping identity on a mismatch would drop all of it on
// every restart. What the stamp still says is that the pair needs squaring
// against what is actually in the chunk, which is what reconcile does — and
// after a restart that is every chunk, deliberately.
// It reports whether the sidecar's stamp disagreed with the column's, which is
// what the reconciliation pass counts as stale.
func (l storeLoader) loadIdentity(pos world.ChunkPos, c *world.Chunk) bool {
	if l.side == nil || l.blocks == nil {
		return false
	}

	sc, found, err := l.side.Load(context.Background(), l.name, pos, c.Gen)
	if !found {
		if err != nil {
			l.log.Warn("read sidecar", "chunk", pos, "error", err)
		}

		return false
	}
	if err != nil {
		l.log.Debug("sidecar stamp does not match the column; reconciling", "chunk", pos, "error", err)
	}

	if bad := l.blocks.LoadChunk(pos, sc.BlockIdentity); bad > 0 {
		l.log.Warn("dropped unreadable block identity entries", "chunk", pos, "entries", bad)
	}
	// Written into the column before anyone can see it, which is the same
	// window the reconciliation pass runs in and the reason both live here.
	if bad := storage.ApplyContainerIdentity(pos, c, sc.ItemIdentity); bad > 0 {
		l.log.Warn("dropped unreadable container identity entries", "chunk", pos, "entries", bad)
	}

	return err != nil
}

// sidecarFor is what the sidecar store writes for one chunk: the identity of
// the blocks somebody placed in it, and the identity of the items in its
// containers.
func (s *Server) sidecarFor(pos world.ChunkPos, c *world.Chunk) Sidecar {
	return Sidecar{
		BlockIdentity: s.blocks.Chunk(pos),
		ItemIdentity:  storage.ContainerIdentity(c),
	}
}

// BlockIdentity is the sparse table of identified blocks, or nil when item
// identity is off.
func (s *Server) BlockIdentity() *storage.BlockIdentity { return s.blocks }

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
		// Set before Handle starts, like SaveAll: the click paths read it
		// without a lock because they only ever run on this connection's own
		// goroutine.
		connection.SetItemIndex(s.index)
		connection.SetBlockIdentity(s.blocks, s.recorder)
		connection.SetCommands(s.dispatchFor, s.completeFor)
		if s.observed() {
			connection.SetMeasure(s.measureConnection, s.countConnection)
		}

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
			s.sampleLevels()
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
	// The tick's own duration is sampled directly: it happens twenty times a
	// second, which is rare enough to measure one at a time and important
	// enough that an average would hide the tick that took a second.
	defer s.Measure(FeatureTick, Labels{World: DefaultWorld})()
	// Flushed at the end, after everything this tick did has been counted.
	defer s.flushTickStats()

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

		if err := s.saveLevel(ctx); err != nil {
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
	if level.GeneratorName == "" && s.genName != "" {
		// A world with no generator record — every world that exists today —
		// adopts the configured one and writes it. That is the whole
		// migration path, and it needs no separate step.
		s.log.Info("recording the generator this world was made with",
			"generator", s.genName, "version", s.genVersion)
		if err := s.saveLevel(ctx); err != nil {
			s.log.Error("failed to record the generator", "error", err)
		}
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

// buildGenerator resolves the configured generator.
//
// A supplied generator wins: WithGenerator is the escape hatch for a type that
// was never registered. Otherwise the name is looked up, and an unknown one is
// an error naming what is registered — before M11.4 the switch fell through to
// the noise generator, so `-generator flta` silently gave you default terrain.
func buildGenerator(
	b *builder,
	registry gen.Registry,
	states world.StateRegistry,
	level LevelData,
	log *slog.Logger,
) (gen.Generator, string, int, json.RawMessage, error) {
	if b.generator != nil {
		return b.generator, "", 0, nil, nil
	}

	name := b.genName
	if name == "" {
		name = b.settings.GeneratorType
	}

	raw := b.genParams
	if len(raw) == 0 {
		raw = b.settings.GeneratorParams
	}

	// A name mismatch resolves to the world's. Regenerating a world in a
	// different style would grow mountains at the edge of a superflat plane
	// somebody built on.
	if level.GeneratorName != "" && level.GeneratorName != name {
		log.Warn("the world was generated by a different generator; using the world's",
			"world", level.GeneratorName, "configured", name)
		name = level.GeneratorName
		raw = level.GeneratorParams
	} else if level.GeneratorName == name && len(level.GeneratorParams) > 0 {
		// Same generator: the world's parameters are the ones its terrain was
		// made from, so they win over the configuration for the same reason.
		raw = level.GeneratorParams
	}

	factory, ok := registry.Lookup(name)
	if !ok {
		return nil, "", 0, nil, fmt.Errorf("%w: unknown generator %q, have %v",
			ErrInvalidOption, name, registry.Names())
	}

	params, err := factory.Parse(raw)
	if err != nil {
		return nil, "", 0, nil, fmt.Errorf("%w: %s parameters: %w", ErrInvalidOption, name, err)
	}

	stored, err := gen.MarshalParams(params)
	if err != nil {
		return nil, "", 0, nil, err
	}

	// The state registry the generator resolves block names through is the
	// server's own, built a few lines above this call.
	generator, err := factory.New(b.settings.Seed, params, states)
	if err != nil {
		return nil, "", 0, nil, fmt.Errorf("%w: build generator %q: %w", ErrInvalidOption, name, err)
	}

	// A version mismatch keeps generating with the new version. Regenerating
	// the old chunks would rewrite terrain someone has built on; what the
	// record buys is that the difference is visible in the log rather than
	// discovered as a seam in the ground.
	if level.GeneratorName == factory.Name() &&
		level.GeneratorVersion != 0 &&
		level.GeneratorVersion != factory.Version() {
		log.Warn("the world was generated by a different version of this generator",
			"generator", factory.Name(),
			"world", level.GeneratorVersion,
			"running", factory.Version())
	}

	return generator, factory.Name(), factory.Version(), stored, nil
}

// readLevel reads the world's metadata before anything is built from it.
//
// Level only reads a file, so it needs no bound store — which is what lets it
// run before the world the store will be bound to exists.
func readLevel(b *builder) (LevelData, bool) {
	if b.worldStore == nil {
		return LevelData{}, false
	}

	level, found, err := b.worldStore.Level(context.Background(), DefaultWorld)
	if err != nil {
		b.log.Error("failed to read level data", "error", err)

		return LevelData{}, false
	}

	return level, found
}

// saveLevel writes the world's metadata, including what generated it.
func (s *Server) saveLevel(ctx context.Context) error {
	if s.worldStore == nil {
		return nil
	}

	age, timeOfDay := s.world.GetTime()

	epoch := s.level.ItemEpoch
	if s.minter != nil {
		epoch = s.minter.Epoch()
	}

	return s.worldStore.SaveLevel(ctx, DefaultWorld, LevelData{
		Age:              age,
		TimeOfDay:        timeOfDay,
		Seed:             s.cfg.Seed,
		GeneratorName:    s.genName,
		GeneratorVersion: s.genVersion,
		GeneratorParams:  s.genParams,
		ItemEpoch:        epoch,
	})
}

// Minter hands out item IDs for this run, or nil when the ID space is
// exhausted and identity is unavailable.
func (s *Server) Minter() *Minter { return s.minter }

// Recorder writes provenance records, or nil when the server runs without
// provenance.
func (s *Server) Recorder() *Recorder { return s.recorder }

// ItemIndex tracks where identified items are, or nil when item identity is
// off.
func (s *Server) ItemIndex() ItemIndex { return s.index }
