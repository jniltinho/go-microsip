package main

import (
	"archive/zip"
	"bytes"
	"debug/macho"
	"encoding/binary"
	"image"
	"image/png"
	"io"
	"strings"
	"testing"
	"time"
)

const (
	version = "0.2.0-rc.1"
	commit  = "0123456789ab"
	macOS13 = 13 << 16
)

// thin is a Mach-O file with a header and the one load command that names its platform and its
// minimum system, followed by text standing for the program: enough for what is checked here.
func thin(cpu macho.Cpu, kind macho.Type, platform, minimum uint32, text string) []byte {
	var file bytes.Buffer
	words := func(values ...uint32) { _ = binary.Write(&file, binary.LittleEndian, values) }
	words(macho.Magic64, uint32(cpu), 0, uint32(kind), 1, 24, 0, 0)
	words(loadBuildVersion, 24, platform, minimum, minimum, 0)
	file.WriteString(text)
	return file.Bytes()
}

func program(cpu macho.Cpu, minimum uint32) []byte {
	return thin(cpu, macho.TypeExec, platformMacOS, minimum, "go-microsip "+version+" ("+commit+") for "+cpu.String())
}

func icon(t *testing.T, width, height int) []byte {
	t.Helper()
	var file bytes.Buffer
	if err := png.Encode(&file, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return file.Bytes()
}

func pack(t *testing.T, slices ...[]byte) ([]byte, error) {
	t.Helper()
	return build(version, commit, slices, icon(t, 256, 256), []byte("the licence"), []byte("the notices"), time.Unix(0, 0))
}

func TestTheUniversalExecutableHoldsBothSlicesUnchanged(t *testing.T) {
	intel, apple := program(macho.CpuAmd64, macOS13), program(macho.CpuArm64, macOS13)
	fat, minimum, err := universal([][]byte{intel, apple})
	if err != nil {
		t.Fatal(err)
	}
	if minimum != macOS13 {
		t.Errorf("minimum %#x, want %#x", minimum, macOS13)
	}
	file, err := macho.NewFatFile(bytes.NewReader(fat))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Arches) != 2 || file.Arches[0].Cpu != macho.CpuAmd64 || file.Arches[1].Cpu != macho.CpuArm64 {
		t.Fatalf("architectures %v, want x86_64 and arm64", file.Arches)
	}
	for i, given := range [][]byte{intel, apple} {
		arch := file.Arches[i]
		if arch.Offset%(1<<align) != 0 {
			t.Errorf("slice %d starts at %d, not on a page", i, arch.Offset)
		}
		// The signature of a slice covers its bytes: they must be the ones given.
		if !bytes.Equal(fat[arch.Offset:arch.Offset+arch.Size], given) {
			t.Errorf("slice %d was changed by the joining", i)
		}
	}
}

func TestWhatIsNotTheTwoExecutablesIsRefused(t *testing.T) {
	intel, apple := program(macho.CpuAmd64, macOS13), program(macho.CpuArm64, macOS13)
	for name, c := range map[string]struct {
		slices [][]byte
		want   string
	}{
		"a file that is not Mach-O": {[][]byte{intel, []byte("#!/bin/sh\n" + version + commit)}, "not a thin Mach-O"},
		"one slice twice":           {[][]byte{intel, intel}, "a second executable"},
		"one slice alone":           {[][]byte{apple}, "exactly one x86_64 and one arm64"},
		"three slices":              {[][]byte{intel, apple, program(macho.Cpu386, macOS13)}, "not for x86_64 or arm64"},
		"an object file":            {[][]byte{intel, thin(macho.CpuArm64, macho.TypeObj, platformMacOS, macOS13, version+commit)}, "not an executable"},
		"an executable for iOS":     {[][]byte{intel, thin(macho.CpuArm64, macho.TypeExec, 2, macOS13, version+commit)}, "not macOS"},
		"no minimum system":         {[][]byte{intel, thin(macho.CpuArm64, macho.TypeExec, platformMacOS, 0, version+commit)}, "does not say which macOS"},
		"another commit":            {[][]byte{intel, thin(macho.CpuArm64, macho.TypeExec, platformMacOS, macOS13, version+" (ffffffffffff)")}, "was not built as version"},
		"another version":           {[][]byte{intel, thin(macho.CpuArm64, macho.TypeExec, platformMacOS, macOS13, "0.1.0 ("+commit+")")}, "was not built as version"},
	} {
		if _, err := pack(t, c.slices...); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want an error with %q", name, err, c.want)
		}
	}
}

