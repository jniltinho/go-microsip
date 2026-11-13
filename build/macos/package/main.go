// Command package joins the two macOS slices of go-microsip into one universal executable, puts
// it in "go-microsip.app" and zips the bundle, on Linux and without Apple's tools: the fat
// Mach-O, Info.plist, PkgInfo and the icon file are written here. It refuses anything it cannot
// vouch for: an input that is not a macOS executable of the expected kind, a set that is not
// exactly x86_64 and arm64, a slice built from another version or commit. The bundle is unsigned
// beyond the signature the Go linker gives the arm64 slice, whose bytes are kept as they are.
//
// Usage:
//
//	go run ./build/macos/package -version V -commit C -icon icon.png -license LICENSE \
//	    -notices THIRD_PARTY_NOTICES.txt -out app.zip <darwin-amd64> <darwin-arm64>
package main

import (
	"archive/zip"
	"bytes"
	"debug/macho"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"image/png"
	"io"
	"log"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	bundle     = "go-microsip.app/Contents/"
	executable = bundle + "MacOS/go-microsip"
	identifier = "io.github.jniltinho.go-microsip"
	// align is 2^14: the page alignment lipo uses for arm64 and x86_64 slices.
	align = 14

	loadBuildVersion = 0x32 // LC_BUILD_VERSION
	loadVersionMin   = 0x24 // LC_VERSION_MIN_MACOSX
	platformMacOS    = 1
)

func main() {
	version := flag.String("version", "", "the version the slices were built with")
	commit := flag.String("commit", "", "the commit the slices were built from")
	icon := flag.String("icon", "", "a square PNG of 128, 256, 512 or 1024 pixels")
	license := flag.String("license", "", "the licence text")
	notices := flag.String("notices", "", "the third-party notices")
	out := flag.String("out", "", "the .zip to write")
	flag.Parse()
	if *version == "" || *commit == "" || *icon == "" || *license == "" || *notices == "" || *out == "" {
		log.Fatal("usage: package -version V -commit C -icon PNG -license FILE -notices FILE -out ZIP <slice> <slice>")
	}
	read := func(path string) []byte {
		data, err := os.ReadFile(path)
		if err != nil {
			log.Fatal(err)
		}
		return data
	}
	var slices [][]byte
	for _, path := range flag.Args() {
		slices = append(slices, read(path))
	}
	archive, err := build(*version, *commit, slices, read(*icon), read(*license), read(*notices), time.Now())
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, archive, 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s: universal go-microsip.app (%d bytes)\n", *out, len(archive))
}

// build returns the zip of the application bundle, after checking what went into it and what
// came out.
func build(version, commit string, slices [][]byte, icon, license, notices []byte, at time.Time) ([]byte, error) {
	for i, slice := range slices {
		// The linker writes both into the program; a slice of another build has not this pair.
		if !bytes.Contains(slice, []byte(version)) || !bytes.Contains(slice, []byte(commit)) {
			return nil, fmt.Errorf("slice %d was not built as version %s from commit %s", i+1, version, commit)
		}
	}
	fat, minimum, err := universal(slices)
	if err != nil {
		return nil, err
	}
	icons, err := icns(icon)
	if err != nil {
		return nil, err
	}
	files := []entry{
		{bundle + "Info.plist", 0o644, []byte(plist(version, minimum))},
		{bundle + "PkgInfo", 0o644, []byte("APPL????")},
		{executable, 0o755, fat},
		{bundle + "Resources/go-microsip.icns", 0o644, icons},
		{bundle + "Resources/LICENSE.txt", 0o644, license},
		{bundle + "Resources/THIRD_PARTY_NOTICES.txt", 0o644, notices},
	}
	archive, err := zipped(files, at)
	if err != nil {
		return nil, err
	}
	if err := verify(archive, files); err != nil {
		return nil, fmt.Errorf("the zip does not read back as written: %w", err)
	}
	return archive, nil
}

// slice is what one thin executable says of itself.
type slice struct {
	cpu     macho.Cpu
	sub     uint32
	minimum uint32 // the lowest macOS it runs on, as X.Y.Z packed in 16, 8 and 8 bits
}

