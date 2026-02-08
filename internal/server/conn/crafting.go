package conn

import (
	"github.com/go-theft-craft/server/internal/server/player"
	"github.com/go-theft-craft/server/pkg/gamedata"
)

// matchRecipe2x2 tries to match a 2x2 crafting grid against all known recipes.
// The grid layout is: [0]=top-left, [1]=top-right, [2]=bottom-left, [3]=bottom-right.
func matchRecipe2x2(grid [4]player.Slot, recipes gamedata.RecipeRegistry) player.Slot {
	all := recipes.All()
	for _, recipeList := range all {
		for _, recipe := range recipeList {
			if len(recipe.InShape) > 0 {
				if matchShaped2x2(grid, recipe) {
					return recipeResultToSlot(recipe.Result)
				}
			} else if len(recipe.Ingredients) > 0 {
				if matchShapeless2x2(grid, recipe) {
					return recipeResultToSlot(recipe.Result)
				}
			}
		}
	}
	return player.EmptySlot
}

// matchShaped2x2 checks if the grid matches a shaped recipe at any valid position.
func matchShaped2x2(grid [4]player.Slot, recipe gamedata.Recipe) bool {
	shape := recipe.InShape
	rows := len(shape)
	if rows == 0 || rows > 2 {
		return false
	}
	cols := 0
	for _, row := range shape {
		if len(row) > cols {
			cols = len(row)
		}
	}
	if cols > 2 {
		return false
	}

	// Try placing the shape at all valid offsets in the 2x2 grid.
	for rowOff := 0; rowOff <= 2-rows; rowOff++ {
		for colOff := 0; colOff <= 2-cols; colOff++ {
			if checkShapedAt(grid, shape, rowOff, colOff) {
				return true
			}
		}
	}

	// Try mirrored (horizontally flipped) shape.
	mirrored := mirrorShape(shape)
	for rowOff := 0; rowOff <= 2-rows; rowOff++ {
		for colOff := 0; colOff <= 2-cols; colOff++ {
			if checkShapedAt(grid, mirrored, rowOff, colOff) {
				return true
			}
		}
	}

	return false
}

// checkShapedAt checks if the shape matches at the given offset in the 2x2 grid.
func checkShapedAt(grid [4]player.Slot, shape [][]gamedata.Ingredient, rowOff, colOff int) bool {
	for r := 0; r < 2; r++ {
		for c := 0; c < 2; c++ {
			gridSlot := grid[r*2+c]
			shapeR := r - rowOff
			shapeC := c - colOff

			var expected gamedata.Ingredient
			inShape := false
			if shapeR >= 0 && shapeR < len(shape) && shapeC >= 0 && shapeC < len(shape[shapeR]) {
				expected = shape[shapeR][shapeC]
				inShape = true
			}

			if inShape && expected.ID > 0 {
				// This position needs an ingredient.
				if gridSlot.IsEmpty() {
					return false
				}
				if int(gridSlot.BlockID) != expected.ID {
					return false
				}
				if expected.Metadata >= 0 && int(gridSlot.ItemDamage) != expected.Metadata {
					return false
				}
			} else {
				// This position should be empty.
				if !gridSlot.IsEmpty() {
					return false
				}
			}
		}
	}
	return true
}

// mirrorShape horizontally flips a recipe shape.
func mirrorShape(shape [][]gamedata.Ingredient) [][]gamedata.Ingredient {
	mirrored := make([][]gamedata.Ingredient, len(shape))
	for i, row := range shape {
		mirrored[i] = make([]gamedata.Ingredient, len(row))
		for j := range row {
			mirrored[i][j] = row[len(row)-1-j]
		}
	}
	return mirrored
}

// matchShapeless2x2 checks if the grid contains exactly the required ingredients
// (in any order) for a shapeless recipe.
func matchShapeless2x2(grid [4]player.Slot, recipe gamedata.Recipe) bool {
	if len(recipe.Ingredients) > 4 {
		return false
	}

	// Collect non-empty grid items.
	var gridItems []player.Slot
	for _, s := range grid {
		if !s.IsEmpty() {
			gridItems = append(gridItems, s)
		}
	}

	if len(gridItems) != len(recipe.Ingredients) {
		return false
	}

	// Try to match each ingredient to a grid item.
	used := make([]bool, len(gridItems))
	for _, ing := range recipe.Ingredients {
		found := false
		for j, gs := range gridItems {
			if used[j] {
				continue
			}
			if int(gs.BlockID) == ing.ID && (ing.Metadata < 0 || int(gs.ItemDamage) == ing.Metadata) {
				used[j] = true
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func recipeResultToSlot(result gamedata.RecipeResult) player.Slot {
	return player.Slot{
		BlockID:    int16(result.ID),
		ItemCount:  int8(result.Count),
		ItemDamage: int16(result.Metadata),
	}
}
