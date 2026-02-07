package gen

// Simplex noise implementation based on the original algorithm by Ken Perlin.
// Produces values in the range [-1, 1].

// grad3 are gradient vectors for 3D simplex noise.
var grad3 = [12][3]float64{
	{1, 1, 0},
	{-1, 1, 0},
	{1, -1, 0},
	{-1, -1, 0},
	{1, 0, 1},
	{-1, 0, 1},
	{1, 0, -1},
	{-1, 0, -1},
	{0, 1, 1},
	{0, -1, 1},
	{0, 1, -1},
	{0, -1, -1},
}

// NoiseGenerator produces deterministic simplex noise from a seed.
type NoiseGenerator struct {
	perm [512]int
}

// NewNoiseGenerator creates a noise generator with a seeded permutation table.
func NewNoiseGenerator(seed int64) *NoiseGenerator {
	ng := &NoiseGenerator{}

	// Initialize with identity permutation.
	var p [256]int
	for i := range p {
		p[i] = i
	}

	// Fisher-Yates shuffle with seed-derived random.
	s := seed
	for i := 255; i > 0; i-- {
		s = s*6364136223846793005 + 1442695040888963407 // LCG
		j := int((s>>33)&0x7FFFFFFF) % (i + 1)
		if j < 0 {
			j = -j
		}
		p[i], p[j] = p[j], p[i]
	}

	// Double the permutation table for wrapping.
	for i := 0; i < 512; i++ {
		ng.perm[i] = p[i&255]
	}
	return ng
}

// Noise2D returns 2D simplex noise for the given coordinates.
// Output is in the range [-1, 1].
func (ng *NoiseGenerator) Noise2D(x, y float64) float64 {
	const (
		f2 = 0.36602540378443864676 // (sqrt(3) - 1) / 2
		g2 = 0.21132486540518711775 // (3 - sqrt(3)) / 6
	)

	// Skew input space to determine simplex cell.
	s := (x + y) * f2
	i := fastFloor(x + s)
	j := fastFloor(y + s)

	t := float64(i+j) * g2
	x0 := x - (float64(i) - t)
	y0 := y - (float64(j) - t)

	// Determine which simplex we are in.
	var i1, j1 int
	if x0 > y0 {
		i1 = 1
	} else {
		j1 = 1
	}

	x1 := x0 - float64(i1) + g2
	y1 := y0 - float64(j1) + g2
	x2 := x0 - 1.0 + 2.0*g2
	y2 := y0 - 1.0 + 2.0*g2

	ii := i & 255
	jj := j & 255
	gi0 := ng.perm[ii+ng.perm[jj]] % 12
	gi1 := ng.perm[ii+i1+ng.perm[jj+j1]] % 12
	gi2 := ng.perm[ii+1+ng.perm[jj+1]] % 12

	var n0, n1, n2 float64

	t0 := 0.5 - x0*x0 - y0*y0
	if t0 >= 0 {
		t0 *= t0
		n0 = t0 * t0 * dot2(grad3[gi0], x0, y0)
	}

	t1 := 0.5 - x1*x1 - y1*y1
	if t1 >= 0 {
		t1 *= t1
		n1 = t1 * t1 * dot2(grad3[gi1], x1, y1)
	}

	t2 := 0.5 - x2*x2 - y2*y2
	if t2 >= 0 {
		t2 *= t2
		n2 = t2 * t2 * dot2(grad3[gi2], x2, y2)
	}

	return 70.0 * (n0 + n1 + n2)
}

