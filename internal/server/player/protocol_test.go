package player

import "testing"

func TestDegreesToAngle(t *testing.T) {
	tests := []struct {
		deg  float32
		want int8
	}{
		{0, 0},
		{90, 64},
		{180, -128},
		{360, 0},
		{-90, -64},
		{45, 32},
	}
	for _, tt := range tests {
		got := DegreesToAngle(tt.deg)
		if got != tt.want {
			t.Errorf("DegreesToAngle(%v) = %d, want %d", tt.deg, got, tt.want)
		}
	}
}

func TestFixedPoint(t *testing.T) {
	tests := []struct {
		coord float64
		want  int32
	}{
		{0.0, 0},
		{1.0, 32},
		{0.5, 16},
		{-1.0, -32},
		{-0.5, -16},
	}
	for _, tt := range tests {
		got := FixedPoint(tt.coord)
		if got != tt.want {
			t.Errorf("FixedPoint(%v) = %d, want %d", tt.coord, got, tt.want)
		}
	}
}

func TestInViewDistance(t *testing.T) {
	tests := []struct {
		cx1, cz1, cx2, cz2, dist int
		want                     bool
	}{
		{0, 0, 0, 0, 8, true},   // same chunk
		{0, 0, 8, 0, 8, true},   // at boundary
		{0, 0, 9, 0, 8, false},  // one past boundary
		{0, 0, 0, 8, 8, true},   // z at boundary
		{0, 0, 0, 9, 8, false},  // z past boundary
		{0, 0, 8, 8, 8, true},   // diagonal at boundary
		{0, 0, -8, -8, 8, true}, // negative diagonal
		{0, 0, -9, 0, 8, false}, // negative past boundary
		{5, 5, 13, 5, 8, true},  // offset at boundary
		{5, 5, 14, 5, 8, false}, // offset past boundary
	}
	for _, tt := range tests {
		got := InViewDistance(tt.cx1, tt.cz1, tt.cx2, tt.cz2, tt.dist)
		if got != tt.want {
			t.Errorf("InViewDistance(%d,%d,%d,%d,%d) = %v, want %v",
				tt.cx1, tt.cz1, tt.cx2, tt.cz2, tt.dist, got, tt.want)
		}
	}
}

func TestDeltaFitsInByte(t *testing.T) {
	tests := []struct {
		dx, dy, dz int32
		want       bool
	}{
		{0, 0, 0, true},
		{127, 127, 127, true},
		{-128, -128, -128, true},
		{128, 0, 0, false},
		{0, -129, 0, false},
		{0, 0, 128, false},
	}
	for _, tt := range tests {
		got := DeltaFitsInByte(tt.dx, tt.dy, tt.dz)
		if got != tt.want {
			t.Errorf("DeltaFitsInByte(%d,%d,%d) = %v, want %v",
				tt.dx, tt.dy, tt.dz, got, tt.want)
		}
	}
}
