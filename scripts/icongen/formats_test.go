package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

var wantIconset = map[string]int{
	"icon_16x16.png":      16,
	"icon_16x16@2x.png":   32,
	"icon_32x32.png":      32,
	"icon_32x32@2x.png":   64,
	"icon_128x128.png":    128,
	"icon_128x128@2x.png": 256,
	"icon_256x256.png":    256,
	"icon_256x256@2x.png": 512,
	"icon_512x512.png":    512,
	"icon_512x512@2x.png": 1024,
}

func requireIconset(t *testing.T, icns string) {
	t.Helper()
	if _, err := exec.LookPath("iconutil"); err != nil {
		t.Skip("iconutil 不存在")
	}
	dir := filepath.Join(t.TempDir(), "x.iconset")
	if out, err := exec.Command("iconutil", "-c", "iconset", icns, "-o", dir).CombinedOutput(); err != nil {
		t.Fatalf("iconutil: %v\n%s", err, out)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, e := range entries {
		got[e.Name()] = true
		want, ok := wantIconset[e.Name()]
		if !ok {
			t.Errorf("多出文件 %s", e.Name())
			continue
		}
		f, err := os.Open(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := png.DecodeConfig(f)
		f.Close()
		if err != nil {
			t.Errorf("%s: %v", e.Name(), err)
			continue
		}
		if cfg.Width != want || cfg.Height != want {
			t.Errorf("%s 像素 = %dx%d, want %d", e.Name(), cfg.Width, cfg.Height, want)
		}
	}
	for name := range wantIconset {
		if !got[name] {
			t.Errorf("缺少文件 %s", name)
		}
	}
}

var pngSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

func encodePNG(t *testing.T, size int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for i := range img.Pix {
		img.Pix[i] = byte(i * 7)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func pngSet(t *testing.T, sizes ...int) map[int][]byte {
	t.Helper()
	m := make(map[int][]byte, len(sizes))
	for _, s := range sizes {
		m[s] = encodePNG(t, s)
	}
	return m
}

func TestWriteICNS(t *testing.T) {
	t.Parallel()
	sizes := []int{16, 32, 64, 128, 256, 512, 1024}
	pngs := pngSet(t, sizes...)
	var buf bytes.Buffer
	if err := WriteICNS(&buf, pngs); err != nil {
		t.Fatalf("WriteICNS: %v", err)
	}
	out := buf.Bytes()

	t.Run("魔数与总长度", func(t *testing.T) {
		t.Parallel()
		if len(out) < 8 || string(out[:4]) != "icns" {
			t.Fatalf("头部 = %q, want icns", out[:min(4, len(out))])
		}
		if got := binary.BigEndian.Uint32(out[4:8]); int(got) != len(out) {
			t.Errorf("长度字段 = %d, want %d", got, len(out))
		}
	})

	t.Run("条目类型码按苹果约定且长度自洽", func(t *testing.T) {
		t.Parallel()
		var types []string
		for off := 8; off < len(out); {
			if off+8 > len(out) {
				t.Fatalf("条目头越界 off=%d", off)
			}
			n := int(binary.BigEndian.Uint32(out[off+4 : off+8]))
			if n < 8 || off+n > len(out) {
				t.Fatalf("条目长度非法 off=%d n=%d", off, n)
			}
			types = append(types, string(out[off:off+4]))
			off += n
		}
		want := []string{"icp4", "ic11", "icp5", "ic12", "ic07", "ic13", "ic08", "ic14", "ic09", "ic10"}
		slices.Sort(types)
		slices.Sort(want)
		if !slices.Equal(types, want) {
			t.Errorf("类型码 = %v, want %v", types, want)
		}
	})

	t.Run("iconutil 解出 10 个文件", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "a.icns")
		if err := os.WriteFile(path, out, 0o644); err != nil {
			t.Fatal(err)
		}
		requireIconset(t, path)
	})

	t.Run("每张 PNG 原样打包", func(t *testing.T) {
		t.Parallel()
		for s, p := range pngs {
			if !bytes.Contains(out, p) {
				t.Errorf("输出里找不到 %d 像素的 PNG", s)
			}
		}
	})
}

func TestCommittedAppIcon(t *testing.T) {
	t.Parallel()
	requireIconset(t, filepath.Join("..", "..", "macos", "Resources", "AppIcon.icns"))
}

func TestWriteICNS_Deterministic(t *testing.T) {
	t.Parallel()
	pngs := pngSet(t, 16, 32, 64, 128, 256, 512, 1024)
	var a, b bytes.Buffer
	if err := WriteICNS(&a, pngs); err != nil {
		t.Fatal(err)
	}
	if err := WriteICNS(&b, pngs); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("两次输出不同")
	}
}

func TestWriteICO(t *testing.T) {
	t.Parallel()
	sizes := []int{16, 24, 32, 48, 256}
	pngs := pngSet(t, sizes...)
	var buf bytes.Buffer
	if err := WriteICO(&buf, pngs); err != nil {
		t.Fatalf("WriteICO: %v", err)
	}
	out := buf.Bytes()
	if len(out) < 6+16*len(sizes) {
		t.Fatalf("输出过短: %d", len(out))
	}

	t.Run("头部", func(t *testing.T) {
		t.Parallel()
		if r := binary.LittleEndian.Uint16(out[0:2]); r != 0 {
			t.Errorf("reserved = %d, want 0", r)
		}
		if ty := binary.LittleEndian.Uint16(out[2:4]); ty != 1 {
			t.Errorf("type = %d, want 1", ty)
		}
		if c := binary.LittleEndian.Uint16(out[4:6]); int(c) != len(sizes) {
			t.Errorf("count = %d, want %d", c, len(sizes))
		}
	})

	t.Run("目录项指向 PNG 数据", func(t *testing.T) {
		t.Parallel()
		seen := map[int]bool{}
		for i := range sizes {
			e := out[6+16*i : 6+16*(i+1)]
			w, h := int(e[0]), int(e[1])
			if w == 0 {
				w = 256
			}
			if h == 0 {
				h = 256
			}
			if w != h {
				t.Errorf("项 %d 宽高不等: %d x %d", i, w, h)
			}
			want, ok := pngs[w]
			if !ok || seen[w] {
				t.Errorf("项 %d 尺寸 %d 不在输入里或重复", i, w)
				continue
			}
			seen[w] = true
			if w == 256 && (e[0] != 0 || e[1] != 0) {
				t.Errorf("256 的宽高字节 = %d,%d, want 0,0", e[0], e[1])
			}
			size := int(binary.LittleEndian.Uint32(e[8:12]))
			off := int(binary.LittleEndian.Uint32(e[12:16]))
			if size != len(want) {
				t.Errorf("%d: 数据长度 = %d, want %d", w, size, len(want))
			}
			if off < 0 || off+size > len(out) {
				t.Errorf("%d: 偏移越界 off=%d size=%d", w, off, size)
				continue
			}
			data := out[off : off+size]
			if !bytes.HasPrefix(data, pngSignature) {
				t.Errorf("%d: 数据不是 PNG 开头", w)
			}
			if !bytes.Equal(data, want) {
				t.Errorf("%d: 数据与输入 PNG 不一致", w)
			}
		}
	})
}

func TestWriteICO_Deterministic(t *testing.T) {
	t.Parallel()
	pngs := pngSet(t, 16, 24, 32, 48, 256)
	var a, b bytes.Buffer
	if err := WriteICO(&a, pngs); err != nil {
		t.Fatal(err)
	}
	if err := WriteICO(&b, pngs); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("两次输出不同")
	}
}
