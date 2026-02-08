package gamedata

type BlockRegistry interface {
	ByID(id int) (Block, bool)
	ByName(name string) (Block, bool)
	All() []Block
}

type ItemRegistry interface {
	ByID(id int) (Item, bool)
	ByName(name string) (Item, bool)
	All() []Item
}

type EntityRegistry interface {
	ByID(id int) (Entity, bool)
	ByName(name string) (Entity, bool)
	All() []Entity
}

type BiomeRegistry interface {
	ByID(id int) (Biome, bool)
	ByName(name string) (Biome, bool)
	All() []Biome
}

type EffectRegistry interface {
	ByID(id int) (Effect, bool)
	ByName(name string) (Effect, bool)
	All() []Effect
}

type EnchantmentRegistry interface {
	ByID(id int) (Enchantment, bool)
	ByName(name string) (Enchantment, bool)
	All() []Enchantment
}

type FoodRegistry interface {
	ByID(id int) (Food, bool)
	ByName(name string) (Food, bool)
	All() []Food
}

type ParticleRegistry interface {
	ByID(id int) (Particle, bool)
	ByName(name string) (Particle, bool)
	All() []Particle
}

type InstrumentRegistry interface {
	ByID(id int) (Instrument, bool)
	ByName(name string) (Instrument, bool)
	All() []Instrument
}

type AttributeRegistry interface {
	ByName(name string) (Attribute, bool)
	ByResource(resource string) (Attribute, bool)
	All() []Attribute
}

type WindowRegistry interface {
	ByID(id string) (Window, bool)
	ByName(name string) (Window, bool)
	All() []Window
}

type MaterialRegistry interface {
	ByName(name string) (Material, bool)
	All() []Material
}

type RecipeRegistry interface {
	ByID(id int) []Recipe
	All() map[int][]Recipe
}

type LanguageRegistry interface {
	Get(key string) (string, bool)
	All() map[string]string
}
