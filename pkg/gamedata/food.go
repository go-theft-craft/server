package gamedata

type Food struct {
	ID               int
	Name             string
	DisplayName      string
	StackSize        int
	FoodPoints       float64
	Saturation       float64
	EffectiveQuality float64
	SaturationRatio  float64
	Variations       []Variation
}
