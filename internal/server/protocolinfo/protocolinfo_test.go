package protocolinfo_test

import (
	"testing"

	v1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/server/internal/server/protocolinfo"
)

func TestProtocolVersionMatchesTheGeneratedDescriptor(t *testing.T) {
	if got, want := protocolinfo.ProtocolVersion, v1_8.Version().Protocol; got != want {
		t.Errorf("server advertises protocol %d, generated descriptor says %d", got, want)
	}
}

func TestVersionNameIsTheNameAClientIsTold(t *testing.T) {
	// Deliberately not v1_8.Version().Name, which is "1.8.9" and names the
	// dataset. The name a client is told is the generated data's
	// MinecraftVersion, and M10 settled which is which —
	// minecraft-protocol's docs/version-names.md is the record. Pinning the
	// constant against that field rather than against the literal alone is
	// what makes a disagreement fail naming the contract: if a later release
	// changes what protocol 47 advertises, this fails here instead of
	// changing a status response nobody was watching.
	set, err := v1_8.Data()
	if err != nil {
		t.Fatalf("v1_8.Data: %v", err)
	}
	if got, want := protocolinfo.VersionName, set.Version().MinecraftVersion; got != want {
		t.Errorf("server advertises %q, the generated data says a client is told %q", got, want)
	}
	if got := protocolinfo.VersionName; got != "1.8.8" {
		t.Errorf("version name is %q, want 1.8.8", got)
	}
}

func TestMetadataEndIsTheProtocol47Terminator(t *testing.T) {
	if got := protocolinfo.MetadataEnd; got != 0x7F {
		t.Errorf("metadata terminator is 0x%02X, want 0x7F", got)
	}
}
