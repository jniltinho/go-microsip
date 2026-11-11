package audio

import "testing"

// The ring keeps order, refuses what does not fit, and its operations do not allocate: they run on
// the device side.
func TestRing(t *testing.T) {
	r := newRing(8)
	if n := r.write([]int16{1, 2, 3, 4, 5, 6}); n != 6 {
		t.Fatalf("wrote %d, want 6", n)
	}
	if n := r.write([]int16{7, 8, 9}); n != 2 {
		t.Fatalf("a full ring took %d, want 2", n)
	}
	got := make([]int16, 5)
	if n := r.read(got); n != 5 || got[0] != 1 || got[4] != 5 {
		t.Fatalf("read %d %v", n, got)
	}
	r.write([]int16{9, 10, 11}) // wraps
	got = make([]int16, 8)
	n := r.read(got)
	if want := []int16{6, 7, 8, 9, 10, 11}; n != len(want) || got[0] != 6 || got[5] != 11 {
		t.Fatalf("after wrapping read %d %v, want %v", n, got[:n], want)
	}
	if r.len() != 0 {
		t.Fatalf("len = %d, want 0", r.len())
	}
	frame := make([]int16, FrameSamples)
	big := newRing(4096)
	if a := testing.AllocsPerRun(100, func() { big.write(frame); big.read(frame) }); a != 0 {
		t.Fatalf("ring operations allocate %v times", a)
	}
}
