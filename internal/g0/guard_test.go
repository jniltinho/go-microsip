package g0

import (
	"bytes"
	"net"
	"testing"
	"time"

	"github.com/emiago/diago/media"
	"github.com/emiago/diago/media/sdp"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

// relay is an on-path attacker's position: every datagram between the two media sessions crosses
// it, so the test can forward, hold back, replay and alter packets, all from the address the
// receiver negotiated.
type relay struct {
	t        *testing.T
	rtp, rtc *net.UDPConn
}

func newRelay(t *testing.T) *relay {
	t.Helper()
	for range 50 { // RTCP is expected on the port after RTP
		a, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			t.Fatal(err)
		}
		port := a.LocalAddr().(*net.UDPAddr).Port
		b, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port + 1})
		if err != nil {
			a.Close()
			continue
		}
		t.Cleanup(func() { a.Close(); b.Close() })
		return &relay{t, a, b}
	}
	t.Fatal("no free port pair")
	return nil
}

func (r *relay) addr() *net.UDPAddr { return r.rtp.LocalAddr().(*net.UDPAddr) }

// take receives the next datagram a session sent to the relay.
func take(t *testing.T, c *net.UDPConn) []byte {
	t.Helper()
	buf := make([]byte, 2000)
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := c.ReadFrom(buf)
	if err != nil {
		t.Fatalf("relay read: %v", err)
	}
	return buf[:n]
}

func send(t *testing.T, c *net.UDPConn, data []byte, to net.UDPAddr) {
	t.Helper()
	if _, err := c.WriteTo(data, &to); err != nil {
		t.Fatal(err)
	}
}

func session(t *testing.T, secure int) *media.MediaSession {
	t.Helper()
	s := &media.MediaSession{
		Laddr:     net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)},
		Codecs:    []media.Codec{media.CodecAudioAlaw},
		SecureRTP: secure,
		Mode:      sdp.ModeSendrecv,
	}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// pair negotiates two sessions with each other and then routes both through the relay.
func pair(t *testing.T, secure int) (a, b *media.MediaSession, r *relay) {
	t.Helper()
	a, b = session(t, secure), session(t, secure)
	if err := a.RemoteSDP(b.LocalSDP(false), false); err != nil {
		t.Fatal(err)
	}
	if err := b.RemoteSDP(a.LocalSDP(true), true); err != nil {
		t.Fatal(err)
	}
	r = newRelay(t)
	a.SetRemoteAddr(r.addr())
	b.SetRemoteAddr(r.addr())
	return a, b, r
}

func packet(seq uint16) *rtp.Packet {
	return &rtp.Packet{
		Header:  rtp.Header{Version: 2, PayloadType: 8, SequenceNumber: seq, Timestamp: uint32(seq) * 160, SSRC: 0x600d},
		Payload: bytes.Repeat([]byte{byte(seq)}, 160),
	}
}

// next returns the sequence number of the next packet the session delivers; packets the guard
// dropped come back empty and are skipped, as RTPSession.ReadRTP does.
func next(t *testing.T, s *media.MediaSession) uint16 {
	t.Helper()
	got := make(chan uint16, 1)
	go func() {
		buf := make([]byte, media.RTPBufSize)
		for {
			var p rtp.Packet
			n, err := s.ReadRTP(buf, &p)
			if err != nil {
				return
			}
			if n > 0 && p.Version == 2 {
				if !bytes.Equal(p.Payload, bytes.Repeat([]byte{byte(p.SequenceNumber)}, 160)) {
					t.Errorf("packet %d was delivered with an altered payload", p.SequenceNumber)
				}
				got <- p.SequenceNumber
				return
			}
		}
	}()
	select {
	case seq := <-got:
		return seq
	case <-time.After(2 * time.Second):
		t.Fatal("no packet was delivered")
		return 0
	}
}

func guard(t *testing.T, strict bool, window uint) {
	t.Helper()
	oldStrict, oldWindow := media.StrictRemoteAddr, media.SRTPReplayWindow
	media.StrictRemoteAddr, media.SRTPReplayWindow = strict, window
	t.Cleanup(func() { media.StrictRemoteAddr, media.SRTPReplayWindow = oldStrict, oldWindow })
}

func bRTP(b *media.MediaSession) net.UDPAddr { return b.Laddr }
func bRTCP(b *media.MediaSession) net.UDPAddr {
	return net.UDPAddr{IP: b.Laddr.IP, Port: b.Laddr.Port + 1}
}

