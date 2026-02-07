package conn

import "testing"

func TestMinecraftSHA1HexDigest(t *testing.T) {
	// Test vectors from wiki.vg.
	tests := []struct {
		name string
		want string
	}{
		{"Notch", "4ed1f46bbe04bc756bcb17c0c7ce3e4632f06a48"},
		{"jeb_", "-7c9d5b0044c130109a5d7b5fb5c317c02b4e28c1"},
		{"simon", "88e16a1019277b15d58faf0541e11910eb756f6"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// wiki.vg test vectors use the username as the serverID input
			// with empty sharedSecret and publicKey.
			got := minecraftSHA1HexDigest(tt.name, nil, nil)
			if got != tt.want {
				t.Errorf("minecraftSHA1HexDigest(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestFormatMojangUUID(t *testing.T) {
	input := "4566e69fc90748ee8d71d7ba5aa00d20"
	want := "4566e69f-c907-48ee-8d71-d7ba5aa00d20"
	got := formatMojangUUID(input)
	if got != want {
		t.Errorf("formatMojangUUID(%q) = %q, want %q", input, got, want)
	}
}
