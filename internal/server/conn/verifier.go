package conn

import (
	"context"
	"fmt"

	"github.com/go-theft-craft/minecraft-protocol/login"
	"github.com/go-theft-craft/minecraft-protocol/wire/java"

	"github.com/go-theft-craft/server/internal/server/player"
)

// mojangVerifier is the session-server half of an online-mode login.
//
// It is the acceptor's only outbound edge. The HTTP behavior and error text
// are the ones the server used before the migration, so an operator watching
// logs sees no change.
//
// It also keeps the skin properties the session server returned. login.Profile
// carries identity only — a name and a UUID — because that is all the protocol
// proves, and skins are a server concern rather than a wire one.
type mojangVerifier struct {
	properties []player.SkinProperty
}

// Verify implements login.Verifier.
func (v *mojangVerifier) Verify(
	ctx context.Context,
	username java.Username,
	hash java.ServerHash,
) (login.Profile, error) {
	profile, err := verifyMojang(ctx, username.String(), hash.String())
	if err != nil {
		return login.Profile{}, fmt.Errorf("mojang verify: %w", err)
	}

	identity, err := java.ParseUUID(profile.ID)
	if err != nil {
		return login.Profile{}, fmt.Errorf("mojang profile UUID %q: %w", profile.ID, err)
	}

	// The session server is authoritative about the account name, including
	// its capitalization, so the name it returns replaces the claimed one.
	name, err := java.ParseUsername(profile.Name)
	if err != nil {
		return login.Profile{}, fmt.Errorf("mojang profile username %q: %w", profile.Name, err)
	}

	v.properties = skinProperties(profile.Properties)

	return login.Profile{Name: name, UUID: identity}, nil
}

// skinProperties converts the session server's properties into the player
// package's own type.
func skinProperties(source []mojangProperty) []player.SkinProperty {
	if len(source) == 0 {
		return nil
	}

	converted := make([]player.SkinProperty, len(source))
	for index, property := range source {
		converted[index] = player.SkinProperty{
			Name:      property.Name,
			Value:     property.Value,
			Signature: property.Signature,
		}
	}

	return converted
}
