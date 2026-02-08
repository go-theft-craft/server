package gamedata

type Item struct {
	ID                int
	Name              string
	DisplayName       string
	StackSize         int
	MaxDurability     int
	EnchantCategories []string
	RepairWith        []string
	Variations        []Variation
}
