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

// foreignCodec is a PBX on loopback TCP that registers anybody and answers every call with a
// session description that offers one codec the softphone does not have (G.722).
func foreignCodec(t *testing.T) (port int, invites, byes *atomic.Int32) {
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
	invites, byes = new(atomic.Int32), new(atomic.Int32)
	srv.OnRegister(func(req *gosip.Request, tx gosip.ServerTransaction) {
		res := gosip.NewResponseFromRequest(req, 200, "OK", nil)
		res.AppendHeader(gosip.NewHeader("Expires", "120"))
		_ = tx.Respond(res)
	})
	srv.OnInvite(func(req *gosip.Request, tx gosip.ServerTransaction) {
		invites.Add(1)
		sdp := "v=0\r\no=- 1 1 IN IP4 127.0.0.1\r\ns=-\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=audio 40000 RTP/AVP 9\r\na=rtpmap:9 G722/8000\r\na=sendrecv\r\n"
		res := gosip.NewResponseFromRequest(req, 200, "OK", []byte(sdp))
		res.AppendHeader(gosip.NewHeader("Content-Type", "application/sdp"))
		res.AppendHeader(gosip.NewHeader("Contact", fmt.Sprintf("<sip:lab@%s;transport=tcp>", addr)))
		_ = tx.Respond(res)
	})
	srv.OnAck(func(*gosip.Request, gosip.ServerTransaction) {})
	srv.OnBye(func(req *gosip.Request, tx gosip.ServerTransaction) {
		byes.Add(1)
		_ = tx.Respond(gosip.NewResponseFromRequest(req, 200, "OK", nil))
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); _ = ua.Close() })
	go func() { _ = srv.ListenAndServe(ctx, "tcp", addr) }()
	for range 50 {
		if conn, err := net.Dial("tcp", addr); err == nil {
			conn.Close()
			return port, invites, byes
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the PBX did not start")
	return 0, nil, nil
}

// A call answered with only a codec the softphone does not have ends as not acceptable: it is
// never shown as established, and no sound device is opened for it or left open.
func TestAnAnswerWithOnlyAForeignCodecIsNotACall(t *testing.T) {
	port, invites, byes := foreignCodec(t)
	sound := &memory.Audio{}
	e := Start(Options{Audio: sound})
	e.Send(domain.Register{Password: domain.NewSecret("fictional-lab-password"), Settings: domain.AccountSettings{
		Username: "1001", Domain: "127.0.0.1", Server: "127.0.0.1", Port: uint16(port), Transport: domain.TCP, Expires: 120,
	}})
	registration(t, e, 3*time.Second)
	if got := registration(t, e, registerTimeout); got.Status != domain.Registered {
		t.Fatalf("registration %v", got)
	}
	target, err := domain.ParseDialTarget("600")
	if err != nil {
		t.Fatal(err)
	}
	e.Send(domain.Dial{Target: target})
	var end domain.CallSnapshot
	deadline := time.After(15 * time.Second)
	for !end.Phase.IsFinal() {
		select {
		case ev := <-e.Events():
			if c, ok := ev.(domain.CallChanged); ok {
				if c.Call.Phase.Kind == domain.Established || c.Call.Phase.Kind == domain.Held {
					t.Fatalf("a call without a common codec was shown as %v", c.Call.Phase)
				}
				end = c.Call
			}
		case <-deadline:
			t.Fatalf("the call did not end (last %v); the PBX saw %d INVITE requests", end.Phase, invites.Load())
		}
	}
	if end.Phase.End != domain.EndNotAcceptable {
		t.Errorf("the call ended as %v, want not acceptable", end.Phase)
	}
	if end.Answered() {
		t.Error("the final snapshot says the call was answered")
	}
	// The PBX answered, so it has a call: the softphone ends it there too.
	for deadline := time.Now().Add(3 * time.Second); byes.Load() == 0 && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
	}
	if byes.Load() != 1 {
		t.Errorf("the PBX was told %d times that the call it answered is over, want once", byes.Load())
	}
	e.Send(domain.Shutdown{})
	drain(t, e.Events())
	if open := sound.OpenStreams(); open != 0 {
		t.Errorf("%d sound streams left open", open)
	}
}
