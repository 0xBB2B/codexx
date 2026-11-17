package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"math"
	"testing"
)

var (
	teal  = color.NRGBA{0x0f, 0x76, 0x6e, 0xff}
	white = color.NRGBA{0xff, 0xff, 0xff, 0xff}
	ink   = color.NRGBA{0x17, 0x2b, 0x3a, 0xff}
)

type pixelWant struct {
	name        string
	x, y        int
	transparent bool
	anyOf       []color.NRGBA
}

func near(a, b color.NRGBA) bool {
	d := func(x, y uint8) bool {
		if x > y {
			return x-y <= 2
		}
		return y-x <= 2
	}
	return d(a.R, b.R) && d(a.G, b.G) && d(a.B, b.B) && d(a.A, b.A)
}

func checkPixels(t *testing.T, img *image.NRGBA, size int, wants []pixelWant) {
	t.Helper()
	if got := img.Bounds(); got.Dx() != size || got.Dy() != size {
		t.Fatalf("bounds = %v, want %dx%d", got, size, size)
	}
	for _, w := range wants {
		t.Run(w.name, func(t *testing.T) {
			t.Parallel()
			got := img.NRGBAAt(w.x, w.y)
			if w.transparent {
				if got.A != 0 {
					t.Errorf("(%d,%d) = %v, want alpha 0", w.x, w.y, got)
				}
				return
			}
			for _, c := range w.anyOf {
				if near(got, c) {
					return
				}
			}
			t.Errorf("(%d,%d) = %v, want one of %v", w.x, w.y, got, w.anyOf)
		})
	}
}

func TestRenderAppIconMac(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		size  int
		wants []pixelWant
	}{
		{"1024", 1024, []pixelWant{
			{"外围留白远角透明", 2, 2, true, nil},
			{"左边底板内为底板色", 150, 512, false, []color.NRGBA{teal}},
			{"圆心位置为图形色或底板色", 512, 512, false, []color.NRGBA{white, teal}},
			{"外圈笔画为白色", 512, 232, false, []color.NRGBA{white}},
		}},
		{"32 四周留 2px", 32, []pixelWant{
			{"左外侧 1px 透明", 1, 16, true, nil},
			{"底板内为底板色", 4, 16, false, []color.NRGBA{teal}},
		}},
		{"16 四周留 1px", 16, []pixelWant{
			{"最外侧 1px 透明", 0, 8, true, nil},
			{"底板内为底板色", 2, 8, false, []color.NRGBA{teal}},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			checkPixels(t, RenderAppIconMac(tt.size), tt.size, tt.wants)
		})
	}
}

func TestRenderAppIconMac_ShadowBlurSigma(t *testing.T) {
	t.Parallel()
	const (
		size     = 1024
		peak     = 0x38
		wantSig  = 16.0
		edgeY    = 100 + 824 + 16
		halfTol  = 2
		sigmaTol = 0.10
	)
	img := RenderAppIconMac(size)

	var tail float64
	for y := edgeY; y < size; y++ {
		tail += float64(img.NRGBAAt(512, y).A)
	}
	// 高斯边缘外侧的 alpha 之和 = 峰值 * sigma / sqrt(2π)
	got := tail / peak * math.Sqrt(2*math.Pi)
	if math.Abs(got-wantSig) > wantSig*sigmaTol {
		t.Errorf("估得 sigma = %.2f, want %.1f ±%.0f%%", got, wantSig, sigmaTol*100)
	}

	half := -1
	for y := 924; y < size; y++ {
		if float64(img.NRGBAAt(512, y).A) <= peak/2 {
			half = y
			break
		}
	}
	if half < 0 || math.Abs(float64(half-edgeY)) > halfTol {
		t.Errorf("alpha 降到峰值一半的位置 y = %d, want %d ±%d（偏移 size/64）", half, edgeY, halfTol)
	}
}

