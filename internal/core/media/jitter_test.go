package media

import (
	"io"
	"testing"
	"time"

	dmedia "github.com/emiago/diago/media"
	"github.com/pion/rtp"
)

// network is a scripted packet source standing where the RTP session is.
type network chan uint16

func (n network) ReadRTP(buf []byte, p *rtp.Packet) (int, error) {
	seq, ok := <-n
	if !ok {
		return 0, io.EOF
	}
	raw, err := (&rtp.Packet{
		Header:  rtp.Header{Version: 2, PayloadType: 8, SequenceNumber: seq, Timestamp: uint32(seq) * 160, SSRC: ssrc},
		Payload: make([]byte, FrameSamples),
	}).Marshal()
	if err != nil {
		return 0, err
	}
	size := copy(buf, raw)
	return size, p.Unmarshal(buf[:size])
}

// The jitter buffer puts reordered packets back in order, drops duplicates, and holds a fixed
// number of packets however many arrive while nobody reads.
func TestJitterBufferOrdersAndIsBounded(t *testing.T) {
	const window = 10
	net := make(network, 400)
	jb := dmedia.NewRTPJitterBuffer(net, 20*time.Millisecond, dmedia.RTPJitterBufferOptions{DelayPackets: 3, MaxPackets: window})
	defer jb.Close()

	for _, seq := range []uint16{1, 3, 2, 2, 5, 4, 6} {
		net <- seq
	}
	buf := make([]byte, dmedia.RTPBufSize)
	for want := uint16(1); want <= 6; want++ {
		var p rtp.Packet
		done := make(chan error, 1)
		go func() { _, err := jb.ReadRTP(buf, &p); done <- err }()
		select {
		case err := <-done:
			if err != nil || p.SequenceNumber != want {
				t.Fatalf("read packet %d, %v; want %d", p.SequenceNumber, err, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("packet %d was not delivered", want)
		}
	}
	if st := jb.Statistics(); st.PacketsDuplicate != 1 {
		t.Errorf("%d duplicates counted, want 1", st.PacketsDuplicate)
	}

	// A burst while the consumer is away: the buffer takes what fits and leaves the rest unread.
	before := jb.Statistics().PacketsRead
	for seq := uint16(7); seq < 307; seq++ {
		net <- seq
	}
	time.Sleep(300 * time.Millisecond)
	if took := jb.Statistics().PacketsRead - before; took > window+1 {
		t.Errorf("the idle buffer took %d packets, more than its %d slots", took, window+1)
	}
}

// A stream that pauses (a held call, a peer that stops sending) and goes on is heard again, whether
// its sequence numbers continue where they stopped or jumped ahead meanwhile. Upstream v0.42.0
// dropped every packet after the pause as late.
func TestJitterBufferResumesAfterSilence(t *testing.T) {
	net := make(network, 64)
	jb := dmedia.NewRTPJitterBuffer(net, 20*time.Millisecond, dmedia.RTPJitterBufferOptions{DelayPackets: 3, MaxPackets: 10})
	defer jb.Close()
	// The consumer reads all the time, through the pauses too, as a call's receive loop does.
	got := make(chan uint16, 64)
	go func() {
		buf := make([]byte, dmedia.RTPBufSize)
		for {
			var p rtp.Packet
			if _, err := jb.ReadRTP(buf, &p); err != nil {
				return
			}
			got <- p.SequenceNumber
		}
	}()
	stream := func(from, to uint16) {
		t.Helper()
		for seq := from; seq <= to; seq++ {
			net <- seq
		}
		for want := from; want <= to; want++ {
			select {
			case seq := <-got:
				if seq != want {
					t.Fatalf("read packet %d, want %d", seq, want)
				}
			case <-time.After(time.Second):
				t.Fatalf("packet %d was not delivered", want)
			}
		}
	}
	silence := 500 * time.Millisecond // 25 packet times: more than the window
	stream(1, 5)
	time.Sleep(silence)
	stream(6, 10) // continues where it stopped
	time.Sleep(silence)
	stream(500, 505) // jumped far ahead
}
