// Package sip is the account's signaling: one user agent on sipgo and diago that registers, places
// and receives calls, and enforces the refusals a softphone owes its user: requests only from the
// registrar, secure media never downgraded, a custom CA bundle that replaces the system roots, and
// no listening socket on a TLS or TCP account.
package sip

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emiago/diago"
	dmedia "github.com/emiago/diago/media"
	"github.com/emiago/sipgo"
	gosip "github.com/emiago/sipgo/sip"

	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/media"
)

// Account is the account's settings with what the engine adds to register it.
type Account struct {
	Settings domain.AccountSettings
	Password domain.Secret
	// CA is the PEM bundle that replaces the system roots for a TLS account. The caller reads it
	// from a file it has checked (config.ReadCAFile).
	CA []byte
	// Codecs is the offer, in order of preference; empty means PCMU then PCMA.
	Codecs []media.Codec
	// UserAgent is the product name sent in the User-Agent header.
	UserAgent string
}

// ErrInsecureAnswer means the peer answered a secure-media offer without SRTP; the call was ended
// before this endpoint sent or played any audio.
var ErrInsecureAnswer = errors.New("the peer answered without SRTP; the call was ended")

// TLSConfig builds the client TLS configuration of a TLS account. The configured bundle is the only
// trust anchor: system roots are never added, so a certificate the operating system trusts but the
// bundle does not is refused.
func TLSConfig(server string, pem []byte) (*tls.Config, error) {
	if len(pem) == 0 {
		return nil, errors.New("a TLS account needs a CA bundle")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return nil, errors.New("the CA bundle holds no certificate")
	}
	return &tls.Config{RootCAs: roots, ServerName: server, MinVersion: tls.VersionTLS12}, nil
}

// Endpoint is the account's user agent.
type Endpoint struct {
	Account Account
	// AcceptAnySource disables the registrar-only filter. Only the control half of the
	// foreign-source lab probe sets it, to show what the filter prevents.
	AcceptAnySource bool
	// RefusedSources counts SIP requests dropped because they did not come from the registrar.
	RefusedSources atomic.Uint64
	// RefusedPlainOffers counts calls answered 488 because a secure-media account got no SRTP offer.
	RefusedPlainOffers atomic.Uint64
	// Incoming delivers each incoming call that passed the refusals. The receiver owns the dialog;
	// the endpoint's handler waits until the dialog ends. A call that arrives while another is
	// waiting here is answered 486.
	Incoming chan *diago.DialogServerSession

	peers  []net.IP
	ua     *sipgo.UserAgent
	dg     *diago.Diago
	cancel context.CancelFunc
	log    *slog.Logger

	// mu guards the binding. The library's registration is not safe for a renewal and another
	// request at once, so halt stops the renewal loop before anything else touches it.
	mu   sync.Mutex
	reg  *diago.RegisterTransaction
	halt func()
}

// stopRenewal ends a running KeepRegistered and waits until it has returned.
func (e *Endpoint) stopRenewal() {
	e.mu.Lock()
	halt := e.halt
	e.halt = nil
	e.mu.Unlock()
	if halt != nil {
		halt()
	}
}

func (e *Endpoint) secure() bool { return e.Account.Settings.Media == domain.SRTP }

// NewEndpoint creates the user agent of a validated account. Nothing is sent until Register or Dial.
func NewEndpoint(a Account, log *slog.Logger) (*Endpoint, error) {
	s := a.Settings
	if err := s.Validate(); err != nil {
		return nil, err
	}
	tran := diago.Transport{
		Transport:    s.Transport.String(),
		BindHost:     "0.0.0.0",
		BindPort:     0,
		TLSURINoSIPS: true,
	}
	if s.Media == domain.SRTP {
		tran.MediaSRTP = 1 // SDES
	}
	name := a.UserAgent
	if name == "" {
		name = "go-microsip"
	}
	uaOpts := []sipgo.UserAgentOption{sipgo.WithUserAgent(name), sipgo.WithUserAgentHostname(s.Domain)}
	if s.Transport == domain.TLS {
		conf, err := TLSConfig(s.Server, a.CA)
		if err != nil {
			return nil, err
		}
		tran.TLSConf = conf
		uaOpts = append(uaOpts, sipgo.WithUserAgenTLSConfig(conf))
	}
	codecs := a.Codecs
	if len(codecs) == 0 {
		codecs = []media.Codec{media.PCMU, media.PCMA}
	}
	wire := make([]dmedia.Codec, 0, len(codecs)+1)
	for _, c := range codecs {
		wire = append(wire, c.Wire())
	}
	wire = append(wire, dmedia.CodecTelephoneEvent8000)

	peers, err := net.LookupIP(s.Server)
	if err != nil {
		return nil, fmt.Errorf("resolve registrar: %w", err)
	}
	ua, err := sipgo.NewUA(uaOpts...)
	if err != nil {
		return nil, err
	}
	e := &Endpoint{Account: a, ua: ua, peers: peers, Incoming: make(chan *diago.DialogServerSession, 1), log: log}
	e.dg = diago.NewDiago(ua,
		diago.WithLogger(log),
		diago.WithServerRequestMiddleware(e.registrarOnly),
		diago.WithTransport(tran),
		diago.WithMediaConfig(diago.MediaConfig{Codecs: wire}),
	)
	return e, nil
}

