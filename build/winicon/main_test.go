package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// TestBuild parses the object back with debug/pe and walks the resource tree to the icon group (ID 3).
func TestBuild(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	for i := range src.Pix {
		src.Pix[i] = 0xff
	}
	src.Set(0, 0, color.NRGBA{1, 2, 3, 255})
	obj, err := build(src)
	if err != nil {
		t.Fatal(err)
	}
	f, err := pe.NewFile(bytes.NewReader(obj))
	if err != nil {
		t.Fatal(err)
	}
	if f.Machine != pe.IMAGE_FILE_MACHINE_AMD64 || len(f.Sections) != 1 || f.Sections[0].Name != ".rsrc" {
		t.Fatalf("header: machine %#x, sections %d", f.Machine, len(f.Sections))
	}
	s := f.Sections[0]
	if len(s.Relocs) != len(sizes)+1 || len(f.Symbols) != 1 || f.Symbols[0].Name != ".rsrc" {
		t.Fatalf("relocs %d, symbols %d", len(s.Relocs), len(f.Symbols))
	}
	sec, _ := s.Data()
	u32 := func(at uint32) uint32 { return binary.LittleEndian.Uint32(sec[at:]) }
	u16 := func(at uint32) uint16 { return binary.LittleEndian.Uint16(sec[at:]) }
	// root → second type (RT_GROUP_ICON) → its only id → its only language → data entry
	if u16(14) != 2 || u32(16) != rtIcon || u32(24) != rtGroupIcon {
		t.Fatalf("root dir: %d types", u16(14))
	}
	ids := u32(28) &^ (1 << 31)
	if u16(ids+14) != 1 || u32(ids+16) != groupID {
		t.Fatalf("group id %d", u32(ids+16))
	}
	lang := u32(ids+20) &^ (1 << 31)
	e := u32(lang + 20)
	group := sec[u32(e) : u32(e)+u32(e+4)]
	if binary.LittleEndian.Uint16(group[4:]) != uint16(len(sizes)) || group[6] != 0 { // first entry 256 px → 0
		t.Fatalf("group: %x", group[:8])
	}
	// the first RT_ICON data is the 256 px PNG, unscaled
	icons := u32(20) &^ (1 << 31)
	lang = u32(icons+20) &^ (1 << 31)
	e = u32(lang + 20)
	img, err := png.Decode(bytes.NewReader(sec[u32(e) : u32(e)+u32(e+4)]))
	if err != nil || img.Bounds().Dx() != 256 {
		t.Fatalf("icon 1: %v", err)
	}
	if r, g, b, _ := img.At(0, 0).RGBA(); r>>8 != 1 || g>>8 != 2 || b>>8 != 3 {
		t.Fatalf("pixel %d %d %d", r>>8, g>>8, b>>8)
	}
	if got := scale(src, 16).Bounds().Dx(); got != 16 {
		t.Fatalf("scale %d", got)
	}
}
