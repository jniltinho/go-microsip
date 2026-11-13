// Package media is the engine's view of the media stack: the two G.711 codecs on 20 ms frames and
// the security settings of the RTP layer. Packets, RFC 4733 events, SRTP and the jitter buffer come
// from the vendored diago library (third_party/diago) and pion; this package fixes how they are
// configured and its tests pin their behavior to reference vectors.
package media

import (
	dmedia "github.com/emiago/diago/media"
	"github.com/zaf/g711"
)

// FrameSamples is one 20 ms frame of 8 kHz mono audio.
const FrameSamples = 160

// ReplayWindow is the SRTP and SRTCP replay window, the size RFC 3711 recommends.
const ReplayWindow = 64

// The media guard is always on: RTP and RTCP only from the negotiated address, replayed or forged
// SRTP and SRTCP dropped and counted (third_party/diago/README.go-microsip.md).
func init() {
	dmedia.StrictRemoteAddr = true
	dmedia.SRTPReplayWindow = ReplayWindow
}

// Codec is one of the two codecs the softphone speaks.
type Codec uint8

// Codecs.
const (
	PCMA Codec = iota // A-law, RTP payload type 8
	PCMU              // µ-law, RTP payload type 0
)

// String is the SDP encoding name.
func (c Codec) String() string {
	if c == PCMU {
		return "PCMU"
	}
	return "PCMA"
}

// PayloadType is the static RTP payload type (RFC 3551).
func (c Codec) PayloadType() uint8 { return c.Wire().PayloadType }

// Wire is the codec as the RTP layer names it.
func (c Codec) Wire() dmedia.Codec {
	if c == PCMU {
		return dmedia.CodecAudioUlaw
	}
	return dmedia.CodecAudioAlaw
}

// Encode writes one frame as FrameSamples octets into payload, which must hold them.
func (c Codec) Encode(payload []byte, frame *[FrameSamples]int16) {
	_ = payload[FrameSamples-1]
	if c == PCMU {
		for i, s := range frame {
			payload[i] = encodeUlaw(s)
		}
		return
	}
	for i, s := range frame {
		payload[i] = g711.EncodeAlawFrame(s)
	}
}

// Decode reads one frame from payload and reports whether payload held exactly one.
func (c Codec) Decode(frame *[FrameSamples]int16, payload []byte) bool {
	if len(payload) != FrameSamples {
		return false
	}
	if c == PCMU {
		for i, b := range payload {
			frame[i] = g711.DecodeUlawFrame(b)
		}
		return true
	}
	for i, b := range payload {
		frame[i] = g711.DecodeAlawFrame(b)
	}
	return true
}

// ulawSegmentEnd is the last 14-bit magnitude (bias included) of each mu-law segment.
var ulawSegmentEnd = [8]int{0x3f, 0x7f, 0xff, 0x1ff, 0x3ff, 0x7ff, 0xfff, 0x1fff}

// encodeUlaw is the G.711 mu-law encoder on the sample's 14 most significant bits, as the ITU
// reference and CPython's audioop do it. The g711 library's encoder puts 508 samples one step away
// at segment boundaries; the reference vectors of the Rust candidate need this one.
func encodeUlaw(sample int16) uint8 {
	v := int(sample) >> 2
	mask := uint8(0xff)
	if v < 0 {
		v, mask = -v, 0x7f
	}
	v = min(v, 8159) + 0x21
	for seg, end := range ulawSegmentEnd {
		if v <= end {
			return (uint8(seg)<<4 | uint8(v>>(seg+1))&0x0f) ^ mask
		}
	}
	return 0x7f ^ mask
}
