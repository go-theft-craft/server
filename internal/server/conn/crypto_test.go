package conn

import (
	"crypto/md5" //nolint:gosec // G501: the offline UUID derivation is MD5 by definition.
	"fmt"
	"testing"

	"github.com/go-theft-craft/minecraft-protocol/login"
	"github.com/go-theft-craft/minecraft-protocol/wire/java"
)

// The server hash and the Mojang UUID formatter moved to minecraft-protocol,
// where java.ComputeServerHash is pinned against the same three canonical
// vectors this file used to hold.
//
// What has to be asserted here instead is that a player's identity did not
// change with them. An offline player's UUID is derived from their name, so a
// different derivation would strand every saved player file: the server looks
// up saved data by UUID.
func TestOfflineIdentityIsUnchangedByTheMigration(t *testing.T) {
	for _, username := range []string{"Tester", "Notch", "jeb_", "a", "0123456789abcdef"} {
		t.Run(username, func(t *testing.T) {
			name, err := java.ParseUsername(username)
			if err != nil {
				t.Fatalf("ParseUsername(%q): %v", username, err)
			}

			// The derivation the server performed before the migration,
			// written out here rather than called, because the function it
			// called no longer exists.
			//nolint:gosec // G401: the derivation is defined in terms of MD5.
			sum := md5.Sum([]byte("OfflinePlayer:" + username))
			sum[6] = (sum[6] & 0x0f) | 0x30
			sum[8] = (sum[8] & 0x3f) | 0x80
			want := fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
				sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])

			if got := login.OfflineUUID(name).String(); got != want {
				t.Fatalf("offline UUID for %q = %q, want %q", username, got, want)
			}
		})
	}
}
