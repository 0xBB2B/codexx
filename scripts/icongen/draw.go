package main

import (
	"image"
	"image/color"
	"image/draw"
	"math"
)

type segment struct{ x1, y1, x2, y2 float64 }

type Glyph struct {
	Stroke float64
	Ring   bool
	Arrows []segment
}

const (
	ringRadius = 34.0
	center     = 50.0
)

var (
	baseColor  = color.NRGBA{0x0f, 0x76, 0x6e, 0xff}
	glyphWhite = color.NRGBA{0xff, 0xff, 0xff, 0xff}
	inkShadow  = color.NRGBA{0x17, 0x2b, 0x3a, 0xff}

	arrowsLarge = []segment{
		{31.5, 42.6, 68.5, 42.6}, {68.5, 42.6, 58.9, 33.0},
		{68.5, 57.4, 31.5, 57.4}, {31.5, 57.4, 41.1, 67.0},
	}
	arrowsSmall = []segment{
		{22, 38, 78, 38}, {78, 38, 65, 25},
		{78, 62, 22, 62}, {22, 62, 35, 75},
	}
)

func GlyphFor(size int) Glyph {
	switch {
	case size >= 64:
		return Glyph{Stroke: 6, Ring: true, Arrows: arrowsLarge}
	case size > 16:
		return Glyph{Stroke: 9, Ring: true, Arrows: arrowsLarge}
	default:
		return Glyph{Stroke: 12, Arrows: arrowsSmall}
	}
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

// 图形占画布内以 (ox,oy) 为左上角、边长 side 的正方形；px,py 是像素中心。
func coverage(g Glyph, px, py, ox, oy, side float64) float64 {
	x := (px - ox) / side * 100
	y := (py - oy) / side * 100
	d := math.Inf(1)
	if g.Ring {
		d = math.Abs(math.Hypot(x-center, y-center) - ringRadius)
	}
	for _, s := range g.Arrows {
		d = math.Min(d, distToSegment(x, y, s))
	}
	return clamp01(0.5 + (g.Stroke/2-d)*side/100)
}

func distToSegment(x, y float64, s segment) float64 {
	dx, dy := s.x2-s.x1, s.y2-s.y1
	t := clamp01(((x-s.x1)*dx + (y-s.y1)*dy) / (dx*dx + dy*dy))
	return math.Hypot(x-(s.x1+t*dx), y-(s.y1+t*dy))
}

func roundedRectCoverage(px, py, x0, y0, side, radius float64) float64 {
	half := side / 2
	qx := math.Abs(px-(x0+half)) - (half - radius)
	qy := math.Abs(py-(y0+half)) - (half - radius)
	sd := math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - radius
	return clamp01(0.5 - sd)
}

func renderPlate(size int, x0, side, radius, glyphSide float64) *image.NRGBA {
	g := GlyphFor(size)
	gx := x0 + (side-glyphSide)/2
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			a := roundedRectCoverage(px, py, x0, x0, side, radius)
			if a == 0 {
				continue
			}
			c := coverage(g, px, py, gx, gx, glyphSide)
			img.SetNRGBA(x, y, color.NRGBA{
				R: mix(baseColor.R, glyphWhite.R, c),
				G: mix(baseColor.G, glyphWhite.G, c),
				B: mix(baseColor.B, glyphWhite.B, c),
				A: uint8(math.Round(a * 255)),
			})
		}
	}
	return img
}

func mix(a, b uint8, t float64) uint8 {
	return uint8(math.Round(float64(a)*(1-t) + float64(b)*t))
}

func RenderAppIconMac(size int) *image.NRGBA {
	margin := float64(size) * 100 / 1024
	switch size {
	case 16:
		margin = 1
	case 32:
		margin = 2
	}
	side := float64(size) - 2*margin
	plate := renderPlate(size, margin, side, side*184/824, side)
	if size < 64 {
		return plate
	}
	out := dropShadow(plate)
	draw.Draw(out, out.Bounds(), plate, image.Point{}, draw.Over)
	return out
}

// 偏移 size/64，高斯标准差 size/32，即 CSS drop-shadow 的第三个值；最大不透明度 0x38
func dropShadow(plate *image.NRGBA) *image.NRGBA {
	size := plate.Bounds().Dx()
	offset := size / 64
	alpha := make([]float64, size*size)
	for y := offset; y < size; y++ {
		for x := 0; x < size; x++ {
			alpha[y*size+x] = float64(plate.Pix[plate.PixOffset(x, y-offset)+3]) / 255
		}
	}
	kernel := gaussianKernel(float64(size) / 32)
	tmp := make([]float64, size*size)
	gaussBlur(alpha, tmp, size, kernel, true)
	gaussBlur(tmp, alpha, size, kernel, false)
	out := image.NewNRGBA(plate.Bounds())
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			a := uint8(math.Round(alpha[y*size+x] * 0x38))
			if a > 0 {
				out.SetNRGBA(x, y, color.NRGBA{inkShadow.R, inkShadow.G, inkShadow.B, a})
			}
		}
	}
	return out
}

func gaussianKernel(sigma float64) []float64 {
	r := int(math.Ceil(3 * sigma))
	k := make([]float64, 2*r+1)
	var sum float64
	for i := range k {
		d := float64(i - r)
		k[i] = math.Exp(-d * d / (2 * sigma * sigma))
		sum += k[i]
	}
	for i := range k {
		k[i] /= sum
	}
	return k
}

func gaussBlur(src, dst []float64, size int, kernel []float64, horizontal bool) {
	r := len(kernel) / 2
	for line := 0; line < size; line++ {
		for i := 0; i < size; i++ {
			var v float64
			for t, w := range kernel {
				j := i + t - r
				if j < 0 || j >= size {
					continue
				}
				if horizontal {
					v += w * src[line*size+j]
				} else {
					v += w * src[j*size+line]
				}
			}
			if horizontal {
				dst[line*size+i] = v
			} else {
				dst[i*size+line] = v
			}
		}
	}
}

func RenderAppIconWin(size int) *image.NRGBA {
	s := float64(size)
	return renderPlate(size, 0, s, s*48/256, s*0.88)
}

func RenderMono(size int, c color.NRGBA) *image.NRGBA {
	g := GlyphFor(size)
	side := float64(size)
	if size > 16 {
		side *= 1.2
	}
	o := (float64(size) - side) / 2
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			a := coverage(g, float64(x)+0.5, float64(y)+0.5, o, o, side)
			if a == 0 {
				continue
			}
			img.SetNRGBA(x, y, color.NRGBA{c.R, c.G, c.B, uint8(math.Round(a * 255))})
		}
	}
	return img
}
