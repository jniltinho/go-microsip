package sip

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/jniltinho/go-microsip/internal/core/domain"

	gosip "github.com/emiago/sipgo/sip"
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

// A request that fails because the listener's port is not usable yet is asked again and
// succeeds; any other failure is returned at once, and a port that stays taken is a failure.
func TestARequestWaitsForItsListener(t *testing.T) {
	taken := errors.New("client transaction failed to request connection: listen udp 127.0.0.1:5070: bind: address already in use")
	calls := 0
	if err := whenBound(context.Background(), func() error {
		if calls++; calls < 3 {
			return taken
		}
		return nil
	}); err != nil || calls != 3 {
		t.Errorf("a listener that became usable: %v after %d attempts", err, calls)
	}
	calls = 0
	refused := errors.New("403 Forbidden")
	if err := whenBound(context.Background(), func() error { calls++; return refused }); err != refused || calls != 1 {
		t.Errorf("another failure: %v after %d attempts", err, calls)
	}
	calls = 0
	if err := whenBound(context.Background(), func() error { calls++; return taken }); err != taken || calls != 5 {
		t.Errorf("a port that stays taken: %v after %d attempts", err, calls)
	}
}

// The local address is the one the routing table gives for the registrar, not the first
// interface, and only when every address of the registrar agrees on it.
func TestTheEndpointBindsWhereTheRegistrarIsReachedFrom(t *testing.T) {
	ip := net.ParseIP
	routes := map[string]net.IP{
		"198.51.100.10": ip("10.8.0.2"), // through a VPN
		"198.51.100.11": ip("10.8.0.2"),
		"203.0.113.5":   ip("192.168.1.20"), // through the local network
		"127.0.0.1":     ip("127.0.0.1"),
	}
	route := func(peer net.IP) net.IP { return routes[peer.String()] }
	for _, c := range []struct {
		name  string
		peers []net.IP
		want  string
	}{
		{"one registrar address through the VPN", []net.IP{ip("198.51.100.10")}, "10.8.0.2"},
		{"two addresses reached from the same one", []net.IP{ip("198.51.100.10"), ip("198.51.100.11")}, "10.8.0.2"},
		{"an IPv6 address beside it is not what is bound", []net.IP{ip("2001:db8::1"), ip("198.51.100.10")}, "10.8.0.2"},
		{"addresses reached from different interfaces", []net.IP{ip("198.51.100.10"), ip("203.0.113.5")}, "0.0.0.0"},
		{"an address with no route", []net.IP{ip("198.51.100.10"), ip("192.0.2.99")}, "0.0.0.0"},
		{"a proxy or tunnel on this host", []net.IP{ip("127.0.0.1")}, "0.0.0.0"},
		{"only IPv6", []net.IP{ip("2001:db8::1")}, "0.0.0.0"},
		{"nothing resolved", nil, "0.0.0.0"},
	} {
		if got := localFor(c.peers, route); got != c.want {
			t.Errorf("%s: bound to %s, want %s", c.name, got, c.want)
		}
	}
	// The kernel's own answer for the loopback is the loopback, which is not bound.
	if got := bindHost([]net.IP{ip("127.0.0.1")}, 5060); got != "0.0.0.0" {
		t.Errorf("a registrar on the loopback binds %s, want every interface", got)
	}
}

// An endpoint's address is gone when no interface of the machine has it any more; an endpoint
// bound to every interface is never said to have lost its address.
func TestAnAddressThatLeftTheMachineIsGone(t *testing.T) {
	_, lan, _ := net.ParseCIDR("192.0.2.7/24")
	lan.IP = net.ParseIP("192.0.2.7")
	have := []net.Addr{&net.IPNet{IP: net.IPv4(127, 0, 0, 1), Mask: net.CIDRMask(8, 32)}, lan}
	for local, want := range map[string]bool{"192.0.2.7": true, "127.0.0.1": true, "0.0.0.0": true, "": true, "198.51.100.9": false} {
		if got := assigned(local, have); got != want {
			t.Errorf("%q assigned: %v, want %v", local, got, want)
		}
	}
	if assigned("192.0.2.7", nil) {
		t.Error("an address is assigned on a machine that has none")
	}
}

// The CANCEL of a call that another phone answered says so in its Reason header (RFC 3326): a
// SIP cause of 200, whatever its text, its spacing or what else the header lists.
func TestACancelSaysWhenTheCallWasAnsweredElsewhere(t *testing.T) {
	reason := func(values ...string) []gosip.Header {
		var out []gosip.Header
		for _, v := range values {
			out = append(out, gosip.NewHeader("Reason", v))
		}
		return out
	}
	for _, yes := range [][]gosip.Header{
		reason(`SIP;cause=200;text="Call completed elsewhere"`),
		reason(`SIP ;cause=200`),
		reason(`sip; Cause = "200" ;text="x"`),
		reason(`Q.850;cause=16`, `SIP;cause=200`),
		reason(`Q.850;cause=16;text="Normal", SIP;cause=200;text="Call completed elsewhere"`),
	} {
		if !completedElsewhere(yes) {
			t.Errorf("%v: not read as answered elsewhere", yes)
		}
	}
	for _, no := range [][]gosip.Header{
		nil,
		reason(`SIP;cause=487;text="Request Terminated"`),
		reason(`Q.850;cause=200`),
		reason(`SIP;text="cause=200"`),
		reason(`SIP;cause=2000`),
		reason(``),
	} {
		if completedElsewhere(no) {
			t.Errorf("%v: read as answered elsewhere", no)
		}
	}
}