// Serve starts accepting incoming calls. A TLS or TCP account listens on nothing: requests arrive on
// the connection the registration opened. Only a UDP account binds a socket.
func (e *Endpoint) Serve(ctx context.Context) error {
	ctx, e.cancel = context.WithCancel(ctx)
	handler := func(d *diago.DialogServerSession) {
		if e.secure() && !offersSRTP(d.InviteRequest.Body()) {
			// Never fall back to plain audio on an account that requires secure media.
			if err := d.Respond(gosip.StatusNotAcceptableHere, "Not Acceptable Here", nil); err != nil {
				e.log.Warn("refusing a plaintext offer failed", "error", err)
				return
			}
			e.RefusedPlainOffers.Add(1) // counted only once the 488 is out
			return
		}
		select {
		case e.Incoming <- d:
			<-d.Context().Done() // the receiver owns the dialog until it ends
		default:
			_ = d.Respond(gosip.StatusBusyHere, "Busy Here", nil)
		}
	}
	if e.Account.Settings.Transport != domain.UDP {
		e.dg.HandleFunc(handler)
		return nil
	}
	return e.dg.ServeBackground(ctx, handler)
}

// uri is a SIP address at the registrar for the given user part.
func (e *Endpoint) uri(user string) gosip.Uri {
	s := e.Account.Settings
	uri := gosip.Uri{User: user, Host: s.Server, Port: int(s.Port)}
	uri.UriParams = gosip.NewParams()
	uri.UriParams.Add("transport", s.Transport.String())
	return uri
}

func (e *Endpoint) authUser() string {
	if u := e.Account.Settings.AuthUsername; u != "" {
		return u
	}
	return e.Account.Settings.Username
}

// identity is the account's address of record as a From header.
func (e *Endpoint) identity() *gosip.FromHeader {
	s := e.Account.Settings
	return &gosip.FromHeader{DisplayName: s.DisplayName, Address: gosip.Uri{User: s.Username, Host: s.Domain}, Params: gosip.NewParams()}
}

// Register sends REGISTER with digest authentication and returns once the registrar accepted it.
func (e *Endpoint) Register(ctx context.Context) error {
	s := e.Account.Settings
	expires := time.Duration(s.Expires) * time.Second
	if expires <= 0 {
		expires = 300 * time.Second
	}
	reg, err := e.dg.RegisterTransaction(ctx, e.uri(s.Username), diago.RegisterOptions{
		Username: e.authUser(), Password: e.Account.Password.Reveal(), Expiry: expires,
	})
	if err != nil {
		return err
	}
	// The address of record, not the product name the user agent carries.
	from := e.identity()
	from.Params.Add("tag", gosip.GenerateTagN(16))
	reg.Origin.AppendHeader(from)
	reg.Origin.AppendHeader(&gosip.ToHeader{Address: from.Address})
	e.stopRenewal()
	if err := reg.Register(ctx); err != nil {
		return err
	}
	e.mu.Lock()
	e.reg = reg
	e.mu.Unlock()
	return nil
}

// KeepRegistered renews the binding before it expires, until ctx ends or the registrar refuses. It
// returns the reason; a canceled context is not a failure.
func (e *Endpoint) KeepRegistered(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	defer close(done)
	e.mu.Lock()
	reg := e.reg
	if reg == nil || e.halt != nil {
		e.mu.Unlock()
		return errors.New("not registered, or already renewing")
	}
	e.halt = func() { cancel(); <-done }
	e.mu.Unlock()
	return reg.QualifyLoop(ctx)
}

