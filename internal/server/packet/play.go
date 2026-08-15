package packet

// GameMode constants.
const (
	GameModeSurvival  uint8 = 0
	GameModeCreative  uint8 = 1
	GameModeAdventure uint8 = 2
	GameModeSpectator uint8 = 3
)

// Dimension constants.
const (
	DimensionNether    int8 = -1
	DimensionOverworld int8 = 0
	DimensionEnd       int8 = 1
)

// Difficulty constants.
const (
	DifficultyPeaceful uint8 = 0
	DifficultyEasy     uint8 = 1
	DifficultyNormal   uint8 = 2
	DifficultyHard     uint8 = 3
)

// PlayerAbility flag bits.
const (
	AbilityInvulnerable int8 = 0x01
	AbilityFlying       int8 = 0x02
	AbilityAllowFlight  int8 = 0x04
	AbilityCreativeMode int8 = 0x08
)

// Player position and look flag bits. A set bit means the matching field is
// relative to the player's current value; a clear bit means it is absolute.
//
// PositionAbsolute is the whole field cleared, which is what a teleport and a
// spawn send. It is named rather than written as a bare zero because zero and
// "all absolute" are the same byte but not the same statement.
const (
	PositionAbsolute int8 = 0x00
	PositionRelX     int8 = 0x01
	PositionRelY     int8 = 0x02
	PositionRelZ     int8 = 0x04
	PositionRelYaw   int8 = 0x08
	PositionRelPitch int8 = 0x10
)
