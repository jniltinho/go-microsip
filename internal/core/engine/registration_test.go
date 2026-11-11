package engine

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	gosip "github.com/emiago/sipgo/sip"

	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/ports/memory"
)

// registrar is a minimal registrar on loopback TCP: it answers every REGISTER with the status it
// was told and grants four seconds, so a renewal comes after three.
type registrar struct {
	status atomic.Int32
	seen   atomic.Int32
}

func (r *registrar) serve(t *testing.T, addr string) {
	t.Helper()
	ua, err := sipgo.NewUA()
	if err != nil {
		t.Fatal(err)
	}
	srv, err := sipgo.NewServer(ua)
	if err != nil {
		t.Fatal(err)
	}
	srv.OnRegister(func(req *gosip.Request, tx gosip.ServerTransaction) {
		r.seen.Add(1)
		res := gosip.NewResponseFromRequest(req, int(r.status.Load()), "Lab", nil)
		res.AppendHeader(gosip.NewHeader("Expires", "4"))
		_ = tx.Respond(res)
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); _ = ua.Close() })
	go func() { _ = srv.ListenAndServe(ctx, "tcp", addr) }()
}

// registration returns the next registration state.
func registration(t *testing.T, e *Engine, within time.Duration) domain.RegistrationState {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case ev, ok := <-e.Events():
			if !ok {
				t.Fatal("the event stream closed")
			}
			if r, ok := ev.(domain.RegistrationChanged); ok {
				return r.State
			}
		case <-deadline:
			t.Fatal("no registration state was reported")
		}
	}
}

// The engine comes back on its own: it retries a registrar that was unreachable, notices when a
// renewal is refused, and registers again once the registrar accepts it.
func TestRegistrationRecovers(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for two retry delays")
	}
	// A port nothing listens on yet.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()

	e := Start(Options{Audio: &memory.Audio{}})
	e.Send(domain.Register{Password: domain.NewSecret("x"), Settings: domain.AccountSettings{
		Username: "1001", Domain: "127.0.0.1", Server: "127.0.0.1", Port: uint16(port), Transport: domain.TCP, Expires: 4,
	}})
	expect := func(step string, within time.Duration, want domain.RegistrationState) {
		t.Helper()
		if got := registration(t, e, within); got != want {
			t.Fatalf("%s: registration %v, want %v", step, got, want)
		}
	}
	registering := domain.RegistrationState{Status: domain.Registering}
	registered := domain.RegistrationState{Status: domain.Registered}

	expect("first attempt", 3*time.Second, registering)
	expect("unreachable registrar", registerTimeout+time.Second, domain.RegistrationState{Status: domain.RegistrationFailed, Failure: domain.Network})

	r := &registrar{}
	r.status.Store(200)
	r.serve(t, addr)
	expect("retry", retryDelay(0)+2*time.Second, registering)
	expect("retry", registerTimeout, registered)

	r.status.Store(503)
	expect("refused renewal", 6*time.Second, domain.RegistrationState{Status: domain.RegistrationFailed, Failure: domain.Rejected, Code: 503})
	r.status.Store(200)
	expect("second retry", retryDelay(0)+2*time.Second, registering)
	expect("second retry", registerTimeout, registered)

	e.Send(domain.Shutdown{})
	got := drain(t, e.Events())
	if _, ok := got[len(got)-1].(domain.Stopped); !ok {
		t.Fatalf("last event %T, want Stopped", got[len(got)-1])
	}
	if r.seen.Load() < 3 {
		t.Errorf("the registrar saw %d REGISTER requests", r.seen.Load())
	}
}
