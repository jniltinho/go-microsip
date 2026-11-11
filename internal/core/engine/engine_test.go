package engine

import (
	"testing"
	"time"

	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/ports/memory"
)

func snapshot(id domain.CallID, version uint64, phase domain.CallPhase) domain.CallChanged {
	return domain.CallChanged{Call: domain.CallSnapshot{ID: id, Version: version, Phase: phase}}
}

// drain reads everything until the stream closes.
func drain(t *testing.T, events <-chan domain.Event) []domain.Event {
	t.Helper()
	var got []domain.Event
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return got
			}
			got = append(got, ev)
		case <-time.After(5 * time.Second):
			t.Fatalf("the event stream did not close; got %v", got)
		}
	}
}

// A front end that is not reading loses nothing final: intermediate snapshots and repeated
// registration states collapse, final snapshots and Stopped stay, in order.
func TestOutboxKeepsWhatIsFinal(t *testing.T) {
	o := newOutbox()
	ringing, established, held := domain.CallPhase{Kind: domain.Ringing}, domain.CallPhase{Kind: domain.Established}, domain.CallPhase{Kind: domain.Held}
	// The deliverer takes the first event and blocks on the absent reader; the rest queue up.
	o.emit(domain.RegistrationChanged{State: domain.RegistrationState{Status: domain.Registering}})
	time.Sleep(50 * time.Millisecond)
	o.emit(domain.RegistrationChanged{State: domain.RegistrationState{Status: domain.RegistrationFailed, Failure: domain.Timeout}})
	o.emit(domain.RegistrationChanged{State: domain.RegistrationState{Status: domain.Registered}})
	o.emit(snapshot(1, 1, ringing))
	o.emit(snapshot(1, 2, established))
	o.emit(snapshot(1, 3, held))
	o.emit(snapshot(1, 4, domain.EndedWith(domain.EndNormal)))
	o.emit(snapshot(2, 1, ringing))
	o.emit(snapshot(2, 2, domain.EndedWith(domain.EndCanceled)))
	for range 3 * noticeBacklog {
		o.emit(domain.Noticed{Notice: domain.NoticeBusy})
	}
	o.emit(domain.Stopped{})
	o.close()

	got := drain(t, o.out)
	var finals, notices int
	var lastSnapshot = map[domain.CallID]domain.CallSnapshot{}
	for i, ev := range got {
		switch v := ev.(type) {
		case domain.CallChanged:
			if prev, ok := lastSnapshot[v.Call.ID]; ok && (prev.Version >= v.Call.Version || prev.Phase.IsFinal()) {
				t.Errorf("call %d: snapshot %d after %d (final %v)", v.Call.ID, v.Call.Version, prev.Version, prev.Phase.IsFinal())
			}
			lastSnapshot[v.Call.ID] = v.Call
			if v.Call.Phase.IsFinal() {
				finals++
			}
		case domain.Noticed:
			notices++
		case domain.Stopped:
			if i != len(got)-1 {
				t.Errorf("Stopped is event %d of %d, not the last", i+1, len(got))
			}
		}
	}
	if finals != 2 || lastSnapshot[1].Phase != domain.EndedWith(domain.EndNormal) || lastSnapshot[2].Phase != domain.EndedWith(domain.EndCanceled) {
		t.Errorf("final snapshots: %d, %v", finals, lastSnapshot)
	}
	if _, ok := got[len(got)-1].(domain.Stopped); !ok {
		t.Errorf("the last event is %T, want Stopped", got[len(got)-1])
	}
	if notices > noticeBacklog {
		t.Errorf("%d notices kept, more than the backlog of %d", notices, noticeBacklog)
	}
	if len(got) > noticeBacklog+8 {
		t.Errorf("%d events for a reader that was away: intermediate ones were not merged", len(got))
	}
	// The registration the reader ends up with is the newest.
	var reg domain.RegistrationState
	for _, ev := range got {
		if v, ok := ev.(domain.RegistrationChanged); ok {
			reg = v.State
		}
	}
	if reg.Status != domain.Registered {
		t.Errorf("last registration state %v, want registered", reg)
	}
}

func next(t *testing.T, e *Engine) domain.Event {
	t.Helper()
	select {
	case ev, ok := <-e.Events():
		if !ok {
			t.Fatal("the event stream closed")
		}
		return ev
	case <-time.After(3 * time.Second):
		t.Fatal("no event")
		return nil
	}
}

