package engine

import (
	"testing"
	"time"

	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/ports/memory"
)

func closed(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	case <-time.After(2 * time.Second):
		return false
	}
}

// Sound devices that were opened beside the actor are closed whoever comes first, the actor that
// would start them or the stop of the call's audio, and neither waits for the other: a Shutdown
// that overtakes the queued result of the open still finds the devices and closes them.
func TestOpenedDevicesAreClosedWhoeverComesFirst(t *testing.T) {
	sound := &memory.Audio{}
	open := func() *memory.Stream {
		s, err := sound.Open("", "")
		if err != nil {
			t.Fatal(err)
		}
		return s.(*memory.Stream)
	}

	// The audio was stopped while the devices were opening.
	c := &call{settled: make(chan struct{})}
	c.stopAudio()
	if s := open(); c.offer(s) || !s.Closed() || !closed(c.settled) || !closed(c.released) {
		t.Errorf("devices offered after the stop: closed %v", s.Closed())
	}

	// The devices are open and their result is queued; the stop comes before the actor takes them.
	c = &call{settled: make(chan struct{})}
	s := open()
	if !c.offer(s) {
		t.Fatal("the offer was refused before any stop")
	}
	c.stopAudio()
	if !closed(c.released) || !closed(c.settled) || !s.Closed() {
		t.Errorf("devices left open by a stop that overtook their result: closed %v", s.Closed())
	}
	if c.take() != nil {
		t.Error("the actor could still take devices the stop had closed")
	}

	// The actor took the devices: they are its to start and to close, not the handoff's.
	c = &call{settled: make(chan struct{})}
	s = open()
	if !c.offer(s) || c.take() != s || c.take() != nil {
		t.Fatal("the actor did not get the offered devices exactly once")
	}
	c.stopAudio()
	if !closed(c.released) || s.Closed() {
		t.Errorf("the handoff closed devices the actor owns: closed %v", s.Closed())
	}
	if sound.OpenStreams() != 1 {
		t.Errorf("%d streams open, want only the one the actor took", sound.OpenStreams())
	}
}

// A reader that falls far behind and comes back still gets every call's final snapshot and
// Stopped, in order; what is merged away is only what a later event replaced.
func TestRelayKeepsFinalStatesForAReaderThatWasAway(t *testing.T) {
	in := make(chan domain.Event)
	out := Relay(in)
	const calls = 300
	for id := domain.CallID(1); id <= calls; id++ {
		for version, phase := range []domain.CallPhase{
			{Kind: domain.Dialing}, {Kind: domain.Ringing}, {Kind: domain.Established}, {Kind: domain.Held},
			{Kind: domain.Established}, {Kind: domain.Terminating}, domain.EndedWith(domain.EndNormal),
		} {
			in <- snapshot(id, uint64(version+1), phase)
		}
		in <- domain.Noticed{Notice: domain.NoticeBusy}
	}
	in <- domain.Stopped{}
	close(in)
	got := drain(t, out)
	finals, next := 0, domain.CallID(1)
	for _, ev := range got {
		if c, ok := ev.(domain.CallChanged); ok && c.Call.Phase.IsFinal() {
			if c.Call.ID != next {
				t.Fatalf("final snapshot of call %d, want call %d next", c.Call.ID, next)
			}
			finals, next = finals+1, next+1
		}
	}
	if _, stopped := got[len(got)-1].(domain.Stopped); finals != calls || !stopped {
		t.Errorf("%d of %d final snapshots; last event %T", finals, calls, got[len(got)-1])
	}
	if len(got) > 2*calls+noticeBacklog+2 {
		t.Errorf("%d events for a reader that was away: intermediate ones were not merged", len(got))
	}
}
