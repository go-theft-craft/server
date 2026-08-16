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

func TestVersionNameStaysWhatTheServerAdvertised(t *testing.T) {
	// Deliberately not v1_8.Version().Name, which is "1.8.9". Both are
	// protocol 47 and this migration changes no byte on the wire.
	if got := protocolinfo.VersionName; got != "1.8.8" {
		t.Errorf("version name is %q, want 1.8.8", got)
	}
}

func TestMetadataEndIsTheProtocol47Terminator(t *testing.T) {
	if got := protocolinfo.MetadataEnd; got != 0x7F {
		t.Errorf("metadata terminator is 0x%02X, want 0x7F", got)
	}
}
