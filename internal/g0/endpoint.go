// Package g0 is the library-level lab probes of the Go rewrite (openspec/changes/migrate-to-go-wails,
// tasks 2.3-2.7): scenarios that drive the core's signaling endpoint directly against the
// disposable Asterisk lab. It is test infrastructure, not the engine.
package g0

import (
	"log/slog"
	"os"

	dmedia "github.com/emiago/diago/media"

	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/media"
	"github.com/jniltinho/go-microsip/internal/core/sip"
)

// Account is one fictional lab account in the probes' own terms.
type Account struct {
	User      string
	Password  string
	Server    string // host name of the registrar; also the TLS server name
	Port      int
	Transport string // "udp", "tcp" or "tls"
	// SecureMedia requires SDES-SRTP.
	SecureMedia bool
	// CAFile is the PEM bundle that replaces the system roots for a TLS account.
	CAFile string
	Codec  dmedia.Codec
	// Expires is the registration's lifetime in seconds; zero is two minutes.
	Expires uint32
}

func (a Account) String() string { return a.User + "/" + a.Transport }

// Endpoint is the core's endpoint together with the lab account it was made from.
type Endpoint struct {
	*sip.Endpoint
	Account Account
}

// settings is the lab account in the core's terms.
func (a Account) settings() domain.AccountSettings {
	transport, _ := domain.ParseTransport(a.Transport)
	s := domain.AccountSettings{
		Username: a.User, Domain: a.Server, Server: a.Server, Port: uint16(a.Port),
		Transport: transport, Expires: 120,
	}
	if a.Expires != 0 {
		s.Expires = a.Expires
	}
	if a.SecureMedia {
		s.Media = domain.SRTP
	}
	return s
}

// ca reads the lab's CA bundle; a missing one is left to the endpoint to refuse.
func (a Account) ca() []byte {
	if a.CAFile == "" {
		return nil
	}
	pem, _ := os.ReadFile(a.CAFile)
	return pem
}

// NewEndpoint creates the core's endpoint for a lab account.
func NewEndpoint(a Account, log *slog.Logger) (*Endpoint, error) {
	codec := media.PCMA
	if a.Codec.Name == dmedia.CodecAudioUlaw.Name {
		codec = media.PCMU
	}
	e, err := sip.NewEndpoint(sip.Account{
		Settings: a.settings(), Password: domain.NewSecret(a.Password), CA: a.ca(), Codecs: []media.Codec{codec},
	}, log)
	if err != nil {
		return nil, err
	}
	return &Endpoint{Endpoint: e, Account: a}, nil
}