// Commands that cannot be carried out are refused with the notice that says why, and nothing else
// happens.
func TestRefusals(t *testing.T) {
	e := Start(Options{Audio: &memory.Audio{}})
	target, _ := domain.ParseDialTarget("1002")
	key, _ := domain.NewDTMFKey('1')
	for name, c := range map[string]struct {
		cmd  domain.Command
		want domain.Notice
	}{
		"dial without an account":   {domain.Dial{Target: target}, domain.NoticeNotRegistered},
		"answer an unknown call":    {domain.Answer{Call: 9}, domain.NoticeUnknownCall},
		"reject an unknown call":    {domain.Reject{Call: 9}, domain.NoticeUnknownCall},
		"hang up an unknown call":   {domain.Hangup{Call: 9}, domain.NoticeUnknownCall},
		"hold an unknown call":      {domain.Hold{Call: 9}, domain.NoticeUnknownCall},
		"resume an unknown call":    {domain.Resume{Call: 9}, domain.NoticeUnknownCall},
		"mute an unknown call":      {domain.SetMute{Call: 9, Muted: true}, domain.NoticeUnknownCall},
		"dtmf on an unknown call":   {domain.SendDTMF{Call: 9, Key: key}, domain.NoticeUnknownCall},
		"transfer an unknown call":  {domain.Transfer{Call: 9, Target: target}, domain.NoticeUnknownCall},
		"register invalid settings": {domain.Register{Settings: domain.AccountSettings{Username: "1001"}}, domain.NoticeInvalidState},
	} {
		if !e.Send(c.cmd) {
			t.Fatalf("%s: the command was not taken", name)
		}
		if ev, ok := next(t, e).(domain.Noticed); !ok || ev.Notice != c.want {
			t.Errorf("%s: got %v, want notice %v", name, ev, c.want)
		}
	}
	e.Send(domain.SetDND{On: true}) // no event
	e.Send(domain.Unregister{})     // already disabled: no event
	e.Send(domain.Shutdown{})
	got := drain(t, e.Events())
	if len(got) != 1 {
		t.Fatalf("after Shutdown got %v, want only Stopped", got)
	}
	if _, ok := got[0].(domain.Stopped); !ok {
		t.Fatalf("last event %T, want Stopped", got[0])
	}
	select {
	case <-e.Done():
	default:
		t.Error("Done is not closed after Stopped")
	}
	if e.Send(domain.Unregister{}) {
		t.Error("a stopped engine took a command")
	}
}

// The command queue is bounded: beyond it Send refuses instead of blocking or growing.
func TestCommandQueueIsBounded(t *testing.T) {
	e := &Engine{commands: make(chan domain.Command, commandQueue), stopped: make(chan struct{})} // no actor: nothing drains
	taken := 0
	done := make(chan struct{})
	go func() {
		for range 10 * commandQueue {
			if e.Send(domain.SetDND{On: true}) {
				taken++
			}
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Send blocked on a full queue")
	}
	if taken != commandQueue {
		t.Errorf("%d commands taken, want exactly the queue of %d", taken, commandQueue)
	}
}

// A registration that cannot reach its registrar is reported as failed with the reason, and the
// engine still shuts down within its bound.
func TestUnreachableRegistrar(t *testing.T) {
	e := Start(Options{Audio: &memory.Audio{}})
	settings := domain.AccountSettings{Username: "1001", Domain: "pbx.invalid", Server: "pbx.invalid", Port: 5060, Transport: domain.UDP, Expires: 120}
	e.Send(domain.Register{Settings: settings, Password: domain.NewSecret("x")})
	if ev, ok := next(t, e).(domain.RegistrationChanged); !ok || ev.State.Status != domain.Registering {
		t.Fatalf("got %v, want registering", ev)
	}
	ev, ok := next(t, e).(domain.RegistrationChanged)
	if !ok || ev.State.Status != domain.RegistrationFailed || ev.State.Failure != domain.Network {
		t.Fatalf("got %v, want failed: network", ev)
	}
	start := time.Now()
	e.Send(domain.Shutdown{})
	got := drain(t, e.Events())
	if _, ok := got[len(got)-1].(domain.Stopped); !ok {
		t.Fatalf("last event %T, want Stopped", got[len(got)-1])
	}
	if took := time.Since(start); took > shutdownTimeout+time.Second {
		t.Errorf("shutdown took %v", took)
	}
}

func TestRetryDelayIsBounded(t *testing.T) {
	prev := time.Duration(0)
	for n := range 40 {
		d := retryDelay(n)
		if d < prev || d > 5*time.Minute || d < 5*time.Second {
			t.Fatalf("retry %d after %v (previous %v)", n, d, prev)
		}
		prev = d
	}
	if retryDelay(0) != 5*time.Second || retryDelay(3) != 40*time.Second || retryDelay(30) != 5*time.Minute {
		t.Errorf("delays %v %v %v", retryDelay(0), retryDelay(3), retryDelay(30))
	}
}
