package sip

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jniltinho/go-microsip/internal/core/domain"
)

// authority is a throwaway certificate authority.
type authority struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func newAuthority(t *testing.T, name string) authority {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: name},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	return authority{cert, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

// server starts a TLS listener whose certificate for host is issued by ca.
func (ca authority) server(t *testing.T, host string) string {
	t.Helper()
	return ca.serverUntil(t, host, time.Now().Add(time.Hour))
}

// serverUntil is server with a certificate that is valid until the moment given.
func (ca authority) serverUntil(t *testing.T, host string, until time.Time) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: host}, DNSNames: []string{host},
		NotBefore: time.Now().Add(-48 * time.Hour), NotAfter: until,
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { _ = c.(*tls.Conn).Handshake(); c.Close() }()
		}
	}()
	return ln.Addr().String()
}

func handshake(addr string, conf *tls.Config) error {
	c, err := tls.DialWithDialer(&net.Dialer{Timeout: 2 * time.Second}, "tcp", addr, conf)
	if err == nil {
		c.Close()
	}
	return err
}

// A registrar whose certificate the operating system trusts, but which does not chain to the
// account's bundle, is refused: the bundle replaces the system roots, it does not add to them.
func TestBundleReplacesSystemRoots(t *testing.T) {
	system, lab := newAuthority(t, "System-trusted CA"), newAuthority(t, "Lab CA")
	dir := t.TempDir()
	systemFile, bundle := filepath.Join(dir, "system.pem"), filepath.Join(dir, "lab.pem")
	if err := os.WriteFile(systemFile, system.pem, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle, lab.pem, 0o600); err != nil {
		t.Fatal(err)
	}
	// Go reads the system roots once per process, from this file when it is set.
	t.Setenv("SSL_CERT_FILE", systemFile)
	t.Setenv("SSL_CERT_DIR", dir+"/none")

	addr := system.server(t, "pbx")
	// Control: with the system roots this server is trusted, so a refusal below is the bundle's.
	if err := handshake(addr, &tls.Config{ServerName: "pbx"}); err != nil {
		t.Fatalf("control failed, the server is not system-trusted: %v", err)
	}
	pem, err := os.ReadFile(bundle)
	if err != nil {
		t.Fatal(err)
	}
	conf, err := TLSConfig("pbx", pem)
	if err != nil {
		t.Fatal(err)
	}
	err = handshake(addr, conf)
	if err == nil {
		t.Fatal("a system-trusted certificate outside the bundle was accepted")
	}
	if !strings.Contains(err.Error(), "unknown authority") {
		t.Fatalf("refused for an unexpected reason: %v", err)
	}

	// The bundle's own authority is accepted, and only under the right name.
	good := lab.server(t, "pbx")
	if err := handshake(good, conf); err != nil {
		t.Fatalf("a certificate from the bundle was refused: %v", err)
	}
	wrong := lab.server(t, "other-host")
	if err := handshake(wrong, conf); err == nil || !strings.Contains(err.Error(), "not pbx") {
		t.Fatalf("a certificate for another host was not refused as such: %v", err)
	}
}

// A registrar whose certificate has expired is refused although the bundle's own authority
// issued it for the right name, and the refusal is a certificate failure, which the engine
// shows and does not retry on another transport.
func TestAnExpiredCertificateIsRefused(t *testing.T) {
	lab := newAuthority(t, "Lab CA")
	conf, err := TLSConfig("pbx", lab.pem)
	if err != nil {
		t.Fatal(err)
	}
	// Control: the same authority and name, still valid, is accepted.
	if err := handshake(lab.server(t, "pbx"), conf); err != nil {
		t.Fatalf("control failed, a valid certificate was refused: %v", err)
	}
	err = handshake(lab.serverUntil(t, "pbx", time.Now().Add(-time.Hour)), conf)
	if err == nil {
		t.Fatal("an expired certificate was accepted")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Fatalf("refused for an unexpected reason: %v", err)
	}
	if got := RegistrationFailure(fmt.Errorf("register: %w", err)); got.Status != domain.RegistrationFailed || got.Failure != domain.Certificate {
		t.Errorf("an expired certificate is reported as %v, want a certificate failure", got)
	}
}

// A TLS account without a bundle or with one that holds no certificate is an error, never a silent
// fall back to the system roots. (An unreadable file is refused earlier, by config.ReadCAFile.)
// An account with no bundle fails as a certificate failure, which is not retried, not as a
// network failure, which is.
func TestNoBundleIsACertificateFailure(t *testing.T) {
	_, err := TLSConfig("pbx", nil)
	if got := RegistrationFailure(fmt.Errorf("endpoint: %w", err)); got.Failure != domain.Certificate {
		t.Errorf("no bundle is reported as %v, want a certificate failure", got)
	}
}

func TestTLSConfigNeedsABundle(t *testing.T) {
	for name, pem := range map[string][]byte{"none": nil, "empty": []byte("not a certificate\n")} {
		if _, err := TLSConfig("pbx", pem); err == nil {
			t.Errorf("%s: a TLS configuration was built", name)
		}
	}
}
