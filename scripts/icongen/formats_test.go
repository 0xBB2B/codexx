package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"testing"
)

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

	t.Run("条目数等于尺寸数且长度自洽", func(t *testing.T) {
		t.Parallel()
		count := 0
		for off := 8; off < len(out); count++ {
			if off+8 > len(out) {
				t.Fatalf("条目头越界 off=%d", off)
			}
			n := int(binary.BigEndian.Uint32(out[off+4 : off+8]))
			if n < 8 || off+n > len(out) {
				t.Fatalf("条目长度非法 off=%d n=%d", off, n)
			}
			off += n
		}
		if count != len(sizes) {
			t.Errorf("条目数 = %d, want %d", count, len(sizes))
		}
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
