package media

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"testing"

	dmedia "github.com/emiago/diago/media"
	"github.com/pion/rtp"
	"github.com/zaf/g711"
)

// The codecs agree with an independent implementation on every input: the reference hashes of the
// Rust candidate's tests, generated with CPython 3.12.3 audioop over ascending samples.
func TestG711MatchesReference(t *testing.T) {
	sum := func(write func(h interface{ Write([]byte) (int, error) })) string {
		h := sha256.New()
		write(h)
		return hex.EncodeToString(h.Sum(nil))
	}
	for name, c := range map[string]struct {
		got  string
		want string
	}{
		"PCMA encode": {sum(func(h interface{ Write([]byte) (int, error) }) {
			for v := -32768; v <= 32767; v++ {
				h.Write([]byte{g711.EncodeAlawFrame(int16(v))})
			}
		}), "38488f6fd710f4686360edc4d38639f96c491595ef93f8eb8d62d5e07ca6ce7b"},
		"PCMU encode": {sum(func(h interface{ Write([]byte) (int, error) }) {
			for v := -32768; v <= 32767; v++ {
				h.Write([]byte{encodeUlaw(int16(v))})
			}
		}), "81d633c9e6972a18c74a58720b96cb8ca0bdd096d4060b646dd708c3b846019a"},
		"PCMA decode": {sum(func(h interface{ Write([]byte) (int, error) }) {
			for v := 0; v <= 255; v++ {
				h.Write(binary.LittleEndian.AppendUint16(nil, uint16(g711.DecodeAlawFrame(uint8(v)))))
			}
		}), "e04788d110e58ff8c70c93b8480190d973e3b67876b6119abbaec766cc75c174"},
		"PCMU decode": {sum(func(h interface{ Write([]byte) (int, error) }) {
			for v := 0; v <= 255; v++ {
				h.Write(binary.LittleEndian.AppendUint16(nil, uint16(g711.DecodeUlawFrame(uint8(v)))))
			}
		}), "3dab54339e520bb2c924826e3b72a917a2b612e9fd12fc867500f1d983a75827"},
	} {
		if c.got != c.want {
			t.Errorf("%s: hash %s, want %s", name, c.got, c.want)
		}
	}
}

// The library's mu-law encoder is not the reference one: this pins how far apart they are, so an
// upgrade that changes it is noticed. Every difference is one quantization step at a boundary.
func TestLibraryUlawDiffersOnlyAtBoundaries(t *testing.T) {
	differ := 0
	for v := -32768; v <= 32767; v++ {
		ours, lib := encodeUlaw(int16(v)), g711.EncodeUlawFrame(int16(v))
		if ours == lib {
			continue
		}
		differ++
		if d := int(g711.DecodeUlawFrame(ours)) - int(g711.DecodeUlawFrame(lib)); d > 1024 || d < -1024 {
			t.Fatalf("sample %d: the encoders are %d apart after decoding", v, d)
		}
	}
	if differ != 508 {
		t.Errorf("%d samples differ from the library's encoder, 508 when this was written", differ)
	}
}

func TestCodecFrames(t *testing.T) {
	if PCMA.PayloadType() != 8 || PCMU.PayloadType() != 0 {
		t.Error("static payload types")
	}
	var silence, back [FrameSamples]int16
	payload := make([]byte, FrameSamples)
	for codec, want := range map[Codec]byte{PCMA: 0xd5, PCMU: 0xff} {
		codec.Encode(payload, &silence)
		for _, b := range payload {
			if b != want {
				t.Fatalf("%v: silence encodes to %#x, want %#x", codec, b, want)
			}
		}
		var tone [FrameSamples]int16
		for i := range tone {
			tone[i] = int16(i*400 - 32000)
		}
		codec.Encode(payload, &tone)
		if !codec.Decode(&back, payload) {
			t.Fatalf("%v: a full frame was refused", codec)
		}
		for i := range tone {
			if d := int(tone[i]) - int(back[i]); d > 1100 || d < -1100 { // G.711 is lossy; the step grows with level
				t.Fatalf("%v: sample %d came back as %d", codec, tone[i], back[i])
			}
		}
		if codec.Decode(&back, payload[:FrameSamples-1]) || codec.Decode(&back, append(payload, 0)) {
			t.Errorf("%v: a payload of another size was accepted", codec)
		}
	}
	if a := testing.AllocsPerRun(100, func() { PCMA.Encode(payload, &silence); PCMA.Decode(&back, payload) }); a != 0 {
		t.Errorf("the codec allocates %v times per frame", a)
	}
}

