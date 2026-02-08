package gamedata

type GameData struct {
	Blocks          BlockRegistry
	Items           ItemRegistry
	Entities        EntityRegistry
	Biomes          BiomeRegistry
	Effects         EffectRegistry
	Enchantments    EnchantmentRegistry
	Foods           FoodRegistry
	Particles       ParticleRegistry
	Instruments     InstrumentRegistry
	Attributes      AttributeRegistry
	Windows         WindowRegistry
	Materials       MaterialRegistry
	Recipes         RecipeRegistry
	Language        LanguageRegistry
	CollisionShapes *CollisionShapes
	Protocol        *Protocol
	Version         *Version
}
