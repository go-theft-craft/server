package conn

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
)

// minecraftSHA1HexDigest computes the Minecraft-style SHA1 hex digest.
// The result is a signed two's complement hex string (no zero-padding,
// negative values prefixed with "-").
func minecraftSHA1HexDigest(serverID string, sharedSecret, publicKeyDER []byte) string {
	h := sha1.New()
	h.Write([]byte(serverID))
	h.Write(sharedSecret)
	h.Write(publicKeyDER)
	hash := h.Sum(nil)

	// Interpret as signed big.Int (two's complement).
	n := new(big.Int).SetBytes(hash)
	// Check sign bit.
	if hash[0]&0x80 != 0 {
		// Negative: compute two's complement.
		// n = n - 2^160
		n.Sub(n, new(big.Int).Lsh(big.NewInt(1), 160))
	}
	return n.Text(16)
}

type mojangProperty struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	Signature string `json:"signature"`
}

type mojangProfile struct {
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	Properties []mojangProperty `json:"properties"`
}

// verifyWithMojang checks the player's session with the Mojang session server.
func verifyWithMojang(ctx context.Context, username, serverHash string) (*mojangProfile, error) {
	url := fmt.Sprintf("https://sessionserver.mojang.com/session/minecraft/hasJoined?username=%s&serverId=%s",
		username, serverHash)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create mojang request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mojang request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("mojang auth failed (status %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mojang unexpected status: %d", resp.StatusCode)
	}

	var profile mojangProfile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return nil, fmt.Errorf("decode mojang response: %w", err)
	}
	return &profile, nil
}

// fetchSkinByUsername looks up a Mojang account by username and returns its
// signed skin/cape properties. Returns (nil, nil) if the username does not
// correspond to a real Mojang account.
func fetchSkinByUsername(ctx context.Context, username string) ([]mojangProperty, error) {
	// Step 1: username → UUID
	uuidURL := fmt.Sprintf("https://api.mojang.com/users/profiles/minecraft/%s", username)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uuidURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create mojang profile request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mojang profile request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mojang profile unexpected status: %d", resp.StatusCode)
	}

	var profile mojangProfile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return nil, fmt.Errorf("decode mojang profile: %w", err)
	}

	// Step 2: UUID → profile with signed textures
	skinURL := fmt.Sprintf("https://sessionserver.mojang.com/session/minecraft/profile/%s?unsigned=false", profile.ID)

	req2, err := http.NewRequestWithContext(ctx, http.MethodGet, skinURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create mojang skin request: %w", err)
	}

	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		return nil, fmt.Errorf("mojang skin request: %w", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mojang skin unexpected status: %d", resp2.StatusCode)
	}

	var skinProfile mojangProfile
	if err := json.NewDecoder(resp2.Body).Decode(&skinProfile); err != nil {
		return nil, fmt.Errorf("decode mojang skin response: %w", err)
	}

	return skinProfile.Properties, nil
}

// formatMojangUUID inserts hyphens into a 32-char hex UUID string.
func formatMojangUUID(hexID string) string {
	if len(hexID) != 32 {
		return hexID
	}
	return strings.Join([]string{
		hexID[0:8],
		hexID[8:12],
		hexID[12:16],
		hexID[16:20],
		hexID[20:32],
	}, "-")
}
