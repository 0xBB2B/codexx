package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"
)

func encode(img image.Image) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		log.Fatal(err)
	}
	return buf.Bytes()
}

func renderPNGs(sizes []int, render func(int) *image.NRGBA) map[int][]byte {
	m := make(map[int][]byte, len(sizes))
	for _, s := range sizes {
		m[s] = encode(render(s))
	}
	return m
}

func write(root, rel string, data []byte) {
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Fatal(err)
	}
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("用法: icongen <仓库根>")
	}
	root := os.Args[1]
	black := color.NRGBA{0, 0, 0, 0xff}

	var icns bytes.Buffer
	if err := WriteICNS(&icns, renderPNGs([]int{16, 32, 64, 128, 256, 512, 1024}, RenderAppIconMac)); err != nil {
		log.Fatal(err)
	}
	write(root, "macos/Resources/AppIcon.icns", icns.Bytes())
	write(root, "macos/Resources/MenuBarIcon.png", encode(RenderMono(18, black)))
	write(root, "macos/Resources/MenuBarIcon@2x.png", encode(RenderMono(36, black)))

	icos := []struct {
		name   string
		sizes  []int
		render func(int) *image.NRGBA
	}{
		{"AppIcon.ico", []int{16, 24, 32, 48, 256}, RenderAppIconWin},
		{"TrayIconLight.ico", []int{16, 32}, func(s int) *image.NRGBA { return RenderMono(s, color.NRGBA{0x17, 0x2b, 0x3a, 0xff}) }},
		{"TrayIconDark.ico", []int{16, 32}, func(s int) *image.NRGBA { return RenderMono(s, color.NRGBA{0xff, 0xff, 0xff, 0xff}) }},
	}
	for _, i := range icos {
		var buf bytes.Buffer
		if err := WriteICO(&buf, renderPNGs(i.sizes, i.render)); err != nil {
			log.Fatal(err)
		}
		write(root, "windows/ModelBridge.App/Assets/"+i.name, buf.Bytes())
	}
	fmt.Println("图标已生成")
}
