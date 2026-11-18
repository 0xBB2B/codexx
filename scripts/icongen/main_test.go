package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func readICOFrame(t *testing.T, path string, size int) *image.NRGBA {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	for i := 0; i < count; i++ {
		e := data[6+16*i:]
		if int(e[0]) != size {
			continue
		}
		n := binary.LittleEndian.Uint32(e[8:12])
		off := binary.LittleEndian.Uint32(e[12:16])
		return decodeNRGBA(t, data[off:off+n])
	}
	t.Fatalf("%s 没有 %dpx 一档", path, size)
	return nil
}

func decodeNRGBA(t *testing.T, b []byte) *image.NRGBA {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	out, ok := img.(*image.NRGBA)
	if !ok {
		t.Fatalf("PNG 不是 NRGBA: %T", img)
	}
	return out
}

func TestMain_GeneratedMonoIcons(t *testing.T) {
	root := t.TempDir()
	oldArgs := os.Args
	os.Args = []string{"icongen", root}
	main()
	os.Args = oldArgs

	assets := filepath.Join(root, "windows/ModelBridge.App/Assets")
	trays := []struct {
		file string
		c    color.NRGBA
	}{
		{"TrayIconLight.ico", ink},
		{"TrayIconDark.ico", white},
	}
	for _, tr := range trays {
		t.Run("托盘 32 放大 1.2 倍 "+tr.file, func(t *testing.T) {
			checkPixels(t, readICOFrame(t, filepath.Join(assets, tr.file), 32), 32, []pixelWant{
				{"放大后外圈右侧不透明", 29, 16, false, []color.NRGBA{tr.c}},
				{"放大后外圈上方不透明", 16, 2, false, []color.NRGBA{tr.c}},
				{"放大后外圈下方不透明", 16, 29, false, []color.NRGBA{tr.c}},
				{"未放大外圈右侧位置透明", 26, 16, true, nil},
				{"未放大外圈上方位置透明", 16, 5, true, nil},
				{"未放大外圈下方位置透明", 16, 26, true, nil},
			})
		})
		t.Run("托盘 16 无外圈 "+tr.file, func(t *testing.T) {
			checkPixels(t, readICOFrame(t, filepath.Join(assets, tr.file), 16), 16, []pixelWant{
				{"外圈上方透明", 8, 2, true, nil},
				{"上箭头横线不透明", 8, 6, false, []color.NRGBA{tr.c}},
			})
		})
	}

	t.Run("菜单栏 18 放大 1.2 倍", func(t *testing.T) {
		b, err := os.ReadFile(filepath.Join(root, "macos/Resources/MenuBarIcon.png"))
		if err != nil {
			t.Fatal(err)
		}
		checkPixels(t, decodeNRGBA(t, b), 18, []pixelWant{
			{"放大后外圈右侧不透明", 16, 8, false, []color.NRGBA{{0, 0, 0, 0xff}}},
			{"放大后外圈上方不透明", 8, 1, false, []color.NRGBA{{0, 0, 0, 0xff}}},
			{"未放大外圈右侧位置透明", 14, 8, true, nil},
			{"未放大外圈上方位置透明", 8, 3, true, nil},
		})
	})
	t.Run("菜单栏 36 放大 1.2 倍", func(t *testing.T) {
		b, err := os.ReadFile(filepath.Join(root, "macos/Resources/MenuBarIcon@2x.png"))
		if err != nil {
			t.Fatal(err)
		}
		checkPixels(t, decodeNRGBA(t, b), 36, []pixelWant{
			{"放大后外圈右侧不透明", 32, 17, false, []color.NRGBA{{0, 0, 0, 0xff}}},
			{"放大后外圈上方不透明", 17, 3, false, []color.NRGBA{{0, 0, 0, 0xff}}},
			{"未放大外圈右侧位置透明", 29, 17, true, nil},
			{"未放大外圈上方位置透明", 17, 6, true, nil},
		})
	})
}
