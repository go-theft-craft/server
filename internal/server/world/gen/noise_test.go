package gen

import (
	"math"
	"testing"
)

func TestNoise2DDeterministic(t *testing.T) {
	ng1 := NewNoiseGenerator(12345)
	ng2 := NewNoiseGenerator(12345)

	for i := 0; i < 100; i++ {
		x := float64(i) * 0.1
		y := float64(i) * 0.2
		if ng1.Noise2D(x, y) != ng2.Noise2D(x, y) {
			t.Fatalf("Noise2D not deterministic at (%f, %f)", x, y)
		}
	}
}

func TestNoise2DRange(t *testing.T) {
	ng := NewNoiseGenerator(42)

	for i := 0; i < 10000; i++ {
		x := float64(i)*0.37 - 500
		y := float64(i)*0.53 - 500
		v := ng.Noise2D(x, y)
		if v < -1.0 || v > 1.0 {
			t.Fatalf("Noise2D(%f, %f) = %f, out of [-1,1]", x, y, v)
		}
	}
}

func TestNoise3DDeterministic(t *testing.T) {
	ng1 := NewNoiseGenerator(99)
	ng2 := NewNoiseGenerator(99)

	for i := 0; i < 100; i++ {
		x := float64(i) * 0.15
		y := float64(i) * 0.25
		z := float64(i) * 0.35
		if ng1.Noise3D(x, y, z) != ng2.Noise3D(x, y, z) {
			t.Fatalf("Noise3D not deterministic at (%f, %f, %f)", x, y, z)
		}
	}
}

func TestNoise3DRange(t *testing.T) {
	ng := NewNoiseGenerator(42)

	for i := 0; i < 10000; i++ {
		x := float64(i)*0.37 - 500
		y := float64(i)*0.53 - 500
		z := float64(i)*0.71 - 500
		v := ng.Noise3D(x, y, z)
		if v < -1.0 || v > 1.0 {
			t.Fatalf("Noise3D(%f, %f, %f) = %f, out of [-1,1]", x, y, z, v)
		}
	}
}

func TestDifferentSeedsDifferentNoise(t *testing.T) {
	ng1 := NewNoiseGenerator(1)
	ng2 := NewNoiseGenerator(2)

	different := false
	for i := 0; i < 100; i++ {
		x := float64(i) * 0.1
		y := float64(i) * 0.2
		if ng1.Noise2D(x, y) != ng2.Noise2D(x, y) {
			different = true
			break
		}
	}
	if !different {
		t.Error("different seeds should produce different noise")
	}
}

func TestOctaveNoise2DRange(t *testing.T) {
	ng := NewNoiseGenerator(123)

	for i := 0; i < 1000; i++ {
		x := float64(i)*0.1 - 50
		y := float64(i)*0.2 - 50
		v := ng.OctaveNoise2D(x, y, 6, 0.5)
		if v < -1.0 || v > 1.0 {
			t.Fatalf("OctaveNoise2D = %f, out of [-1,1]", v)
		}
	}
}

func TestOctaveNoise2DSmoothness(t *testing.T) {
	ng := NewNoiseGenerator(456)

	// Adjacent samples should not differ by more than some reasonable amount.
	prev := ng.OctaveNoise2D(0, 0, 4, 0.5)
	step := 0.01
	for i := 1; i < 1000; i++ {
		x := float64(i) * step
		curr := ng.OctaveNoise2D(x, 0, 4, 0.5)
		diff := math.Abs(curr - prev)
		if diff > 0.1 {
			t.Fatalf("noise changed too rapidly at x=%f: diff=%f", x, diff)
		}
		prev = curr
	}
}