func TestTheBundleHoldsExactlyItsFiles(t *testing.T) {
	// The arm64 slice needs a later system than the x86_64 one: the bundle must ask for the later.
	archive, err := pack(t, program(macho.CpuAmd64, macOS13), program(macho.CpuArm64, 14<<16|2<<8|1))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]*zip.File{}
	for _, file := range reader.File {
		files[file.Name] = file
	}
	want := []string{"Info.plist", "PkgInfo", "MacOS/go-microsip", "Resources/go-microsip.icns", "Resources/LICENSE.txt", "Resources/THIRD_PARTY_NOTICES.txt"}
	if len(files) != len(want) {
		t.Errorf("%d files, want %d", len(files), len(want))
	}
	for _, name := range want {
		if files[bundle+name] == nil {
			t.Errorf("%s is missing", name)
		}
	}
	if mode := files[executable].Mode().Perm(); mode != 0o755 {
		t.Errorf("the executable's mode is %o", mode)
	}
	from, err := files[bundle+"Info.plist"].Open()
	if err != nil {
		t.Fatal(err)
	}
	text, _ := io.ReadAll(from)
	for _, part := range []string{
		"<key>CFBundleIdentifier</key>\n\t<string>io.github.jniltinho.go-microsip</string>",
		"<key>CFBundleExecutable</key>\n\t<string>go-microsip</string>",
		// Three numbers: the pre-release suffix is not a macOS version.
		"<key>CFBundleShortVersionString</key>\n\t<string>0.2.0</string>",
		"<key>CFBundleVersion</key>\n\t<string>0.2.0</string>",
		"<key>LSMinimumSystemVersion</key>\n\t<string>14.2.1</string>",
		"<key>NSMicrophoneUsageDescription</key>",
		"<key>NSLocalNetworkUsageDescription</key>",
		"<string>sip</string>",
		"<string>tel</string>",
		"<key>NSHighResolutionCapable</key>\n\t<true/>",
	} {
		if !strings.Contains(string(text), part) {
			t.Errorf("Info.plist lacks %q", part)
		}
	}
	if strings.Contains(string(text), "LSUIElement") {
		t.Error("the application must show in the Dock")
	}
}

func TestTheIconMustBeASquareOfAKnownSize(t *testing.T) {
	if file, err := icns(icon(t, 256, 256)); err != nil || string(file[:4]) != "icns" || string(file[8:12]) != "ic08" {
		t.Errorf("a 256-pixel icon: %v, %q", err, file[:12])
	}
	for name, image := range map[string][]byte{"not a PNG": []byte("icon"), "not square": icon(t, 256, 128), "an odd size": icon(t, 100, 100)} {
		if _, err := icns(image); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestAZipThatDoesNotReadBackIsRefused(t *testing.T) {
	files := []entry{{"a", 0o644, []byte("one")}, {"b", 0o755, []byte("two")}}
	archive, err := zipped(files, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := verify(archive, files); err != nil {
		t.Errorf("the zip as written: %v", err)
	}
	for name, other := range map[string][]entry{
		"a file more":     append(files[:2:2], entry{"c", 0o644, nil}),
		"a file less":     files[:1],
		"another mode":    {files[0], {"b", 0o644, []byte("two")}},
		"another content": {files[0], {"b", 0o755, []byte("2")}},
	} {
		if verify(archive, other) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