// Noise3D returns 3D simplex noise for the given coordinates.
// Output is in the range [-1, 1].
func (ng *NoiseGenerator) Noise3D(x, y, z float64) float64 {
	const (
		f3 = 1.0 / 3.0
		g3 = 1.0 / 6.0
	)

	s := (x + y + z) * f3
	i := fastFloor(x + s)
	j := fastFloor(y + s)
	k := fastFloor(z + s)

	t := float64(i+j+k) * g3
	x0 := x - (float64(i) - t)
	y0 := y - (float64(j) - t)
	z0 := z - (float64(k) - t)

	var i1, j1, k1, i2, j2, k2 int
	if x0 >= y0 {
		if y0 >= z0 {
			i1, j1, k1 = 1, 0, 0
			i2, j2, k2 = 1, 1, 0
		} else if x0 >= z0 {
			i1, j1, k1 = 1, 0, 0
			i2, j2, k2 = 1, 0, 1
		} else {
			i1, j1, k1 = 0, 0, 1
			i2, j2, k2 = 1, 0, 1
		}
	} else {
		if y0 < z0 {
			i1, j1, k1 = 0, 0, 1
			i2, j2, k2 = 0, 1, 1
		} else if x0 < z0 {
			i1, j1, k1 = 0, 1, 0
			i2, j2, k2 = 0, 1, 1
		} else {
			i1, j1, k1 = 0, 1, 0
			i2, j2, k2 = 1, 1, 0
		}
	}

	x1 := x0 - float64(i1) + g3
	y1 := y0 - float64(j1) + g3
	z1 := z0 - float64(k1) + g3
	x2 := x0 - float64(i2) + 2.0*g3
	y2 := y0 - float64(j2) + 2.0*g3
	z2 := z0 - float64(k2) + 2.0*g3
	x3 := x0 - 1.0 + 3.0*g3
	y3 := y0 - 1.0 + 3.0*g3
	z3 := z0 - 1.0 + 3.0*g3

	ii := i & 255
	jj := j & 255
	kk := k & 255
	gi0 := ng.perm[ii+ng.perm[jj+ng.perm[kk]]] % 12
	gi1 := ng.perm[ii+i1+ng.perm[jj+j1+ng.perm[kk+k1]]] % 12
	gi2 := ng.perm[ii+i2+ng.perm[jj+j2+ng.perm[kk+k2]]] % 12
	gi3 := ng.perm[ii+1+ng.perm[jj+1+ng.perm[kk+1]]] % 12

	var n0, n1, n2, n3 float64

	t0 := 0.6 - x0*x0 - y0*y0 - z0*z0
	if t0 >= 0 {
		t0 *= t0
		n0 = t0 * t0 * dot3(grad3[gi0], x0, y0, z0)
	}

	t1 := 0.6 - x1*x1 - y1*y1 - z1*z1
	if t1 >= 0 {
		t1 *= t1
		n1 = t1 * t1 * dot3(grad3[gi1], x1, y1, z1)
	}

	t2 := 0.6 - x2*x2 - y2*y2 - z2*z2
	if t2 >= 0 {
		t2 *= t2
		n2 = t2 * t2 * dot3(grad3[gi2], x2, y2, z2)
	}

	t3 := 0.6 - x3*x3 - y3*y3 - z3*z3
	if t3 >= 0 {
		t3 *= t3
		n3 = t3 * t3 * dot3(grad3[gi3], x3, y3, z3)
	}

	return 32.0 * (n0 + n1 + n2 + n3)
}

// OctaveNoise2D layers multiple octaves of 2D noise for natural-looking terrain.
// Returns a value roughly in [-1, 1].
func (ng *NoiseGenerator) OctaveNoise2D(x, y float64, octaves int, persistence float64) float64 {
	var total, amplitude, maxVal float64
	frequency := 1.0
	amplitude = 1.0

	for range octaves {
		total += ng.Noise2D(x*frequency, y*frequency) * amplitude
		maxVal += amplitude
		amplitude *= persistence
		frequency *= 2.0
	}
	return total / maxVal
}

// OctaveNoise3D layers multiple octaves of 3D noise.
func (ng *NoiseGenerator) OctaveNoise3D(x, y, z float64, octaves int, persistence float64) float64 {
	var total, amplitude, maxVal float64
	frequency := 1.0
	amplitude = 1.0

	for range octaves {
		total += ng.Noise3D(x*frequency, y*frequency, z*frequency) * amplitude
		maxVal += amplitude
		amplitude *= persistence
		frequency *= 2.0
	}
	return total / maxVal
}

func fastFloor(x float64) int {
	xi := int(x)
	if x < float64(xi) {
		return xi - 1
	}
	return xi
}

func dot2(g [3]float64, x, y float64) float64 {
	return g[0]*x + g[1]*y
}

func dot3(g [3]float64, x, y, z float64) float64 {
	return g[0]*x + g[1]*y + g[2]*z
}
