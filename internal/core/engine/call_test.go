package engine

import (
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/media"
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

// slowDevices are sound devices whose Close takes until it is let go.
type slowDevices struct {
	*memory.Stream
	gate chan struct{}
}

func (s slowDevices) Close() {
	<-s.gate
	s.Stream.Close()
}

// The shutdown waits for the sound devices of every call that ended, not only of the last one: a
// call that comes and goes meanwhile does not hide devices that are still closing.
func TestShutdownWaitsForTheDevicesOfEveryCall(t *testing.T) {
	a := &actor{e: Start(Options{Audio: &memory.Audio{}})}
	defer a.e.Send(domain.Shutdown{})
	stream, _ := (&memory.Audio{}).Open("", "")
	slow := slowDevices{Stream: stream.(*memory.Stream), gate: make(chan struct{})}

	first := &call{snap: domain.CallSnapshot{ID: 1, Phase: domain.CallPhase{Kind: domain.Established}}, settled: make(chan struct{})}
	if !first.offer(slow) {
		t.Fatal("the offer was refused")
	}
	a.end(first, domain.EndNormal) // its devices start closing, slowly
	second := &call{snap: domain.CallSnapshot{ID: 2, Phase: domain.CallPhase{Kind: domain.Ringing}}}
	a.end(second, domain.EndBusy) // a call that never had sound, over at once

	waited := make(chan struct{})
	go func() { a.cleanup.Wait(); close(waited) }()
	select {
	case <-waited:
		t.Fatal("the cleanup of the first call was forgotten behind the second")
	case <-time.After(200 * time.Millisecond):
	}
	close(slow.gate)
	if !closed(waited) || !slow.Closed() {
		t.Errorf("the devices of the first call: closed %v", slow.Closed())
	}
}

// A reader that is away while a call is answered, fails and ends still learns that it was
// answered, and when: the final snapshot says so itself, whatever was merged away before it.
func TestTheFinalSnapshotSaysTheCallWasAnswered(t *testing.T) {
	a := &actor{e: &Engine{events: newOutbox(), opts: Options{}}}
	c := a.newCall(domain.Outgoing, "1002", domain.Dialing)
	time.Sleep(50 * time.Millisecond) // the deliverer takes the first snapshot and waits for a reader
	a.move(c, domain.CallPhase{Kind: domain.Ringing})
	a.move(c, domain.CallPhase{Kind: domain.Established})
	time.Sleep(5 * time.Millisecond)
	a.move(c, domain.CallPhase{Kind: domain.Terminating}) // replaces "established" for the absent reader
	a.end(c, domain.EndFailed)
	a.e.events.emit(domain.Stopped{})
	a.e.events.close()
	got := drain(t, a.e.events.out)
	var final domain.CallSnapshot
	established := false
	for _, ev := range got {
		if ch, ok := ev.(domain.CallChanged); ok {
			established = established || ch.Call.Phase.Kind == domain.Established
			final = ch.Call
		}
	}
	if established {
		t.Fatal("the reader saw the call established: this test must merge that snapshot away")
	}
	if !final.Phase.IsFinal() || !final.Answered() || final.StartedMS == 0 || final.AnsweredMS < final.StartedMS || final.EndedMS < final.AnsweredMS+5 {
		t.Errorf("final snapshot %+v", final)
	}
	// A call that never was established says that too.
	b := &actor{e: &Engine{events: newOutbox(), opts: Options{}}}
	never := b.newCall(domain.Incoming, "1003", domain.Ringing)
	b.end(never, domain.EndCanceled)
	if never.snap.Answered() || never.snap.EndedMS == 0 {
		t.Errorf("unanswered call %+v", never.snap)
	}
}

// queuedMicrophone is a microphone that records on its own, whether or not anybody reads it, as a
// sound device does: samples queue up until Capture takes them, a frame at a time.
type queuedMicrophone struct {
	*memory.Stream
	samples []int16
}

func (q *queuedMicrophone) record(value int16, samples int) {
	for range samples {
		q.samples = append(q.samples, value)
	}
}

func (q *queuedMicrophone) Discard() { q.samples = q.samples[:0] }

func (q *queuedMicrophone) Capture(frame *[media.FrameSamples]int16) bool {
	if len(q.samples) < media.FrameSamples {
		return false
	}
	copy(frame[:], q.samples)
	q.samples = q.samples[media.FrameSamples:]
	return true
}

// What the microphone records while the call is held is never sent: not during the hold, and not
// after the resume, when a device that kept recording still has some of it queued.
func TestNothingRecordedOnHoldIsEverSent(t *testing.T) {
	const talk, private = 1, 7 // what is said to the other party, and what is said on hold
	q := &queuedMicrophone{Stream: &memory.Stream{}}
	mic := microphone{audio: q}
	var sent []int16
	changes := uint64(0) // moved by the actor at every change of hold or mute
	was := [2]bool{}
	tick := func(held, muted bool) {
		if now := [2]bool{held, muted}; now != was {
			was, changes = now, changes+2
		}
		if out, _ := mic.next(held, muted, changes); out != nil {
			sent = append(sent, out[:]...)
		}
	}
	for range 5 {
		q.record(talk, media.FrameSamples)
		tick(false, false)
	}
	if len(sent) != 5*media.FrameSamples || sent[0] != talk {
		t.Fatalf("before the hold: %d samples sent", len(sent))
	}
	// On hold the device records two and a half frames per tick: its queue grows whatever is read.
	for range 50 {
		q.record(private, media.FrameSamples*5/2)
		tick(true, false)
	}
	if len(sent) != 5*media.FrameSamples {
		t.Fatal("something was sent during the hold")
	}
	// The resume comes with a queue full of the hold and a frame that is half hold, half talk.
	q.record(private, media.FrameSamples/2)
	for range 20 {
		q.record(talk, media.FrameSamples)
		tick(false, false)
	}
	after := sent[5*media.FrameSamples:]
	talked := 0
	for _, v := range after {
		if v == private {
			t.Fatal("audio recorded on hold was sent after the resume")
		}
		if v == talk {
			talked++
		}
	}
	if len(after) != 20*media.FrameSamples || talked < 17*media.FrameSamples {
		t.Errorf("after the resume: %d samples sent, %d of them the talk", len(after), talked)
	}
	// Muted is the same promise: what is said then is not sent, not even once unmuted, and the
	// stream goes on with silence meanwhile.
	before := len(sent)
	for range 10 {
		q.record(private, media.FrameSamples*3/2)
		tick(false, true)
	}
	for range 10 {
		q.record(talk, media.FrameSamples)
		tick(false, false)
	}
	got := sent[before:]
	if len(got) != 20*media.FrameSamples || slices.Contains(got, private) || got[len(got)-1] != talk || slices.Contains(got[:10*media.FrameSamples], talk) {
		t.Errorf("mute and unmute: %d samples, private sent %v, last %d", len(got), slices.Contains(got, private), got[len(got)-1])
	}
}

// A mute or a hold that begins and ends between two ticks of the sender is never seen as a flag:
// the count of changes still makes the sender drop what was recorded meanwhile.
func TestPrivacyBetweenTwoTicksIsNotMissed(t *testing.T) {
	const talk, private = 1, 7
	q := &queuedMicrophone{Stream: &memory.Stream{}}
	mic := microphone{audio: q}
	c := &call{}
	var sent []int16
	tick := func() {
		if out, _ := mic.next(c.held.Load(), c.muted.Load(), c.privacy.Load()); out != nil {
			sent = append(sent, out[:]...)
		}
	}
	q.record(talk, media.FrameSamples)
	tick()
	for _, flag := range []*atomic.Bool{&c.muted, &c.held} {
		// The sender is busy elsewhere (a DTMF key, say) while the user mutes, speaks and unmutes.
		c.private(flag, true)
		q.record(private, 3*media.FrameSamples+40)
		c.private(flag, false)
		for range 6 {
			q.record(talk, media.FrameSamples)
			tick()
		}
	}
	if slices.Contains(sent, private) {
		t.Fatal("audio recorded during a mute or a hold the sender never saw was sent")
	}
	if n := len(sent) / media.FrameSamples; n != 13 || sent[len(sent)-1] != talk {
		t.Errorf("%d frames sent, the last one %d", n, sent[len(sent)-1])
	}
}