// Unregister removes the binding.
func (e *Endpoint) Unregister(ctx context.Context) error {
	e.stopRenewal()
	e.mu.Lock()
	reg := e.reg
	e.reg = nil
	e.mu.Unlock()
	if reg == nil {
		return nil
	}
	return reg.Unregister(ctx)
}

// Dial calls a user part at the registrar and returns once the call is answered. onProgress, when
// set, is told each provisional status (180 Ringing, 183 Session Progress). A secure-media account
// ends a call whose answer is not SRTP and returns ErrInsecureAnswer.
func (e *Endpoint) Dial(ctx context.Context, target string, onProgress func(status int)) (*diago.DialogClientSession, *diago.DialogMedia, error) {
	// shortcut: the call is addressed to the server's host, so an account whose domain differs from
	// its server sends To with the server's name; add an outbound-proxy route when one is needed.
	d, med, err := e.dg.Invite(ctx, e.uri(target), diago.InviteOptions{
		Transport: e.Account.Settings.Transport.String(),
		Username:  e.authUser(), Password: e.Account.Password.Reveal(),
		Headers: []gosip.Header{e.identity()},
		OnResponse: func(res *gosip.Response) error {
			if onProgress != nil && res.StatusCode > 100 && res.StatusCode < 200 {
				onProgress(res.StatusCode)
			}
			return nil
		},
	})
	if err != nil {
		return nil, nil, err
	}
	// The library accepts a plain answer to a secure offer; a secure-media account does not.
	if e.secure() && (d.InviteResponse == nil || !offersSRTP(d.InviteResponse.Body())) {
		hctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = d.Hangup(hctx)
		cancel()
		_ = med.Close()
		_ = d.Close()
		return nil, nil, ErrInsecureAnswer
	}
	return d, med, nil
}

// Close unregisters and releases the sockets.
func (e *Endpoint) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := e.Unregister(ctx); err != nil {
		e.log.Warn("unregister failed", "error", err)
	}
	if e.cancel != nil {
		e.cancel()
	}
	_ = e.ua.Close()
}

// registrarOnly drops every request that does not come from the configured registrar: a softphone
// takes calls from its PBX, not from whoever can reach its socket.
func (e *Endpoint) registrarOnly(next sipgo.RequestHandler) sipgo.RequestHandler {
	return func(req *gosip.Request, tx gosip.ServerTransaction) {
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
	if err != nil || port != strconv.Itoa(int(e.Account.Settings.Port)) {
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

// Secure reports whether a negotiated call's audio is SRTP.
func Secure(med *diago.DialogMedia) bool {
	return offersSRTP(med.MediaSession().LocalSDP(true))
}

// DropTransport closes every connection of the account's transport, as a network loss would.
func (e *Endpoint) DropTransport() error {
	return e.ua.TransportLayer().Pool(e.Account.Settings.Transport.String()).Clear()
}

// ListenPorts are the ports the user agent listens on for a transport.
func (e *Endpoint) ListenPorts(network string) []int {
	return e.ua.TransportLayer().ListenPorts(network)
}

// RegistrationFailure maps a Register error to the reason shown to the user.
func RegistrationFailure(err error) domain.RegistrationState {
	fail := func(kind domain.FailureKind, code int) domain.RegistrationState {
		return domain.RegistrationState{Status: domain.RegistrationFailed, Failure: kind, Code: uint16(code)}
	}
	var (
		refused  *diago.RegisterResponseError
		verify   *tls.CertificateVerificationError
		unknown  x509.UnknownAuthorityError
		hostname x509.HostnameError
		invalid  x509.CertificateInvalidError
		netErr   net.Error
	)
	switch {
	case errors.As(err, &refused) && refused.RegisterRes != nil:
		if code := refused.RegisterRes.StatusCode; code == 401 || code == 403 || code == 407 {
			return fail(domain.Unauthorized, 0)
		} else {
			return fail(domain.Rejected, code)
		}
	case errors.As(err, &verify), errors.As(err, &unknown), errors.As(err, &hostname), errors.As(err, &invalid):
		return fail(domain.Certificate, 0)
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return fail(domain.Timeout, 0)
	}
	// The transport layer does not always keep the TLS error's type.
	if text := err.Error(); strings.Contains(text, "x509:") || strings.Contains(text, "tls: failed to verify") {
		return fail(domain.Certificate, 0)
	}
	return fail(domain.Network, 0)
}