// A replayed or altered SRTP packet from the negotiated address is dropped and counted, and never
// reaches the decoder; the genuine stream continues.
func TestSRTPReplayAndTamperAreDropped(t *testing.T) {
	guard(t, true, 64)
	a, b, r := pair(t, 1)
	dropped := media.DroppedUnauthenticated.Load()

	if err := a.WriteRTP(packet(1)); err != nil {
		t.Fatal(err)
	}
	first := take(t, r.rtp)
	if bytes.Contains(first, bytes.Repeat([]byte{1}, 32)) {
		t.Fatal("the payload crossed the wire in clear")
	}
	send(t, r.rtp, first, bRTP(b))
	if seq := next(t, b); seq != 1 {
		t.Fatalf("delivered packet %d, want 1", seq)
	}

	send(t, r.rtp, first, bRTP(b)) // replay
	if err := a.WriteRTP(packet(2)); err != nil {
		t.Fatal(err)
	}
	send(t, r.rtp, take(t, r.rtp), bRTP(b))
	if seq := next(t, b); seq != 2 {
		t.Fatalf("after a replay, delivered packet %d, want 2", seq)
	}
	if got := media.DroppedUnauthenticated.Load() - dropped; got != 1 {
		t.Fatalf("replay: %d packets counted as dropped, want 1", got)
	}

	if err := a.WriteRTP(packet(3)); err != nil {
		t.Fatal(err)
	}
	third := take(t, r.rtp)
	altered := bytes.Clone(third)
	altered[20] ^= 0x01 // one bit of the encrypted payload
	send(t, r.rtp, altered, bRTP(b))
	send(t, r.rtp, third, bRTP(b))
	if seq := next(t, b); seq != 3 {
		t.Fatalf("after a tampered copy, delivered packet %d, want 3", seq)
	}
	if got := media.DroppedUnauthenticated.Load() - dropped; got != 2 {
		t.Fatalf("tamper: %d packets counted as dropped, want 2", got)
	}
}

// The same holds for SRTCP.
func TestSRTCPReplayAndTamperAreDropped(t *testing.T) {
	guard(t, true, 64)
	a, b, r := pair(t, 1)
	dropped := media.DroppedUnauthenticated.Load()
	read := func() int {
		type res struct {
			n   int
			err error
		}
		done := make(chan res, 1)
		go func() {
			n, err := b.ReadRTCP(make([]byte, media.RTPBufSize), make([]rtcp.Packet, 4))
			done <- res{n, err}
		}()
		select {
		case x := <-done:
			if x.err != nil {
				t.Fatalf("ReadRTCP: %v", x.err)
			}
			return x.n
		case <-time.After(2 * time.Second):
			t.Fatal("ReadRTCP did not return")
			return 0
		}
	}

	if err := a.WriteRTCP(&rtcp.SenderReport{SSRC: 0x600d, PacketCount: 1}); err != nil {
		t.Fatal(err)
	}
	first := take(t, r.rtc)
	send(t, r.rtc, first, bRTCP(b))
	if n := read(); n != 1 {
		t.Fatalf("genuine SRTCP delivered %d packets, want 1", n)
	}
	send(t, r.rtc, first, bRTCP(b)) // replay
	if n := read(); n != 0 {
		t.Fatalf("a replayed SRTCP packet delivered %d packets", n)
	}
	if err := a.WriteRTCP(&rtcp.SenderReport{SSRC: 0x600d, PacketCount: 2}); err != nil {
		t.Fatal(err)
	}
	altered := take(t, r.rtc)
	altered[10] ^= 0x01
	send(t, r.rtc, altered, bRTCP(b))
	if n := read(); n != 0 {
		t.Fatalf("a tampered SRTCP packet delivered %d packets", n)
	}
	if got := media.DroppedUnauthenticated.Load() - dropped; got != 2 {
		t.Fatalf("%d SRTCP packets counted as dropped, want 2", got)
	}
}

// RTP and RTCP from an address other than the negotiated one are dropped and counted, with plain
// and with secure media; the same bytes from the negotiated address are accepted.
func TestForeignSourceIsDropped(t *testing.T) {
	for name, secure := range map[string]int{"plain": 0, "secure": 1} {
		t.Run(name, func(t *testing.T) {
			guard(t, true, 64)
			a, b, r := pair(t, secure)
			foreign := media.DroppedForeignSource.Load()
			intruder, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if err != nil {
				t.Fatal(err)
			}
			defer intruder.Close()

			if err := a.WriteRTP(packet(7)); err != nil {
				t.Fatal(err)
			}
			genuine := take(t, r.rtp)
			send(t, intruder, genuine, bRTP(b)) // right bytes, wrong address
			forged, _ := packet(9).Marshal()
			send(t, intruder, forged, bRTP(b))
			send(t, r.rtp, genuine, bRTP(b))
			if seq := next(t, b); seq != 7 {
				t.Fatalf("delivered packet %d, want 7", seq)
			}
			if got := media.DroppedForeignSource.Load() - foreign; got != 2 {
				t.Fatalf("%d foreign RTP packets counted, want 2", got)
			}

			report, _ := (&rtcp.SenderReport{SSRC: 0x600d}).Marshal()
			send(t, intruder, report, bRTCP(b))
			n, err := b.ReadRTCP(make([]byte, media.RTPBufSize), make([]rtcp.Packet, 4))
			if err != nil || n != 0 {
				t.Fatalf("foreign RTCP delivered %d packets, err %v", n, err)
			}
			if got := media.DroppedForeignSource.Load() - foreign; got != 3 {
				t.Fatalf("%d foreign packets counted, want 3", got)
			}
		})
	}
}

// Control: without the guard (upstream defaults) the same replay is delivered again, so the tests
// above fail if the guard stops working.
func TestWithoutGuardReplayIsAccepted(t *testing.T) {
	guard(t, false, 0)
	a, b, r := pair(t, 1)
	if err := a.WriteRTP(packet(1)); err != nil {
		t.Fatal(err)
	}
	first := take(t, r.rtp)
	send(t, r.rtp, first, bRTP(b))
	send(t, r.rtp, first, bRTP(b))
	if next(t, b) != 1 || next(t, b) != 1 {
		t.Fatal("the unguarded session did not deliver the replayed packet")
	}
}