func TestRenderAppIconMac_Shadow(t *testing.T) {
	t.Parallel()
	const maxAlpha = 0x38 + 2
	tests := []struct {
		name         string
		size         int
		x, below, up int
	}{
		{"1024", 1024, 512, 930, 93},
		{"64", 64, 32, 60, 3},
		{"128", 128, 64, 120, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			img := RenderAppIconMac(tt.size)
			below := img.NRGBAAt(tt.x, tt.below)
			up := img.NRGBAAt(tt.x, tt.up)
			if below.A == 0 {
				t.Fatalf("底板正下方 (%d,%d) alpha = 0, want 投影", tt.x, tt.below)
			}
			if below.A > maxAlpha {
				t.Errorf("投影 alpha = %d, 不应超过 %d", below.A, maxAlpha)
			}
			if up.A >= below.A {
				t.Errorf("底板正上方 alpha = %d, 应小于正下方 %d（投影向下偏移）", up.A, below.A)
			}
		})
	}

	t.Run("投影底色为 #172b3a", func(t *testing.T) {
		t.Parallel()
		img := RenderAppIconMac(1024)
		p := img.NRGBAAt(512, 930)
		if !near(color.NRGBA{p.R, p.G, p.B, 0xff}, ink) {
			t.Errorf("rgb = %v, want %v", p, ink)
		}
	})

	t.Run("小于 64 像素无投影", func(t *testing.T) {
		t.Parallel()
		for _, c := range []struct{ size, x, y0, y1 int }{{32, 16, 30, 31}, {48, 24, 44, 47}, {16, 8, 15, 15}} {
			img := RenderAppIconMac(c.size)
			for y := c.y0; y <= c.y1; y++ {
				if a := img.NRGBAAt(c.x, y).A; a != 0 {
					t.Errorf("size %d (%d,%d) alpha = %d, want 0", c.size, c.x, y, a)
				}
			}
		}
	})
}

func TestRenderAppIconWin(t *testing.T) {
	t.Parallel()
	checkPixels(t, RenderAppIconWin(256), 256, []pixelWant{
		{"圆角外透明", 2, 2, true, nil},
		{"顶边中点铺满为底板色", 128, 4, false, []color.NRGBA{teal}},
		{"两箭头之间为底板色", 128, 128, false, []color.NRGBA{teal}},
		{"外圈笔画为白色", 128, 51, false, []color.NRGBA{white}},
	})
}

func TestRenderMono(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		size  int
		wants []pixelWant
	}{
		{"16 无外圈", 16, []pixelWant{
			{"外圈上方透明", 8, 2, true, nil},
			{"外圈下方透明", 8, 13, true, nil},
			{"外圈左上透明", 4, 4, true, nil},
			{"外圈右下透明", 11, 11, true, nil},
			{"上箭头横线不透明", 8, 6, false, []color.NRGBA{ink}},
			{"下箭头横线不透明", 8, 9, false, []color.NRGBA{ink}},
		}},
		{"64 放大 1.2 倍有外圈", 64, []pixelWant{
			{"放大后外圈上方不透明", 31, 5, false, []color.NRGBA{ink}},
			{"放大后外圈下方不透明", 31, 58, false, []color.NRGBA{ink}},
			{"放大后外圈左侧不透明", 5, 31, false, []color.NRGBA{ink}},
			{"放大后外圈右侧不透明", 58, 31, false, []color.NRGBA{ink}},
			{"未放大外圈上方位置透明", 31, 10, true, nil},
			{"未放大外圈右侧位置透明", 53, 31, true, nil},
			{"上箭头横线不透明", 32, 27, false, []color.NRGBA{ink}},
			{"无底板：角落透明", 0, 0, true, nil},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			checkPixels(t, RenderMono(tt.size, ink), tt.size, tt.wants)
		})
	}
}

func TestRenderMono_OpaquePixelsUseGivenColor(t *testing.T) {
	t.Parallel()
	for _, size := range []int{16, 32, 64} {
		img := RenderMono(size, ink)
		drawn := 0
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				p := img.NRGBAAt(x, y)
				if p.A == 0 {
					continue
				}
				drawn++
				if !near(color.NRGBA{p.R, p.G, p.B, 0xff}, ink) {
					t.Fatalf("size %d (%d,%d) rgb = %v, want %v", size, x, y, p, ink)
				}
			}
		}
		if drawn == 0 {
			t.Errorf("size %d: 没有任何可见像素", size)
		}
	}
}

func TestRender_Deterministic(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		render func() *image.NRGBA
	}{
		{"mac", func() *image.NRGBA { return RenderAppIconMac(128) }},
		{"win", func() *image.NRGBA { return RenderAppIconWin(128) }},
		{"mono", func() *image.NRGBA { return RenderMono(48, ink) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !bytes.Equal(tt.render().Pix, tt.render().Pix) {
				t.Error("两次渲染结果不同")
			}
		})
	}
}

