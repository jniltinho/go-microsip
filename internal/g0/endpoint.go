// Package g0 is the G0 spike of the Go rewrite (openspec/changes/migrate-to-go-wails, tasks 2.3-2.7):
// lab probes that prove sipgo + diago can carry the softphone's telephony and media-security rules
// against the disposable Asterisk lab, before any screen is built. It is test infrastructure for the
// go/no-go decision, not the production engine.
package g0

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/emiago/diago"
	"github.com/emiago/diago/media"
	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

// Account is one fictional lab account and the way to reach its registrar.
type Account struct {
	User      string
	Password  string
	Server    string // host name of the registrar; also the TLS server name
	Port      int
	Transport string // "udp", "tcp" or "tls"
	// SecureMedia requires SDES-SRTP: keys are offered and accepted only on a TLS account, and a
	// plaintext offer is refused.
	SecureMedia bool
	// CAFile is the PEM bundle that replaces the system roots for a TLS account.
	CAFile string
	Codec  media.Codec
}

func (a Account) String() string { return a.User + "/" + a.Transport }

// TLSConfig builds the client TLS configuration of a TLS account. The configured bundle is the only
// trust anchor: system roots are never added, so a certificate the operating system trusts but the
// bundle does not is refused.
func TLSConfig(a Account) (*tls.Config, error) {
	if a.CAFile == "" {
		return nil, errors.New("a TLS account needs a CA bundle")
	}
	pem, err := os.ReadFile(a.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read CA bundle: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return nil, errors.New("the CA bundle holds no certificate")
	}
	return &tls.Config{RootCAs: roots, ServerName: a.Server, MinVersion: tls.VersionTLS12}, nil
}

// The media guard of the vendored diago (third_party/diago/README.go-microsip.md) is always on: RTP
// and RTCP only from the negotiated address, SRTP and SRTCP replay protection.
func init() {
	media.StrictRemoteAddr = true
	media.SRTPReplayWindow = 64
}

// Endpoint is one registered user agent: a softphone without the window.
type Endpoint struct {
	Account Account
	// AcceptAnySource disables the registrar-only filter. Only the control half of the
	// foreign-source probe sets it, to show what the filter prevents.
	AcceptAnySource bool
	// RefusedSources counts SIP requests dropped because they did not come from the registrar.
	RefusedSources atomic.Uint64
	// RefusedPlainOffers counts calls answered 488 because a secure-media account got no SRTP offer.
	RefusedPlainOffers atomic.Uint64
	peers              []net.IP
	ua                 *sipgo.UserAgent
	dg                 *diago.Diago
	reg                *diago.RegisterTransaction
	Incoming           chan *diago.DialogServerSession
	cancel             context.CancelFunc
	log                *slog.Logger
}

// NewEndpoint creates the user agent. Nothing is sent until Register or Dial.
func NewEndpoint(a Account, log *slog.Logger) (*Endpoint, error) {
	if a.SecureMedia && a.Transport != "tls" {
		return nil, errors.New("SDES keys travel only over TLS: secure media needs a TLS account")
	}
	tran := diago.Transport{
		Transport:    a.Transport,
		BindHost:     "0.0.0.0",
		BindPort:     0,
		TLSURINoSIPS: true,
	}
	if a.SecureMedia {
		tran.MediaSRTP = 1 // SDES
	}
	// sipgo uses the user agent's name as the From user, and the registrar identifies the endpoint
	// by it, so the name is the account's user (the production engine sets From explicitly instead,
	// to keep a product name in the User-Agent header).
	uaOpts := []sipgo.UserAgentOption{sipgo.WithUserAgent(a.User), sipgo.WithUserAgentHostname(a.Server)}
	if a.Transport == "tls" {
		conf, err := TLSConfig(a)
		if err != nil {
			return nil, err
		}
		tran.TLSConf = conf
		uaOpts = append(uaOpts, sipgo.WithUserAgenTLSConfig(conf))
	}
	ua, err := sipgo.NewUA(uaOpts...)
	if err != nil {
		return nil, err
	}
	codec := a.Codec
	if codec.Name == "" {
		codec = media.CodecAudioAlaw
	}
	e := &Endpoint{Account: a, ua: ua, Incoming: make(chan *diago.DialogServerSession, 1), log: log}
	if e.peers, err = net.LookupIP(a.Server); err != nil {
		_ = ua.Close()
		return nil, fmt.Errorf("resolve registrar: %w", err)
	}
	e.dg = diago.NewDiago(ua,
		diago.WithLogger(log),
		diago.WithServerRequestMiddleware(e.registrarOnly),
		diago.WithTransport(tran),
		diago.WithMediaConfig(diago.MediaConfig{Codecs: []media.Codec{codec, media.CodecTelephoneEvent8000}}),
	)
	return e, nil
}

// Serve starts accepting incoming calls. A TLS or TCP account listens on nothing: requests arrive on
// the connection the registration opened. Only a UDP account binds a socket.
func (e *Endpoint) Serve(ctx context.Context) error {
	ctx, e.cancel = context.WithCancel(ctx)
	handler := func(d *diago.DialogServerSession) {
		if e.Account.SecureMedia && !offersSRTP(d.InviteRequest.Body()) {
			// Never fall back to plain audio on an account that requires secure media.
			if err := d.Respond(sip.StatusNotAcceptableHere, "Not Acceptable Here", nil); err != nil {
				e.log.Warn("refusing a plaintext offer failed", "error", err)
				return
			}
			e.RefusedPlainOffers.Add(1) // counted only once the 488 is out
			return
		}
		select {
		case e.Incoming <- d:
			<-d.Context().Done() // the scenario owns the dialog until it ends
		default:
			_ = d.Respond(sip.StatusBusyHere, "Busy Here", nil)
		}
	}
	if e.Account.Transport != "udp" {
		e.dg.HandleFunc(handler)
		return nil
	}
	return e.dg.ServeBackground(ctx, handler)
}

