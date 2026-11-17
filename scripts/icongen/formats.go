package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"slices"
)

var icnsTypes = []struct {
	size int
	typ  string
}{
	{16, "icp4"}, {32, "ic11"}, {32, "icp5"}, {64, "ic12"}, {128, "ic07"},
	{256, "ic13"}, {256, "ic08"}, {512, "ic14"}, {512, "ic09"}, {1024, "ic10"},
}

func WriteICNS(w io.Writer, pngs map[int][]byte) error {
	var body bytes.Buffer
	for _, e := range icnsTypes {
		data, ok := pngs[e.size]
		if !ok {
			continue
		}
		body.WriteString(e.typ)
		binary.Write(&body, binary.BigEndian, uint32(8+len(data)))
		body.Write(data)
	}
	var out bytes.Buffer
	out.WriteString("icns")
	binary.Write(&out, binary.BigEndian, uint32(8+body.Len()))
	out.Write(body.Bytes())
	_, err := w.Write(out.Bytes())
	return err
}

func WriteICO(w io.Writer, pngs map[int][]byte) error {
	sizes := make([]int, 0, len(pngs))
	for s := range pngs {
		sizes = append(sizes, s)
	}
	slices.Sort(sizes)

	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, [3]uint16{0, 1, uint16(len(sizes))})
	offset := 6 + 16*len(sizes)
	for _, s := range sizes {
		dim := byte(s)
		if s >= 256 {
			dim = 0
		}
		out.Write([]byte{dim, dim, 0, 0})
		binary.Write(&out, binary.LittleEndian, [2]uint16{1, 32})
		binary.Write(&out, binary.LittleEndian, [2]uint32{uint32(len(pngs[s])), uint32(offset)})
		offset += len(pngs[s])
	}
	for _, s := range sizes {
		out.Write(pngs[s])
	}
	_, err := w.Write(out.Bytes())
	return err
}
