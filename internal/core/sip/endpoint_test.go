package sip

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/jniltinho/go-microsip/internal/core/domain"
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
		"session-level crypto, plain audio":  {head + "a=crypto:1 AES_CM_128_HMAC_SHA1_80 inline:QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVphYmNk\r\n" + audioPlain, false},
		"secure profile without a key":       {head + "m=audio 4000 RTP/SAVP 8\r\n", false},
		"secure profile, unsupported suite":  {head + "m=audio 4000 RTP/SAVP 8\r\na=crypto:1 F8_128_HMAC_SHA1_80 inline:QUJD\r\n", false},
		"secure profile, empty key":          {head + "m=audio 4000 RTP/SAVP 8\r\na=crypto:1 AES_CM_128_HMAC_SHA1_80 inline:\r\n", false},
		"plain audio first, secure second":   {head + audioPlain + audioSecure, false},
		"bare newlines":                      {"v=0\nm=audio 4000 RTP/SAVP 8\na=crypto:1 AES_CM_128_HMAC_SHA1_80 inline:QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVphYmNk\n", true},
		"key with a lifetime":                {head + "m=audio 4000 RTP/SAVP 8\r\na=crypto:1 AES_CM_128_HMAC_SHA1_80 inline:QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVphYmNk|2^31\r\n", true},
		"key too short":                      {head + "m=audio 4000 RTP/SAVP 8\r\na=crypto:1 AES_CM_128_HMAC_SHA1_80 inline:QUJD\r\n", false},
		"key not base64":                     {head + "m=audio 4000 RTP/SAVP 8\r\na=crypto:1 AES_CM_128_HMAC_SHA1_80 inline:!!!!REVGR0hJSktMTU5PUFFSU1RVVldYWVphYmNk\r\n", false},
	} {
		if got := offersSRTP([]byte(c.sdp)); got != c.want {
			t.Errorf("%s: offersSRTP = %v, want %v", name, got, c.want)
		}
	}
}

// A request is the registrar's only from its address and its port.
func TestFromRegistrar(t *testing.T) {
	e := &Endpoint{Account: Account{Settings: domain.AccountSettings{Port: 5060}}, peers: []net.IP{net.ParseIP("192.0.2.10"), net.ParseIP("2001:db8::10")}}
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

func TestRegistrationFailure(t *testing.T) {
	for name, c := range map[string]struct {
		err  error
		want domain.FailureKind
	}{
		"unknown authority": {fmt.Errorf("dial: %w", x509.UnknownAuthorityError{}), domain.Certificate},
		"wrong host":        {fmt.Errorf("dial: %w", x509.HostnameError{Host: "pbx-alias", Certificate: &x509.Certificate{}}), domain.Certificate},
		"untyped tls error": {errors.New("fail: tls: failed to verify certificate: x509: certificate signed by unknown authority"), domain.Certificate},
		"deadline":          {fmt.Errorf("register: %w", context.DeadlineExceeded), domain.Timeout},
		"refused":           {&net.OpError{Op: "dial", Err: errors.New("connection refused")}, domain.Network},
		"no such host":      {&net.DNSError{Err: "no such host", Name: "pbx.invalid", IsNotFound: true}, domain.Network},
	} {
		if got := RegistrationFailure(c.err); got.Status != domain.RegistrationFailed || got.Failure != c.want {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}

// The local address is the one the routing table gives for the registrar, not the first interface.
func TestTheEndpointBindsWhereTheRegistrarIsReachedFrom(t *testing.T) {
	if got := bindHost([]net.IP{net.ParseIP("::1"), net.ParseIP("127.0.0.1")}, 5060); got != "127.0.0.1" {
		t.Errorf("a registrar on the loopback is reached from %s, want 127.0.0.1", got)
	}
	if got := bindHost([]net.IP{net.ParseIP("2001:db8::1")}, 5060); got != "0.0.0.0" {
		t.Errorf("without an IPv4 registrar the bind is %s, want every interface", got)
	}
}