func (e *Endpoint) registrar() sip.Uri {
	uri := sip.Uri{User: e.Account.User, Host: e.Account.Server, Port: e.Account.Port}
	uri.UriParams = sip.NewParams()
	uri.UriParams.Add("transport", e.Account.Transport)
	return uri
}

// Register sends REGISTER with digest authentication and returns once the registrar accepted it.
func (e *Endpoint) Register(ctx context.Context) error {
	reg, err := e.dg.RegisterTransaction(ctx, e.registrar(), diago.RegisterOptions{
		Username: e.Account.User, Password: e.Account.Password, Expiry: 120 * time.Second,
	})
	if err != nil {
		return err
	}
	if err := reg.Register(ctx); err != nil {
		return err
	}
	e.reg = reg
	return nil
}

// Unregister removes the binding.
func (e *Endpoint) Unregister(ctx context.Context) error {
	if e.reg == nil {
		return nil
	}
	err := e.reg.Unregister(ctx)
	e.reg = nil
	return err
}

// Dial calls an extension of the lab and returns once it is answered.
func (e *Endpoint) Dial(ctx context.Context, ext string) (*diago.DialogClientSession, *diago.DialogMedia, error) {
	uri := sip.Uri{User: ext, Host: e.Account.Server, Port: e.Account.Port}
	uri.UriParams = sip.NewParams()
	uri.UriParams.Add("transport", e.Account.Transport)
	d, med, err := e.dg.Invite(ctx, uri, diago.InviteOptions{
		Transport: e.Account.Transport,
		Username:  e.Account.User, Password: e.Account.Password,
		Headers: []sip.Header{},
	})
	if err != nil {
		return nil, nil, err
	}
	// The library accepts a plain answer to a secure offer; a secure-media account does not.
	if e.Account.SecureMedia && (d.InviteResponse == nil || !offersSRTP(d.InviteResponse.Body())) {
		hangup(d, med)
		return nil, nil, ErrInsecureAnswer
	}
	return d, med, nil
}

// ErrInsecureAnswer means the peer answered a secure-media offer without SRTP; the call was ended
// before this endpoint sent or played any audio.
var ErrInsecureAnswer = errors.New("the peer answered without SRTP; the call was ended")

// Close unregisters and releases the sockets.
func (e *Endpoint) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := e.Unregister(ctx); err != nil {
		e.log.Warn("unregister failed", "account", e.Account.String(), "error", err)
	}
	if e.cancel != nil {
		e.cancel()
	}
	_ = e.ua.Close()
}

// registrarOnly drops every request that does not come from the configured registrar: a softphone
// takes calls from its PBX, not from whoever can reach its socket.
func (e *Endpoint) registrarOnly(next sipgo.RequestHandler) sipgo.RequestHandler {
	return func(req *sip.Request, tx sip.ServerTransaction) {
		if !e.AcceptAnySource && !e.fromRegistrar(req.Source()) {
			e.RefusedSources.Add(1)
			tx.Terminate() // no response: the sender learns nothing
			return
		}
		next(req, tx)
	}
}

func (e *Endpoint) fromRegistrar(source string) bool {
	host, port, err := net.SplitHostPort(source)
	// The registrar sends from the port the account talks to: its listening port over UDP, the
	// far end of the account's own connection over TCP and TLS.
	if err != nil || port != strconv.Itoa(e.Account.Port) {
		return false
	}
	ip := net.ParseIP(host)
	for _, p := range e.peers {
		if p.Equal(ip) {
			return true
		}
	}
	return false
}

// offersSRTP reports whether a session description's audio stream is SDES-SRTP: its own media
// line uses the secure profile and its own section carries a usable key for the one suite this
// softphone supports. Attributes of other sections and session-level text do not count.
func offersSRTP(sdp []byte) bool {
	inAudio, secure, keyed := false, false, false
	for _, line := range strings.Split(string(sdp), "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "m="):
			if inAudio {
				return secure && keyed // only the first audio stream is used
			}
			f := strings.Fields(line)
			inAudio = len(f) >= 4 && f[0] == "m=audio"
			secure = inAudio && f[2] == "RTP/SAVP"
		case inAudio && strings.HasPrefix(line, "a=crypto:"):
			f := strings.Fields(line)
			keyed = keyed || (len(f) >= 3 && f[1] == "AES_CM_128_HMAC_SHA1_80" && validKey(f[2]))
		}
	}
	return inAudio && secure && keyed
}

// validKey reports whether a crypto attribute's key parameter carries the 30 bytes this suite
// needs: a 16-byte master key and a 14-byte master salt, base64, before any lifetime or MKI.
func validKey(param string) bool {
	key, ok := strings.CutPrefix(param, "inline:")
	if !ok {
		return false
	}
	key, _, _ = strings.Cut(key, "|")
	raw, err := base64.StdEncoding.DecodeString(key)
	return err == nil && len(raw) == 30
}

// DropTransport closes every connection of the account's transport, as a network loss would.
func (e *Endpoint) DropTransport() error {
	return e.ua.TransportLayer().Pool(e.Account.Transport).Clear()
}

// ListenPorts are the ports the user agent listens on for a transport.
func (e *Endpoint) ListenPorts(network string) []int {
	return e.ua.TransportLayer().ListenPorts(network)
}