func TestRenderMono_ScaledUp(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		size  int
		wants []pixelWant
	}{
		{"18 图形放大 1.2 倍", 18, []pixelWant{
			{"放大后外圈右侧不透明", 16, 8, false, []color.NRGBA{ink}},
			{"放大后外圈左侧不透明", 1, 8, false, []color.NRGBA{ink}},
			{"放大后外圈上方不透明", 8, 1, false, []color.NRGBA{ink}},
			{"放大后外圈下方不透明", 8, 16, false, []color.NRGBA{ink}},
			{"未放大外圈位置透明", 8, 3, true, nil},
			{"未放大外圈右侧位置透明", 14, 8, true, nil},
		}},
		{"36 图形放大 1.2 倍", 36, []pixelWant{
			{"放大后外圈右侧不透明", 32, 17, false, []color.NRGBA{ink}},
			{"放大后外圈左侧不透明", 3, 17, false, []color.NRGBA{ink}},
			{"放大后外圈上方不透明", 17, 3, false, []color.NRGBA{ink}},
			{"放大后外圈下方不透明", 17, 32, false, []color.NRGBA{ink}},
			{"未放大外圈上方位置透明", 17, 6, true, nil},
			{"未放大外圈右侧位置透明", 29, 17, true, nil},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			checkPixels(t, RenderMono(tt.size, ink), tt.size, tt.wants)
		})
	}
}

func whiteCoverage(p color.NRGBA) float64 {
	return float64(int(p.R)-int(teal.R)) / float64(255-int(teal.R))
}

func TestRenderAppIconMac_64(t *testing.T) {
	t.Parallel()
	checkPixels(t, RenderAppIconMac(64), 64, []pixelWant{
		{"左上角透明", 0, 0, true, nil},
		{"右上角透明", 63, 0, true, nil},
		{"左下角透明", 0, 63, true, nil},
		{"右下角透明", 63, 63, true, nil},
		{"左侧留白透明", 1, 32, true, nil},
		{"底板内为底板色", 8, 32, false, []color.NRGBA{teal}},
	})
}

func TestRender_StrokeWidth(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want float64
		tol  float64
		got  func() float64
	}{
		{"mac 1024 外圈线宽 6% 图形尺度", 0.06 * 824, 3, func() float64 {
			img := RenderAppIconMac(1024)
			sum := 0.0
			for x := 700; x < 900; x++ {
				sum += whiteCoverage(img.NRGBAAt(x, 512))
			}
			return sum
		}},
		{"win 32 箭头横线线宽 9% 图形尺度", 0.09 * 32 * 0.88, 1, func() float64 {
			img := RenderAppIconWin(32)
			sum := 0.0
			for y := 10; y < 16; y++ {
				sum += whiteCoverage(img.NRGBAAt(16, y))
			}
			return sum
		}},
		{"mono 16 箭头横线线宽 12%", 0.12 * 16, 1, func() float64 {
			img := RenderMono(16, ink)
			sum := 0.0
			for y := 3; y < 8; y++ {
				sum += float64(img.NRGBAAt(8, y).A) / 255
			}
			return sum
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.got(); got < tt.want-tt.tol || got > tt.want+tt.tol {
				t.Errorf("线宽 = %.2f, want %.2f ± %.0f", got, tt.want, tt.tol)
			}
		})
	}
}

func TestRenderAppIconWin_GraphicScale(t *testing.T) {
	t.Parallel()
	tests := []struct {
		size   int
		stroke float64
	}{
		{24, 0.09},
		{32, 0.09},
		{48, 0.09},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d", tt.size), func(t *testing.T) {
			t.Parallel()
			img := RenderAppIconWin(tt.size)
			edge := 0.0
			for x := 0; x < tt.size/2; x++ {
				c := whiteCoverage(img.NRGBAAt(x, tt.size/2))
				if c >= 0.99 {
					break
				}
				edge += 1 - c
			}
			s := float64(tt.size)
			want := s * (0.06 + 0.88*(0.16-tt.stroke/2))
			if edge < want-1 || edge > want+1 {
				t.Errorf("外圈最外点离画布左边 = %.2f, want %.2f ± 1", edge, want)
			}
		})
	}
}
