package server_test

import (
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"log/slog"
	"testing"

	"github.com/go-theft-craft/server/config"
	"github.com/go-theft-craft/server/pkg/world/gen"
	"github.com/go-theft-craft/server/server"
)

func TestNewAppliesDefaultsWhenGivenNoOptions(t *testing.T) {
	srv, err := server.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got := srv.Settings()
	want := config.DefaultConfig()

	if got.Port != want.Port {
		t.Errorf("port is %d, want %d", got.Port, want.Port)
	}
	if got.MOTD != want.MOTD {
		t.Errorf("MOTD is %q, want %q", got.MOTD, want.MOTD)
	}
}

func TestOptionsApplyInOrderAndTheLastOneWins(t *testing.T) {
	srv, err := server.New(server.WithPort(1), server.WithPort(2))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if got := srv.Settings().Port; got != 2 {
		t.Errorf("port is %d, want 2 from the later option", got)
	}
}

func TestWithSettingsIsOverriddenByLaterOptions(t *testing.T) {
	// A caller loading a config file then applying flags is the vanilla
	// example's exact shape, so the ordering has to work that way round.
	settings := config.DefaultConfig()
	settings.Port = 100

	srv, err := server.New(
		server.WithSettings(settings),
		server.WithPort(200),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if got := srv.Settings().Port; got != 200 {
		t.Errorf("port is %d, want 200", got)
	}
}

func TestWithSettingsCopiesRatherThanAliases(t *testing.T) {
	// The caller keeps its own struct. Mutating it after New must not
	// reach inside the server.
	settings := config.DefaultConfig()
	settings.Port = 100

	srv, err := server.New(server.WithSettings(settings))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	settings.Port = 999

	if got := srv.Settings().Port; got != 100 {
		t.Errorf("port is %d, want 100; New aliased the caller's settings", got)
	}
}

func TestAnInvalidOptionIsRejectedBeforeAnyWork(t *testing.T) {
	_, err := server.New(server.WithPort(-1))
	if !errors.Is(err, server.ErrInvalidOption) {
		t.Fatalf("New returned %v, want ErrInvalidOption", err)
	}
}

func TestWithGeneratorReplacesTheOneSettingsWouldHaveChosen(t *testing.T) {
	custom := gen.NewFlatGenerator(7)

	srv, err := server.New(
		server.WithSeed(1),
		server.WithGenerator(custom),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if srv.Generator() != custom {
		t.Error("New built its own generator instead of using the supplied one")
	}
}

func TestWithLoggerIsUsedAndNilLoggerIsRejected(t *testing.T) {
	log := slog.New(slog.DiscardHandler)

	srv, err := server.New(server.WithLogger(log))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if srv.Logger() != log {
		t.Error("New did not use the supplied logger")
	}

	if _, err := server.New(server.WithLogger(nil)); !errors.Is(err, server.ErrInvalidOption) {
		t.Errorf("New accepted a nil logger, returned %v", err)
	}
}

func TestWithPrivateKeyIsCarriedIntoSettings(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	srv, err := server.New(server.WithPrivateKey(key))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if srv.Settings().PrivateKey != key {
		t.Error("New did not carry the private key into settings")
	}
}
