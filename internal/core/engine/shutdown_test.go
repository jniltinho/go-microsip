package engine

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	gosip "github.com/emiago/sipgo/sip"

	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/ports/memory"
)

// pbx is a loopback PBX on TCP that registers anyone and answers every call, until it is told to
// fall silent: from then on it reads requests and answers none, like a PBX that hangs or a network
// that drops what comes back.
type pbx struct {
	port   int
	silent atomic.Bool
	byes   atomic.Int32
	// unregistrations counts the REGISTER requests with Expires: 0 that arrived.
	unregistrations atomic.Int32
}

func startPBX(t *testing.T) *pbx {
	t.Helper()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	p := &pbx{port: probe.Addr().(*net.TCPAddr).Port}
	probe.Close()
	// Where the softphone sends its audio: read and dropped.
	audio, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for buf := make([]byte, 2048); ; {
			if _, err := audio.Read(buf); err != nil {
				return
			}
		}
	}()
	ua, err := sipgo.NewUA()
	if err != nil {
		t.Fatal(err)
	}
	srv, err := sipgo.NewServer(ua)
	if err != nil {
		t.Fatal(err)
	}
	srv.OnRegister(func(req *gosip.Request, tx gosip.ServerTransaction) {
		if h := req.GetHeader("Expires"); h != nil && h.Value() == "0" {
			p.unregistrations.Add(1)
		}
		if p.silent.Load() {
			return
		}
		res := gosip.NewResponseFromRequest(req, 200, "OK", nil)
		res.AppendHeader(gosip.NewHeader("Expires", "60"))
		_ = tx.Respond(res)
	})
	srv.OnInvite(func(req *gosip.Request, tx gosip.ServerTransaction) {
		if p.silent.Load() {
			return
		}
		sdp := fmt.Sprintf("v=0\r\no=- 1 1 IN IP4 127.0.0.1\r\ns=-\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\n"+
			"m=audio %d RTP/AVP 0 101\r\na=rtpmap:0 PCMU/8000\r\na=rtpmap:101 telephone-event/8000\r\na=sendrecv\r\n",
			audio.LocalAddr().(*net.UDPAddr).Port)
		res := gosip.NewResponseFromRequest(req, 200, "OK", []byte(sdp))
		res.AppendHeader(gosip.NewHeader("Content-Type", "application/sdp"))
		res.AppendHeader(gosip.NewHeader("Contact", fmt.Sprintf("<sip:pbx@127.0.0.1:%d;transport=tcp>", p.port)))
		_ = tx.Respond(res)
	})
	srv.OnAck(func(*gosip.Request, gosip.ServerTransaction) {})
	srv.OnBye(func(req *gosip.Request, tx gosip.ServerTransaction) {
		p.byes.Add(1)
		if p.silent.Load() {
			return
		}
		_ = tx.Respond(gosip.NewResponseFromRequest(req, 200, "OK", nil))
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); _ = ua.Close(); _ = audio.Close() })
	ready := make(chan struct{})
	go func() {
		_ = srv.ListenAndServe(context.WithValue(ctx, sipgo.ListenReadyCtxKey, sipgo.ListenReadyCtxValue(ready)), "tcp", addr)
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("the PBX did not start")
	}
	return p
}

// registered starts an engine registered at the PBX.
func registered(t *testing.T, p *pbx) *Engine {
	t.Helper()
	e := Start(Options{Audio: &memory.Audio{}})
	e.Send(domain.Register{Password: domain.NewSecret("x"), Settings: domain.AccountSettings{
		Username: "1001", Domain: "127.0.0.1", Server: "127.0.0.1", Port: uint16(p.port), Transport: domain.TCP, Expires: 60,
	}})
	for {
		if s := registration(t, e, registerTimeout+time.Second); s.Status == domain.Registered {
			return e
		} else if s.Status != domain.Registering {
			t.Fatalf("registration %v", s)
		}
	}
}

// stopped sends Shutdown and returns how long the engine took to report Stopped, and what it
// reported on the way.
func stopped(t *testing.T, e *Engine) (time.Duration, []domain.Event) {
	t.Helper()
	start := time.Now()
	e.Send(domain.Shutdown{})
	var got []domain.Event
	for limit := time.After(shutdownTimeout + 5*time.Second); ; {
		select {
		case ev, ok := <-e.Events():
			if !ok {
				t.Fatalf("the event stream closed without Stopped; got %v", got)
			}
			got = append(got, ev)
			if _, done := ev.(domain.Stopped); done {
				return time.Since(start), got
			}
		case <-limit:
			t.Fatalf("no Stopped %v after Shutdown; got %v", shutdownTimeout+5*time.Second, got)
		}
	}
}

