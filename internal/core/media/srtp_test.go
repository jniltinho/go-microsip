package media

import (
	"bytes"
	"encoding/binary"
	"testing"

	dmedia "github.com/emiago/diago/media"
	"github.com/pion/srtp/v3"
)

// These tests pin the SRTP behavior the softphone relies on, on the library and settings the RTP
// layer uses (AES_CM_128_HMAC_SHA1_80, replay window of 64 for SRTP and SRTCP). They are the
// properties the Rust candidate's SRTP adapter tested, checked from the outside.

const ssrc = 0x10203040

func contexts(t *testing.T) (sender, receiver *srtp.Context) {
	t.Helper()
	if dmedia.SRTPReplayWindow != ReplayWindow || !dmedia.StrictRemoteAddr {
		t.Fatal("the media guard is not configured")
	}
	key, salt := bytes.Repeat([]byte{0x39}, 16), bytes.Repeat([]byte{0x39}, 14)
	sender, err := srtp.CreateContext(key, salt, srtp.ProtectionProfileAes128CmHmacSha1_80)
	if err != nil {
		t.Fatal(err)
	}
	receiver, err = srtp.CreateContext(key, salt, srtp.ProtectionProfileAes128CmHmacSha1_80,
		srtp.SRTPReplayProtection(ReplayWindow), srtp.SRTCPReplayProtection(ReplayWindow))
	if err != nil {
		t.Fatal(err)
	}
	return sender, receiver
}

func rtpPacket(seq uint16) []byte {
	p := bytes.Repeat([]byte{0x55}, 172)
	p[0], p[1] = 0x80, 8
	binary.BigEndian.PutUint16(p[2:], seq)
	binary.BigEndian.PutUint32(p[4:], uint32(seq)*160)
	binary.BigEndian.PutUint32(p[8:], ssrc)
	return p
}

func rtcpPacket() []byte {
	p := []byte{0x80, 201, 0, 1}
	p = binary.BigEndian.AppendUint32(p, ssrc)
	p = append(p, 0x81, 202, 0, 3)
	p = binary.BigEndian.AppendUint32(p, ssrc)
	return append(p, 1, 3, 'l', 'a', 'b', 0, 0, 0)
}

