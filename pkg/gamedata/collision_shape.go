package gamedata

type CollisionShapes struct {
	Blocks map[string][]int
	Shapes map[int][]BoundingBox
}

type BoundingBox struct {
	MinX, MinY, MinZ float64
	MaxX, MaxY, MaxZ float64
}
