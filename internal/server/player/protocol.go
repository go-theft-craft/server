package player

import "math"

// DegreesToAngle converts degrees to a Minecraft protocol angle byte.
// One byte = 1/256 of a full turn.
func DegreesToAngle(degrees float32) int8 {
	return int8(math.Floor(float64(degrees) / 360.0 * 256.0))
}

// FixedPoint converts a double coordinate to fixed-point (coord * 32).
func FixedPoint(coord float64) int32 {
	return int32(math.Floor(coord * 32.0))
}

// InViewDistance checks if two chunk positions are within view distance
// using Chebyshev (chessboard) distance.
func InViewDistance(cx1, cz1, cx2, cz2, viewDist int) bool {
	dx := cx1 - cx2
	if dx < 0 {
		dx = -dx
	}
	dz := cz1 - cz2
	if dz < 0 {
		dz = -dz
	}
	return dx <= viewDist && dz <= viewDist
}

// DeltaFitsInByte returns true if all three deltas fit in a signed byte [-128, 127].
func DeltaFitsInByte(dx, dy, dz int32) bool {
	return dx >= -128 && dx <= 127 &&
		dy >= -128 && dy <= 127 &&
		dz >= -128 && dz <= 127
}
