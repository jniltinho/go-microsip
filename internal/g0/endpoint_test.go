package g0

import (
	"net"
	"testing"
)

const (
	audioSecure = "m=audio 4000 RTP/SAVP 8 101\r\na=rtpmap:8 PCMA/8000\r\n" +
		"a=crypto:1 AES_CM_128_HMAC_SHA1_80 inline:QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVphYmNk\r\n"
	audioPlain = "m=audio 4000 RTP/AVP 8 101\r\na=rtpmap:8 PCMA/8000\r\n"
	head       = "v=0\r\no=- 1 1 IN IP4 192.0.2.1\r\ns=-\r\nc=IN IP4 192.0.2.1\r\nt=0 0\r\n"
)

// Only the audio stream's own profile and key make an offer secure.
func TestOffersSRTP(t *testing.T) {
	video := "m=video 4002 RTP/SAVP 96\r\na=crypto:1 AES_CM_128_HMAC_SHA1_80 inline:QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVphYmNk\r\n"
	for name, c := range map[string]struct {
		sdp  string
		want bool
	}{
		"secure audio":                       {head + audioSecure, true},
		"plain audio":                        {head + audioPlain, false},
		"empty":                              {"", false},
		"plain audio, secure video after":    {head + audioPlain + video, false},
		"secure video first, plain audio":    {head + video + audioPlain, false},
		"plain audio with a stray attribute": {head + audioPlain + "a=x-note: RTP/SAVP a=crypto:1\r\n", false},
		"session-level crypto, plain audio":  {head + "a=crypto:1 AES_CM_128_HMAC_SHA1_80 inline:QUJD\r\n" + audioPlain, false},
		"secure profile without a key":       {head + "m=audio 4000 RTP/SAVP 8\r\n", false},
		"secure profile, unsupported suite":  {head + "m=audio 4000 RTP/SAVP 8\r\na=crypto:1 F8_128_HMAC_SHA1_80 inline:QUJD\r\n", false},
		"secure profile, empty key":          {head + "m=audio 4000 RTP/SAVP 8\r\na=crypto:1 AES_CM_128_HMAC_SHA1_80 inline:\r\n", false},
		"plain audio first, secure second":   {head + audioPlain + audioSecure, false},
		"bare newlines":                      {"v=0\nm=audio 4000 RTP/SAVP 8\na=crypto:1 AES_CM_128_HMAC_SHA1_80 inline:QUJD\n", true},
	} {
		if got := offersSRTP([]byte(c.sdp)); got != c.want {
			t.Errorf("%s: offersSRTP = %v, want %v", name, got, c.want)
		}
	}
}

// A request is the registrar's only from its address and its port.
func TestFromRegistrar(t *testing.T) {
	e := &Endpoint{Account: Account{Port: 5060}, peers: []net.IP{net.ParseIP("192.0.2.10"), net.ParseIP("2001:db8::10")}}
	for source, want := range map[string]bool{
		"192.0.2.10:5060":     true,
		"[2001:db8::10]:5060": true,
		"192.0.2.10:40000":    false, // the registrar's address, another port
		"192.0.2.11:5060":     false,
		"192.0.2.10":          false,
		"":                    false,
		"pbx:5060":            false,
	} {
		if got := e.fromRegistrar(source); got != want {
			t.Errorf("fromRegistrar(%q) = %v, want %v", source, got, want)
		}
	}
}