// RFC 4733 events as the stack sends them: the sixteen event numbers, network byte order, the
// marker-free payload layout, and an end repeated three times with a duration that stops growing.
func TestDTMFEvents(t *testing.T) {
	for event, key := range "0123456789*#ABCD" {
		events := dmedia.RTPDTMFEncode8000(key)
		if len(events) != 7 {
			t.Fatalf("key %q: %d packets, want 7", key, len(events))
		}
		ends := 0
		for i, e := range events {
			if int(e.Event) != event || e.Volume != 10 {
				t.Errorf("key %q packet %d: event %d volume %d", key, i, e.Event, e.Volume)
			}
			if i > 0 && e.Duration < events[i-1].Duration {
				t.Errorf("key %q packet %d: the duration went back", key, i)
			}
			if e.EndOfEvent {
				ends++
				if e.Duration != events[len(events)-1].Duration {
					t.Errorf("key %q: the end packets differ in duration", key)
				}
			} else if ends > 0 {
				t.Errorf("key %q packet %d: a packet after the end is not an end", key, i)
			}
		}
		if ends != 3 {
			t.Errorf("key %q: the end was sent %d times, want 3", key, ends)
		}
	}
	// One event on the wire, as in the Rust candidate's vector: key 5, end, volume 10, 960 samples.
	wire := dmedia.DTMFEncode(dmedia.DTMFEvent{Event: 5, EndOfEvent: true, Volume: 10, Duration: 960})
	if want := []byte{5, 0x8a, 0x03, 0xc0}; string(wire) != string(want) {
		t.Errorf("event on the wire % x, want % x", wire, want)
	}
	var back dmedia.DTMFEvent
	if err := dmedia.DTMFDecode(wire, &back); err != nil || back.Event != 5 || !back.EndOfEvent || back.Duration != 960 {
		t.Errorf("decoded %+v, %v", back, err)
	}
	if dmedia.DTMFDecode([]byte{1, 2, 3}, &back) == nil {
		t.Error("a truncated event was accepted")
	}
}

// The RTP parser refuses what is not RTP and never panics on noise.
func TestRTPParser(t *testing.T) {
	good := &rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 8, SequenceNumber: 7, Timestamp: 1120, SSRC: 0xfeedf00d}, Payload: make([]byte, FrameSamples)}
	wire, err := good.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var p rtp.Packet
	if err := dmedia.RTPUnmarshal(wire, &p); err != nil || p.SequenceNumber != 7 || p.SSRC != 0xfeedf00d || len(p.Payload) != FrameSamples {
		t.Fatalf("round trip: %+v, %v", p.Header, err)
	}
	for name, bad := range map[string][]byte{
		"empty":            {},
		"truncated header": wire[:11],
		"bad padding":      append(append([]byte{wire[0] | 0x20}, wire[1:12]...), 0, 0, 200),
	} {
		var p rtp.Packet
		if err := dmedia.RTPUnmarshal(bad, &p); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// Deterministic noise: a bounded pseudo-random walk over lengths and contents.
	state := uint32(0x9e3779b9)
	buf := make([]byte, 1500)
	for i := 0; i < 20000; i++ {
		state = state*1664525 + 1013904223
		n := int(state>>8) % len(buf)
		for j := 0; j < n; j++ {
			state = state*1664525 + 1013904223
			buf[j] = byte(state >> 24)
		}
		var p rtp.Packet
		_ = dmedia.RTPUnmarshal(buf[:n], &p)
		var e dmedia.DTMFEvent
		_ = dmedia.DTMFDecode(buf[:n%8], &e)
	}
}
