// SPDX-License-Identifier: MPL-2.0

// This file is not part of upstream diago. It was added by the go-microsip project
// (see ../README.go-microsip.md) together with the marked changes in media_session.go.

package media

import (
	"net"
	"sync/atomic"

	"github.com/pion/srtp/v3"
)

var (
	// StrictRemoteAddr makes a session without RTP NAT learning accept RTP and RTCP only from the
	// address negotiated in the session description, and drop instead of failing on a packet that
	// does not authenticate. Dropped packets are counted below and never reach a decoder.
	StrictRemoteAddr = false

	// SRTPReplayWindow enables SRTP and SRTCP replay protection on receive contexts when above zero.
	// 64 is the window RFC 3711 recommends.
	SRTPReplayWindow uint = 0

	// DroppedForeignSource counts packets dropped because they came from another address.
	DroppedForeignSource atomic.Uint64
	// DroppedUnauthenticated counts SRTP and SRTCP packets dropped because they failed
	// authentication or were replayed.
	DroppedUnauthenticated atomic.Uint64
)

// srtpReceiveOptions are the options of every receive (remote) SRTP context.
func srtpReceiveOptions() []srtp.ContextOption {
	if SRTPReplayWindow == 0 {
		return nil
	}
	return []srtp.ContextOption{
		srtp.SRTPReplayProtection(SRTPReplayWindow),
		srtp.SRTCPReplayProtection(SRTPReplayWindow),
	}
}

// foreignSource reports whether a packet must be dropped because of where it came from.
func (m *MediaSession) foreignSource(from net.Addr, want *net.UDPAddr) bool {
	if !StrictRemoteAddr || m.RTPNAT != RTPNATDisabled || want.IP == nil {
		return false
	}
	got, ok := from.(*net.UDPAddr)
	if ok && got.Port == want.Port && got.IP.Equal(want.IP) {
		return false
	}
	DroppedForeignSource.Add(1)
	return true
}
