// Command winicon writes a Windows amd64 resource object (.syso) holding the app icon, so the Go linker embeds it
// in the .exe (Explorer, taskbar and the window's title bar). Pure Go, no windres/go-winres: the icon group is
// resource ID 3, the one Wails loads for its windows.
//
// Usage: go run ./build/winicon -icon logo.png -out rsrc_windows_amd64.syso
//
// Taken from the sibling project goXterm (same author, MIT).
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
)

const (
	rtIcon      = 3
	rtGroupIcon = 14
	groupID     = 3 // Wails: NewIconFromResource(module, 3)
	langEnUS    = 0x0409
)

// sizes are the icon images written, largest first; Windows picks the closest one per DPI.
var sizes = []int{256, 64, 48, 32, 24, 16}

func main() {
	icon := flag.String("icon", "", "square PNG (256 px)")
	out := flag.String("out", "rsrc_windows_amd64.syso", "output .syso")
	flag.Parse()
	f, err := os.Open(*icon)
	if err != nil {
		log.Fatal(err)
	}
	src, err := png.Decode(f)
	f.Close()
	if err != nil {
		log.Fatal(err)
	}
	syso, err := build(src)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, syso, 0o644); err != nil {
		log.Fatal(err)
	}
}

type resource struct {
	typ, id uint16
	data    []byte
}

// build returns the COFF object: one RT_ICON per size (PNG-compressed, Vista+) and their RT_GROUP_ICON.
func build(src image.Image) ([]byte, error) {
	var res []resource
	group := new(bytes.Buffer)
	le(group, uint16(0), uint16(1), uint16(len(sizes))) // GRPICONDIR: reserved, type 1 (icon), count
	for i, s := range sizes {
		var b bytes.Buffer
		if err := png.Encode(&b, scale(src, s)); err != nil {
			return nil, err
		}
		id := uint16(i + 1)
		res = append(res, resource{rtIcon, id, b.Bytes()})
		// GRPICONDIRENTRY: width/height (0 = 256), colors, reserved, planes, bit count, bytes, id
		le(group, uint8(s), uint8(s), uint8(0), uint8(0), uint16(1), uint16(32), uint32(b.Len()), id)
	}
	res = append(res, resource{rtGroupIcon, groupID, group.Bytes()})
	return coff(res), nil
}

// scale shrinks src to n×n by averaging the source pixels under each target pixel.
func scale(src image.Image, n int) image.Image {
	sb := src.Bounds()
	if sb.Dx() == n && sb.Dy() == n {
		return src
	}
	dst := image.NewNRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		y0, y1 := sb.Min.Y+y*sb.Dy()/n, sb.Min.Y+max((y+1)*sb.Dy()/n, y*sb.Dy()/n+1)
		for x := 0; x < n; x++ {
			x0, x1 := sb.Min.X+x*sb.Dx()/n, sb.Min.X+max((x+1)*sb.Dx()/n, x*sb.Dx()/n+1)
			var r, g, b, a, c uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					pr, pg, pb, pa := src.At(sx, sy).RGBA() // premultiplied
					r, g, b, a, c = r+uint64(pr), g+uint64(pg), b+uint64(pb), a+uint64(pa), c+1
				}
			}
			dst.Set(x, y, color.RGBA64{uint16(r / c), uint16(g / c), uint16(b / c), uint16(a / c)})
		}
	}
	return dst
}

// coff lays out the .rsrc section (type → id → language directories, data entries, data) and wraps it in a COFF
// object with one ADDR32NB relocation per data entry, which the Go linker resolves to image-relative offsets.
func coff(res []resource) []byte {
	// res is already ordered by type (RT_ICON 3 before RT_GROUP_ICON 14) and by id within a type.
	var types []uint16
	for _, r := range res {
		if len(types) == 0 || types[len(types)-1] != r.typ {
			types = append(types, r.typ)
		}
	}
	off := 16 + 8*len(types)
	typeDir := map[uint16]int{}
	for _, t := range types {
		typeDir[t] = off
		n := 0
		for _, r := range res {
			if r.typ == t {
				n++
			}
		}
		off += 16 + 8*n
	}
	langDir, entry, data := make([]int, len(res)), make([]int, len(res)), make([]int, len(res))
	for i := range res {
		langDir[i], off = off, off+16+8
	}
	for i := range res {
		entry[i], off = off, off+16
	}
	for i, r := range res {
		off = (off + 7) &^ 7
		data[i], off = off, off+len(r.data)
	}
	sec := make([]byte, (off+7)&^7)
	dir := func(at, n int) { binary.LittleEndian.PutUint16(sec[at+14:], uint16(n)) } // NumberOfIdEntries
	put := func(at int, id uint32, to int, sub bool) {
		binary.LittleEndian.PutUint32(sec[at:], id)
		if sub {
			to |= 1 << 31
		}
		binary.LittleEndian.PutUint32(sec[at+4:], uint32(to))
	}
	dir(0, len(types))
	for ti, t := range types {
		put(16+8*ti, uint32(t), typeDir[t], true)
		k := 0
		for i, r := range res {
			if r.typ == t {
				put(typeDir[t]+16+8*k, uint32(r.id), langDir[i], true)
				k++
			}
		}
		dir(typeDir[t], k)
	}
	for i, r := range res {
		dir(langDir[i], 1)
		put(langDir[i]+16, langEnUS, entry[i], false)
		binary.LittleEndian.PutUint32(sec[entry[i]:], uint32(data[i])) // addend; + section RVA at link time
		binary.LittleEndian.PutUint32(sec[entry[i]+4:], uint32(len(r.data)))
		copy(sec[data[i]:], r.data)
	}

	const hdr, shdr = 20, 40
	relocAt := hdr + shdr + len(sec)
	symAt := relocAt + 10*len(res)
	b := new(bytes.Buffer)
	// IMAGE_FILE_HEADER: machine AMD64, 1 section, timestamp, symbol table, 1 symbol, no optional header, flags
	le(b, uint16(0x8664), uint16(1), uint32(0), uint32(symAt), uint32(1), uint16(0), uint16(0))
	// IMAGE_SECTION_HEADER .rsrc: initialized data, readable
	b.WriteString(".rsrc\x00\x00\x00")
	le(b, uint32(0), uint32(0), uint32(len(sec)), uint32(hdr+shdr), uint32(relocAt), uint32(0),
		uint16(len(res)), uint16(0), uint32(0x40000040))
	b.Write(sec)
	for i := range res {
		le(b, uint32(entry[i]), uint32(0), uint16(3)) // IMAGE_REL_AMD64_ADDR32NB against symbol 0
	}
	// symbol 0: .rsrc, value 0, section 1, type 0, IMAGE_SYM_CLASS_STATIC, no aux; then an empty string table
	b.WriteString(".rsrc\x00\x00\x00")
	le(b, uint32(0), int16(1), uint16(0), uint8(3), uint8(0), uint32(4))
	return b.Bytes()
}

func le(b *bytes.Buffer, v ...any) {
	for _, x := range v {
		_ = binary.Write(b, binary.LittleEndian, x)
	}
}