func protect(t *testing.T, c *srtp.Context, seq uint16) []byte {
	t.Helper()
	wire, err := c.EncryptRTP(nil, rtpPacket(seq), nil)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

// accept decrypts a copy (decryption works in place) and fails the test unless it yields seq.
func accept(t *testing.T, c *srtp.Context, wire []byte, seq uint16) {
	t.Helper()
	plain, err := c.DecryptRTP(nil, bytes.Clone(wire), nil)
	if err != nil {
		t.Fatalf("packet %d refused: %v", seq, err)
	}
	if !bytes.Equal(plain, rtpPacket(seq)) {
		t.Fatalf("packet %d came out altered", seq)
	}
}

func refuse(t *testing.T, c *srtp.Context, wire []byte, why string) {
	t.Helper()
	if _, err := c.DecryptRTP(nil, bytes.Clone(wire), nil); err == nil {
		t.Fatalf("%s: accepted", why)
	}
}

func TestSRTPOverheadAndConfidentiality(t *testing.T) {
	sender, receiver := contexts(t)
	wire := protect(t, sender, 1)
	if len(wire) != 172+10 {
		t.Errorf("SRTP packet of %d bytes, want the 10-byte tag on 172", len(wire))
	}
	if bytes.Equal(wire[12:172], rtpPacket(1)[12:]) {
		t.Error("the payload is not encrypted")
	}
	if !bytes.Equal(wire[:12], rtpPacket(1)[:12]) {
		t.Error("the header changed")
	}
	control, err := sender.EncryptRTCP(nil, rtcpPacket(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(control) != len(rtcpPacket())+14 {
		t.Errorf("SRTCP packet of %d bytes, want index and tag (14) on %d", len(control), len(rtcpPacket()))
	}
	if bytes.Equal(control[8:24], rtcpPacket()[8:]) {
		t.Error("the report is not encrypted")
	}
	plain, err := receiver.DecryptRTCP(nil, bytes.Clone(control), nil)
	if err != nil || !bytes.Equal(plain, rtcpPacket()) {
		t.Errorf("SRTCP round trip: %v", err)
	}
	accept(t, receiver, wire, 1)
}

func TestSRTPReplayIsRefused(t *testing.T) {
	sender, receiver := contexts(t)
	wire := protect(t, sender, 10)
	accept(t, receiver, wire, 10)
	refuse(t, receiver, wire, "a duplicate")
}

// A forged packet is refused and changes nothing: the genuine one it imitates is still accepted
// once, and a seen packet stays seen.
func TestSRTPTamperingChangesNothing(t *testing.T) {
	sender, receiver := contexts(t)
	first := protect(t, sender, 10)
	accept(t, receiver, first, 10)
	next := protect(t, sender, 11)
	for _, offset := range []int{1, 3, 4, 8, 12, len(next) - 1} { // payload type, sequence, timestamp, source, payload, tag
		forged := bytes.Clone(next)
		forged[offset] ^= 4
		refuse(t, receiver, forged, "a tampered packet")
	}
	forgedSeen := bytes.Clone(first)
	forgedSeen[20] ^= 1
	refuse(t, receiver, forgedSeen, "a forged copy of a seen packet")
	refuse(t, receiver, first, "the seen packet, after its forged copy")
	accept(t, receiver, next, 11)
}

func TestSRTPReorderingWindow(t *testing.T) {
	sender, receiver := contexts(t)
	var packets [][]byte
	for seq := uint16(10); seq <= 74; seq++ {
		packets = append(packets, protect(t, sender, seq))
	}
	accept(t, receiver, packets[64], 74)
	accept(t, receiver, packets[1], 11) // 63 behind: inside the window
	refuse(t, receiver, packets[1], "a duplicate inside the window")
	refuse(t, receiver, packets[0], "a packet 64 behind")
}

func TestSRTPRollover(t *testing.T) {
	sender, receiver := contexts(t)
	before, late, after := protect(t, sender, 65534), protect(t, sender, 65535), protect(t, sender, 0)
	accept(t, receiver, before, 65534)
	accept(t, receiver, after, 0)
	accept(t, receiver, late, 65535) // from before the rollover, arriving after it
	refuse(t, receiver, late, "a duplicate across the rollover")
}

func TestSRTPForgedRolloverDoesNotPoison(t *testing.T) {
	sender, receiver := contexts(t)
	before, last, rollover := protect(t, sender, 65534), protect(t, sender, 65535), protect(t, sender, 0)
	accept(t, receiver, before, 65534)
	forged := bytes.Clone(rollover)
	forged[12] ^= 1
	refuse(t, receiver, forged, "a forged rollover packet")
	accept(t, receiver, last, 65535)
	accept(t, receiver, rollover, 0)
}

func TestSRTPWrongKeyChangesNothing(t *testing.T) {
	sender, receiver := contexts(t)
	other, err := srtp.CreateContext(bytes.Repeat([]byte{0x40}, 16), bytes.Repeat([]byte{0x40}, 14), srtp.ProtectionProfileAes128CmHmacSha1_80)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := other.EncryptRTP(nil, rtpPacket(10), nil)
	if err != nil {
		t.Fatal(err)
	}
	refuse(t, receiver, foreign, "a packet under another key")
	accept(t, receiver, protect(t, sender, 10), 10)
}

func TestSRTCPReplayAndTampering(t *testing.T) {
	sender, receiver := contexts(t)
	var reports [][]byte
	for range 3 {
		wire, err := sender.EncryptRTCP(nil, rtcpPacket(), nil)
		if err != nil {
			t.Fatal(err)
		}
		reports = append(reports, wire)
	}
	open := func(wire []byte) error {
		_, err := receiver.DecryptRTCP(nil, bytes.Clone(wire), nil)
		return err
	}
	if err := open(reports[2]); err != nil {
		t.Fatalf("the newest report was refused: %v", err)
	}
	if open(reports[2]) == nil {
		t.Error("a replayed report was accepted")
	}
	for _, offset := range []int{9, len(reports[1]) - 12, len(reports[1]) - 1} { // body, index, tag
		forged := bytes.Clone(reports[1])
		forged[offset] ^= 1
		if open(forged) == nil {
			t.Errorf("a report tampered at byte %d was accepted", offset)
		}
	}
	if err := open(reports[0]); err != nil { // older, unseen, inside the window
		t.Errorf("an unseen report inside the window was refused: %v", err)
	}
	if open(reports[0]) == nil {
		t.Error("that report was accepted twice")
	}
}
