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