// inspect reads one input: a thin Mach-O executable for macOS on x86_64 or arm64.
func inspect(data []byte) (slice, error) {
	file, err := macho.NewFile(bytes.NewReader(data))
	if err != nil {
		return slice{}, fmt.Errorf("not a thin Mach-O file: %w", err)
	}
	if file.Type != macho.TypeExec {
		return slice{}, fmt.Errorf("a Mach-O file of type %v, not an executable", file.Type)
	}
	if file.Cpu != macho.CpuAmd64 && file.Cpu != macho.CpuArm64 {
		return slice{}, fmt.Errorf("an executable for %v, not for x86_64 or arm64", file.Cpu)
	}
	found := slice{cpu: file.Cpu, sub: file.SubCpu}
	for _, load := range file.Loads {
		raw := load.Raw()
		if len(raw) < 16 {
			continue
		}
		switch file.ByteOrder.Uint32(raw[0:4]) {
		case loadBuildVersion:
			if file.ByteOrder.Uint32(raw[8:12]) != platformMacOS {
				return slice{}, errors.New("an executable for a platform that is not macOS")
			}
			found.minimum = file.ByteOrder.Uint32(raw[12:16])
		case loadVersionMin:
			found.minimum = file.ByteOrder.Uint32(raw[8:12])
		}
	}
	if found.minimum == 0 {
		return slice{}, errors.New("an executable that does not say which macOS it needs")
	}
	return found, nil
}

// universal joins the thin executables into a fat one, as lipo -create does, and returns it with
// the highest minimum macOS of its slices. Every slice's bytes are in the result unchanged, at an
// aligned offset: the arm64 slice's signature covers those bytes and stays valid.
func universal(slices [][]byte) ([]byte, uint32, error) {
	type placed struct {
		slice
		offset, size uint32
	}
	var (
		parts   []placed
		minimum uint32
		seen    = map[macho.Cpu]bool{}
	)
	offset := uint32(1 << align) // the first slice starts on its own page, after the fat header
	for i, data := range slices {
		found, err := inspect(data)
		if err != nil {
			return nil, 0, fmt.Errorf("slice %d: %w", i+1, err)
		}
		if seen[found.cpu] {
			return nil, 0, fmt.Errorf("slice %d: a second executable for %v", i+1, found.cpu)
		}
		seen[found.cpu] = true
		minimum = max(minimum, found.minimum)
		parts = append(parts, placed{found, offset, uint32(len(data))})
		offset += (uint32(len(data)) + 1<<align - 1) &^ (1<<align - 1)
	}
	if len(parts) != 2 || !seen[macho.CpuAmd64] || !seen[macho.CpuArm64] {
		return nil, 0, errors.New("a universal application needs exactly one x86_64 and one arm64 executable")
	}
	var fat bytes.Buffer
	word := func(values ...uint32) { _ = binary.Write(&fat, binary.BigEndian, values) }
	word(macho.MagicFat, uint32(len(parts)))
	for _, part := range parts {
		word(uint32(part.cpu), part.sub, part.offset, part.size, align)
	}
	for i, data := range slices {
		fat.Write(make([]byte, int(parts[i].offset)-fat.Len()))
		fat.Write(data)
	}
	// Read back: the two architectures, each slice whole and where the header says.
	joined := fat.Bytes()
	back, err := macho.NewFatFile(bytes.NewReader(joined))
	if err != nil {
		return nil, 0, fmt.Errorf("the universal executable does not read back: %w", err)
	}
	if len(back.Arches) != len(parts) {
		return nil, 0, fmt.Errorf("the universal executable holds %d architectures, not %d", len(back.Arches), len(parts))
	}
	for i, arch := range back.Arches {
		end := uint64(arch.Offset) + uint64(arch.Size)
		if arch.Cpu != parts[i].cpu || arch.Offset%(1<<align) != 0 || end > uint64(len(joined)) ||
			!bytes.Equal(joined[arch.Offset:end], slices[i]) {
			return nil, 0, fmt.Errorf("slice %d is not in the universal executable as it was given", i+1)
		}
	}
	return joined, minimum, nil
}

// macOS writes a packed version as "13.0" or "13.2.1".
func macOS(packed uint32) string {
	text := fmt.Sprintf("%d.%d", packed>>16, packed>>8&0xff)
	if patch := packed & 0xff; patch != 0 {
		text += fmt.Sprintf(".%d", patch)
	}
	return text
}

