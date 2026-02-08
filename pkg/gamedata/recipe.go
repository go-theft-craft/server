package gamedata

type Recipe struct {
	Ingredients []Ingredient
	InShape     [][]Ingredient
	Result      RecipeResult
}

type Ingredient struct {
	ID       int
	Metadata int
}

type RecipeResult struct {
	ID       int
	Count    int
	Metadata int
}