// Quitting is bounded whatever the PBX does (desktop-app: "Quit during a call"): with a PBX that
// answers nothing any more, the engine still stops within shutdownTimeout, having tried to
// unregister, and says the account is no longer registered.
func TestShutdownIsBoundedWhenThePBXStopsAnswering(t *testing.T) {
	p := startPBX(t)
	e := registered(t, p)
	p.silent.Store(true)
	took, events := stopped(t, e)
	if took > shutdownTimeout+time.Second {
		t.Errorf("the engine took %v to stop with a silent PBX, bound %v", took.Round(time.Millisecond), shutdownTimeout)
	}
	if took < time.Second {
		t.Errorf("the engine stopped after %v: it did not wait for the unregistration at all", took.Round(time.Millisecond))
	}
	if p.unregistrations.Load() == 0 {
		t.Error("no unregistration was sent before stopping")
	}
	disabled := false
	for _, ev := range events {
		if r, ok := ev.(domain.RegistrationChanged); ok && r.State.Status == domain.Disabled {
			disabled = true
		}
	}
	if !disabled {
		t.Errorf("the registration was not reported as disabled before Stopped: %v", events)
	}
}

// The same during a call: the call is hung up locally and reported ended, the hang-up and the
// unregistration are sent, and the engine stops within the bound although neither is answered.
func TestShutdownDuringACallIsBoundedWhenThePBXStopsAnswering(t *testing.T) {
	p := startPBX(t)
	e := registered(t, p)
	target, err := domain.ParseDialTarget("600")
	if err != nil {
		t.Fatal(err)
	}
	e.Send(domain.Dial{Target: target})
	for established := false; !established; {
		select {
		case ev := <-e.Events():
			if c, ok := ev.(domain.CallChanged); ok {
				if c.Call.Phase.IsFinal() {
					t.Fatalf("the call ended before it was established: %+v", c.Call)
				}
				established = c.Call.Phase.Kind == domain.Established
			}
		case <-time.After(10 * time.Second):
			t.Fatal("the call to the PBX was not established")
		}
	}
	p.silent.Store(true)
	took, events := stopped(t, e)
	if took > shutdownTimeout+time.Second {
		t.Errorf("the engine took %v to stop during a call with a silent PBX, bound %v", took.Round(time.Millisecond), shutdownTimeout)
	}
	ended := false
	for _, ev := range events {
		if c, ok := ev.(domain.CallChanged); ok && c.Call.Phase.IsFinal() {
			ended = true
		}
	}
	if !ended {
		t.Errorf("the call was not reported ended before Stopped: %v", events)
	}
	if p.byes.Load() == 0 {
		t.Error("no hang-up was sent to the PBX")
	}
	if p.unregistrations.Load() == 0 {
		t.Error("no unregistration was sent before stopping")
	}
}

// A quit nobody was asked about stops the engine only when it has no call, and the engine is the
// one that knows: with a call it changes nothing and says so, without one it stops.
func TestAConditionalShutdownIsRefusedDuringACall(t *testing.T) {
	p := startPBX(t)
	e := registered(t, p)
	target, err := domain.ParseDialTarget("600")
	if err != nil {
		t.Fatal(err)
	}
	e.Send(domain.Dial{Target: target})
	// Asked at once, before any snapshot of the call was read: a front end would not know of it.
	asked := make(chan bool, 1)
	e.Send(domain.Shutdown{IfIdle: true, Taken: asked})
	select {
	case taken := <-asked:
		if taken {
			t.Fatal("the engine stopped although a call had just been dialed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the engine did not answer the conditional shutdown")
	}
	var call domain.CallSnapshot
	for established := false; !established; {
		select {
		case ev, ok := <-e.Events():
			if !ok {
				t.Fatal("the engine stopped: the refused shutdown ended it")
			}
			if c, isCall := ev.(domain.CallChanged); isCall {
				call = c.Call
				established = c.Call.Phase.Kind == domain.Established
			}
		case <-time.After(10 * time.Second):
			t.Fatal("the call did not go on after the refused shutdown")
		}
	}
	e.Send(domain.Hangup{Call: call.ID})
	for ended := false; !ended; {
		select {
		case ev := <-e.Events():
			if c, isCall := ev.(domain.CallChanged); isCall {
				ended = c.Call.Phase.IsFinal()
			}
		case <-time.After(10 * time.Second):
			t.Fatal("the call did not end")
		}
	}
	// Without a call the same request is taken.
	e.Send(domain.Shutdown{IfIdle: true, Taken: asked})
	select {
	case taken := <-asked:
		if !taken {
			t.Fatal("the engine refused to stop although it had no call")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the engine did not answer the conditional shutdown")
	}
	for {
		select {
		case ev, ok := <-e.Events():
			if _, done := ev.(domain.Stopped); done || !ok {
				return
			}
		case <-time.After(shutdownTimeout + 5*time.Second):
			t.Fatal("the engine did not stop")
		}
	}
}
