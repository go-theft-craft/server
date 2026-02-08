package gamedata_test

import (
	"testing"

	"github.com/go-theft-craft/server/pkg/gamedata"
)

func TestLoad_UnknownVersion(t *testing.T) {
	_, err := gamedata.Load("nonexistent-version")
	if err == nil {
		t.Fatal("expected error for unknown version, got nil")
	}
}

func TestRegisterAndLoad(t *testing.T) {
	called := false
	gamedata.Register("test-version", func() *gamedata.GameData {
		called = true
		return &gamedata.GameData{}
	})

	gd, err := gamedata.Load("test-version")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gd == nil {
		t.Fatal("expected non-nil GameData")
	}
	if !called {
		t.Fatal("factory function was not called")
	}
}

func TestRegisteredVersions(t *testing.T) {
	gamedata.Register("rv-test", func() *gamedata.GameData {
		return &gamedata.GameData{}
	})

	versions := gamedata.RegisteredVersions()
	found := false
	for _, v := range versions {
		if v == "rv-test" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected 'rv-test' in registered versions, got %v", versions)
	}
}
