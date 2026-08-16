package conn

import (
	"github.com/go-theft-craft/minecraft-protocol/data"

	"github.com/go-theft-craft/server/internal/server/player"
)

// matchRecipe2x2 matches the player's own 2x2 grid. The grid layout is
// [0]=top-left, [1]=top-right, [2]=bottom-left, [3]=bottom-right.
func matchRecipe2x2(grid [4]player.Slot, recipes data.RecipeRegistry) player.Slot {
	return matchRecipe(grid[:], 2, recipes)
}

// matchRecipe tries a square crafting grid of the given side length against
// every known recipe. The grid is row-major and must hold size*size slots: 2
// for the player's own window, 3 for a crafting table.
func matchRecipe(grid []player.Slot, size int, recipes data.RecipeRegistry) player.Slot {
	if len(grid) < size*size {
		return player.EmptySlot
	}
	grid = grid[:size*size]

	for _, recipeList := range recipes.All() {
		for _, recipe := range recipeList {
			switch {
			case len(recipe.InShape) > 0:
				if matchShaped(grid, size, recipe) {
					return recipeResultToSlot(recipe.Result)
				}
			case len(recipe.Ingredients) > 0:
				if matchShapeless(grid, size, recipe) {
					return recipeResultToSlot(recipe.Result)
				}
			}
		}
	}

	return player.EmptySlot
}

// matchShaped checks whether the shape sits anywhere in the grid, in either
// orientation. Vanilla matches a mirrored shape too, so a recipe drawn
// left-handed crafts the same.
func matchShaped(grid []player.Slot, size int, recipe data.Recipe) bool {
	shape := recipe.InShape
	rows := len(shape)
	if rows == 0 || rows > size {
		return false
	}
	cols := 0
	for _, row := range shape {
		if len(row) > cols {
			cols = len(row)
		}
	}
	if cols > size {
		return false
	}

	for _, candidate := range []data.RecipeShape{shape, mirrorShape(shape)} {
		for rowOff := 0; rowOff <= size-rows; rowOff++ {
			for colOff := 0; colOff <= size-cols; colOff++ {
				if checkShapedAt(grid, size, candidate, rowOff, colOff) {
					return true
				}
			}
		}
	}

	return false
}

// checkShapedAt checks the shape against the grid at one offset. Every grid
// cell the shape does not cover has to be empty, or a recipe would match a grid
// that holds more than it asks for.
func checkShapedAt(grid []player.Slot, size int, shape data.RecipeShape, rowOff, colOff int) bool {
	for r := range size {
		for c := range size {
			gridSlot := grid[r*size+c]
			shapeR := r - rowOff
			shapeC := c - colOff

			var expected data.Ingredient
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
				if data.ItemID(gridSlot.BlockID) != expected.ID {
					return false
				}
				if expected.Metadata >= 0 && data.Metadata(gridSlot.ItemDamage) != expected.Metadata {
					return false
				}
			} else if !gridSlot.IsEmpty() {
				// This position should be empty.
				return false
			}
		}
	}

	return true
}

// mirrorShape horizontally flips a recipe shape.
func mirrorShape(shape data.RecipeShape) data.RecipeShape {
	mirrored := make(data.RecipeShape, len(shape))
	for i, row := range shape {
		mirrored[i] = make(data.RecipeIngredients, len(row))
		for j := range row {
			mirrored[i][j] = row[len(row)-1-j]
		}
	}

	return mirrored
}

// matchShapeless checks whether the grid holds exactly the required
// ingredients, in any arrangement.
func matchShapeless(grid []player.Slot, size int, recipe data.Recipe) bool {
	if len(recipe.Ingredients) > size*size {
		return false
	}

	var gridItems []player.Slot
	for _, s := range grid {
		if !s.IsEmpty() {
			gridItems = append(gridItems, s)
		}
	}

	if len(gridItems) != len(recipe.Ingredients) {
		return false
	}

	used := make([]bool, len(gridItems))
	for _, ing := range recipe.Ingredients {
		found := false
		for j, gs := range gridItems {
			if used[j] {
				continue
			}
			if data.ItemID(gs.BlockID) == ing.ID && (ing.Metadata < 0 || data.Metadata(gs.ItemDamage) == ing.Metadata) {
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

func recipeResultToSlot(result data.RecipeResult) player.Slot {
	return player.Slot{
		BlockID:    int16(result.ID),
		ItemCount:  int8(result.Count),
		ItemDamage: int16(result.Metadata),
	}
}
