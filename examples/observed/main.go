// Command observed runs a server that exports what it is doing.
//
// It is the worked answer to "how do I wire an Observer up", which a README
// snippet cannot be: the interesting part is not the option, it is the
// mapping, and a mapping that is not compiled and started by a test is a
// mapping that rots.
//
// The metrics endpoint binds to loopback. A metrics endpoint is not a public
// one — it says who is online, where they are, and how hard the machine is
// working — and a default that has to be explained after the fact is the wrong
// default.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/go-theft-craft/server/config"
	"github.com/go-theft-craft/server/server"
)

func main() {
	cfg := config.DefaultConfig()

	var dataDir, metricsAddr string
	flag.StringVar(&dataDir, "data-dir", "data", "directory for persistent data")
	flag.StringVar(&metricsAddr, "metrics", "127.0.0.1:9090", "address for the metrics endpoint; loopback by default")
	flag.IntVar(&cfg.Port, "port", cfg.Port, "server port")
	flag.IntVar(&cfg.ViewDistance, "view-distance", cfg.ViewDistance, "entity view distance in chunks")
	flag.IntVar(&cfg.WorldRadius, "world-radius", cfg.WorldRadius, "world radius in chunks (0 = infinite)")
	flag.StringVar(&cfg.GeneratorType, "generator", cfg.GeneratorType, "world generator type (default, flat)")
	chunkDetail := flag.Bool("chunk-detail", false, "label chunk samples with exact coordinates; see the option's cardinality note")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	store, err := server.FileStore(dataDir, log)
	if err != nil {
		log.Error("create store", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	registry := prometheus.NewRegistry()
	sink := newSink(registry)

	options := []server.Option{
		server.WithSettings(cfg),
		server.WithLogger(log),
		server.WithWorldStore(store.World()),
		server.WithSideStore(store.Side()),
		server.WithPlayerStore(store.Players()),
		server.WithObserver(sink),
	}
	if *chunkDetail {
		options = append(options, server.WithChunkDetail())
	}

	srv, err := server.New(options...)
	if err != nil {
		log.Error("create server", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	metrics := &http.Server{
		Addr:              metricsAddr,
		Handler:           promhttp.HandlerFor(registry, promhttp.HandlerOpts{}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		log.Info("metrics listening", "addr", metricsAddr, "path", "/metrics")
		if err := metrics.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("metrics endpoint", "error", err)
		}
	}()
	defer func() {
		shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = metrics.Shutdown(shutdown)
	}()

	if err := srv.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
