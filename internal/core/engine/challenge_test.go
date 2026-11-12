package engine

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"log/slog"
	"net"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	gosip "github.com/emiago/sipgo/sip"

	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/ports/memory"
)

// challenger is a registrar on loopback TCP that asks for a digest: a REGISTER without the right
// answer gets the status it was made with (401, or 407 as a proxy asks) and a challenge; one
// with the right answer gets 200.
type challenger struct {
	status         int
	password       string
	seen, accepted atomic.Int32
}

const (
	challengeRealm = "lab.invalid"
	challengeNonce = "9f2c1d5e7a3b4c6d"
)

var digestField = regexp.MustCompile(`(\w+)="?([^",]+)"?`)

func sum(text string) string {
	hash := md5.Sum([]byte(text))
	return hex.EncodeToString(hash[:])
}

// answers reports whether the header holds the digest of the password for this request.
func (c *challenger) answers(header gosip.Header) bool {
	if header == nil {
		return false
	}
	fields := map[string]string{}
	for _, m := range digestField.FindAllStringSubmatch(header.Value(), -1) {
		fields[m[1]] = m[2]
	}
	if fields["nonce"] != challengeNonce || fields["realm"] != challengeRealm {
		return false
	}
	want := sum(sum(fields["username"]+":"+challengeRealm+":"+c.password) + ":" + challengeNonce + ":" + sum("REGISTER:"+fields["uri"]))
	return fields["response"] == want
}

func (c *challenger) serve(t *testing.T) (port int) {
	t.Helper()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	port = probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	ua, err := sipgo.NewUA()
	if err != nil {
		t.Fatal(err)
	}
	srv, err := sipgo.NewServer(ua)
	if err != nil {
		t.Fatal(err)
	}
	ask, answer := "WWW-Authenticate", "Authorization"
	if c.status == 407 {
		ask, answer = "Proxy-Authenticate", "Proxy-Authorization"
	}
	srv.OnRegister(func(req *gosip.Request, tx gosip.ServerTransaction) {
		c.seen.Add(1)
		if c.answers(req.GetHeader(answer)) {
			c.accepted.Add(1)
			res := gosip.NewResponseFromRequest(req, 200, "OK", nil)
			res.AppendHeader(gosip.NewHeader("Expires", "120"))
			_ = tx.Respond(res)
			return
		}
		res := gosip.NewResponseFromRequest(req, c.status, "Lab", nil)
		res.AppendHeader(gosip.NewHeader(ask, `Digest realm="`+challengeRealm+`", nonce="`+challengeNonce+`", algorithm=MD5`))
		_ = tx.Respond(res)
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); _ = ua.Close() })
	go func() { _ = srv.ListenAndServe(ctx, "tcp", addr) }()
	for range 50 { // the listener is up before the engine dials
		if conn, err := net.Dial("tcp", addr); err == nil {
			conn.Close()
			return port
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the registrar did not start")
	return 0
}

func challenged(port int, password string) domain.Register {
	return domain.Register{Password: domain.NewSecret(password), Settings: domain.AccountSettings{
		Username: "1001", Domain: "127.0.0.1", Server: "127.0.0.1", Port: uint16(port), Transport: domain.TCP, Expires: 120,
	}}
}

// A registrar that asks for the digest as a proxy does, with 407, is answered like one that asks
// with 401: the engine registers, with one challenge and one answer.
func TestAProxyChallengeIsAnswered(t *testing.T) {
	for _, status := range []int{401, 407} {
		c := &challenger{status: status, password: "fictional-lab-password"}
		port := c.serve(t)
		e := Start(Options{Audio: &memory.Audio{}})
		e.Send(challenged(port, "fictional-lab-password"))
		if got := registration(t, e, 3*time.Second); got.Status != domain.Registering {
			t.Fatalf("%d: first state %v", status, got)
		}
		if got := registration(t, e, registerTimeout); got.Status != domain.Registered {
			t.Fatalf("%d: registration %v, want registered", status, got)
		}
		if c.seen.Load() != 2 || c.accepted.Load() != 1 {
			t.Errorf("%d: the registrar saw %d REGISTER requests and accepted %d, want a challenge and its answer", status, c.seen.Load(), c.accepted.Load())
		}
		e.Send(domain.Shutdown{})
		drain(t, e.Events())
	}
}

// A password the registrar refuses is reported as such and is not tried again: no second
// attempt follows the first retry delay, whichever way the registrar asks, and the password is
// nowhere in the engine's diagnostics.
func TestRefusedCredentialsAreNotTriedAgain(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for a retry delay")
	}
	const wrong = "fictional-wrong-password"
	for _, status := range []int{401, 407} {
		c := &challenger{status: status, password: "fictional-lab-password"}
		port := c.serve(t)
		var log bytes.Buffer
		e := Start(Options{Audio: &memory.Audio{}, Log: slog.New(slog.NewTextHandler(&log, &slog.HandlerOptions{Level: slog.LevelDebug}))})
		e.Send(challenged(port, wrong))
		registration(t, e, 3*time.Second) // registering
		got := registration(t, e, registerTimeout)
		if got.Status != domain.RegistrationFailed || got.Failure != domain.Unauthorized {
			t.Fatalf("%d: registration %v, want failed as unauthorized", status, got)
		}
		after := c.seen.Load()
		select {
		case ev := <-e.Events():
			t.Errorf("%d: after the refusal the engine reported %v", status, ev)
		case <-time.After(retryDelay(0) + 2*time.Second):
		}
		if now := c.seen.Load(); now != after || c.accepted.Load() != 0 {
			t.Errorf("%d: the registrar saw %d REGISTER requests after the refusal (%d before), accepted %d", status, now-after, after, c.accepted.Load())
		}
		e.Send(domain.Shutdown{})
		drain(t, e.Events())
		if strings.Contains(log.String(), wrong) {
			t.Errorf("%d: the engine's diagnostics hold the password", status)
		}
	}
}