// icns wraps the PNG as an Apple icon file, under the entry of its size.
func icns(image []byte) ([]byte, error) {
	config, err := png.DecodeConfig(bytes.NewReader(image))
	if err != nil {
		return nil, fmt.Errorf("the icon is not a PNG: %w", err)
	}
	kinds := map[int]string{128: "ic07", 256: "ic08", 512: "ic09", 1024: "ic10"}
	kind, ok := kinds[config.Width]
	if !ok || config.Width != config.Height {
		return nil, fmt.Errorf("the icon is %d by %d pixels: it must be a square of 128, 256, 512 or 1024", config.Width, config.Height)
	}
	var file bytes.Buffer
	file.WriteString("icns")
	_ = binary.Write(&file, binary.BigEndian, uint32(16+len(image)))
	file.WriteString(kind)
	_ = binary.Write(&file, binary.BigEndian, uint32(8+len(image)))
	file.Write(image)
	return file.Bytes(), nil
}

// plist is the bundle's information file. macOS takes versions of three numbers: a pre-release
// suffix stays in what `go-microsip version` prints.
func plist(version string, minimum uint32) string {
	numbers, _, _ := strings.Cut(version, "-")
	return strings.NewReplacer("__VERSION__", numbers, "__MINIMUM__", macOS(minimum), "__ID__", identifier).Replace(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key>
	<string>go-microsip</string>
	<key>CFBundleDisplayName</key>
	<string>go-microsip</string>
	<key>CFBundleIdentifier</key>
	<string>__ID__</string>
	<key>CFBundleVersion</key>
	<string>__VERSION__</string>
	<key>CFBundleShortVersionString</key>
	<string>__VERSION__</string>
	<key>CFBundleExecutable</key>
	<string>go-microsip</string>
	<key>CFBundleIconFile</key>
	<string>go-microsip</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleInfoDictionaryVersion</key>
	<string>6.0</string>
	<key>LSMinimumSystemVersion</key>
	<string>__MINIMUM__</string>
	<key>NSMicrophoneUsageDescription</key>
	<string>go-microsip sends your voice to the person you are on a call with.</string>
	<key>NSLocalNetworkUsageDescription</key>
	<string>go-microsip registers with, and calls through, a telephone exchange on your local network.</string>
	<key>CFBundleURLTypes</key>
	<array>
		<dict>
			<key>CFBundleURLName</key>
			<string>__ID__.call</string>
			<key>CFBundleTypeRole</key>
			<string>Viewer</string>
			<key>CFBundleURLSchemes</key>
			<array>
				<string>sip</string>
				<string>tel</string>
			</array>
		</dict>
	</array>
	<key>NSHighResolutionCapable</key>
	<true/>
	<key>NSHumanReadableCopyright</key>
	<string>go-microsip is free software under the GNU General Public License, version 3 or later.</string>
</dict>
</plist>
`)
}

// entry is one file of the bundle.
type entry struct {
	name string
	mode os.FileMode
	data []byte
}

func zipped(files []entry, at time.Time) ([]byte, error) {
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for _, file := range files {
		header := &zip.FileHeader{Name: file.name, Method: zip.Deflate, Modified: at}
		header.SetMode(file.mode)
		to, err := writer.CreateHeader(header)
		if err == nil {
			_, err = to.Write(file.data)
		}
		if err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return archive.Bytes(), nil
}

// verify opens the zip again: exactly the files, their modes and their contents.
func verify(archive []byte, files []entry) error {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return err
	}
	var want, got []string
	for _, file := range files {
		want = append(want, file.name)
	}
	for _, file := range reader.File {
		got = append(got, file.Name)
	}
	sort.Strings(want)
	sort.Strings(got)
	if strings.Join(want, "\n") != strings.Join(got, "\n") {
		return fmt.Errorf("it holds %v, not %v", got, want)
	}
	for i, file := range reader.File {
		from, err := file.Open()
		if err != nil {
			return err
		}
		data, err := io.ReadAll(from)
		_ = from.Close()
		if err != nil {
			return err
		}
		if !bytes.Equal(data, files[i].data) || file.Mode().Perm() != files[i].mode {
			return fmt.Errorf("%s is not what was written", file.Name)
		}
	}
	return nil
}
