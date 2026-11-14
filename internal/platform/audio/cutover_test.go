package audio

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A device layer made of memory: queues that record whether they were ended, and an opener the
// test can hold or fail.
type fakeQueues struct {
	mu      sync.Mutex
	opened  []*fakeQueue
	fail    atomic.Bool
	hold    chan struct{} // when set, open waits for it
	entered chan struct{} // when set, open says that it began
	onEnd   func(*fakeQueue)
}

type fakeQueue struct {
	instance uint64
	ended    atomic.Bool
	owner    *fakeQueues
}

func (q *fakeQueue) end() {
	if q.owner.onEnd != nil {
		q.owner.onEnd(q)
	}
	q.ended.Store(true)
}

func (f *fakeQueues) open(instance uint64) (captureQueue, error) {
	if f.entered != nil {
		f.entered <- struct{}{}
	}
	if f.hold != nil {
		<-f.hold
	}
	if f.fail.Load() {
		return nil, errors.New("no queue")
	}
	q := &fakeQueue{instance: instance, owner: f}
	f.mu.Lock()
	f.opened = append(f.opened, q)
	f.mu.Unlock()
	return q, nil
}

// live is the queues opened and not ended.
func (f *fakeQueues) live() []*fakeQueue {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*fakeQueue
	for _, q := range f.opened {
		if !q.ended.Load() {
			out = append(out, q)
		}
	}
	return out
}

// settled waits until the worker has one live queue and it is the current instance's.
func settled(t *testing.T, c *cutover, f *fakeQueues) uint64 {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		c.mu.Lock()
		current := c.current
		c.mu.Unlock()
		c.b.discard.Lock()
		discarding := c.b.discarding
		c.b.discard.Unlock()
		if live := f.live(); len(live) == 1 && live[0].instance == current && !discarding {
			return current
		}
	}
	t.Fatal("the cutover did not settle on one queue")
	return 0
}

const (
	marked = int16(-7777) // a sample recorded before a discard
	fresh  = int16(1234)  // one recorded after it
)

func block(value int16, samples int) []int16 {
	p := make([]int16, samples)
	for i := range p {
		p[i] = value
	}
	return p
}

// drain reads every frame the engine can get and fails if a marked sample is among them.
func drain(t *testing.T, b *buffers) (frames int) {
	t.Helper()
	var frame [FrameSamples]int16
	for b.Capture(&frame) {
		frames++
		for _, v := range frame {
			if v == marked {
				t.Fatalf("a sample recorded before the discard reached the engine (frame %d)", frames)
			}
		}
	}
	return frames
}

func TestACallbackHeldBetweenItsCheckAndItsPublicationCannotOutliveADiscard(t *testing.T) {
	f := &fakeQueues{}
	c := newCutover(newBuffers(), f.open)
	defer c.close()
	instance := settled(t, c, f)

	inside, release := make(chan struct{}), make(chan struct{})
	c.hook = func() { close(inside); <-release }
	published := make(chan bool)
	go func() { published <- c.publish(instance, block(marked, 4*FrameSamples)) }()
	<-inside // the callback passed its check and has not published yet
	c.hook = nil

	discarded := make(chan struct{})
	go func() { c.discard(); close(discarded) }()
	select {
	case <-discarded:
		t.Fatal("a discard completed while a callback sat between its check and its publication")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if !<-published {
		t.Error("the callback that held the lock was the current instance's and should have published")
	}
	<-discarded
	// What it published went in before the discard emptied the ring.
	if frames := drain(t, c.b); frames != 0 {
		t.Errorf("%d frames were left for the engine after the discard", frames)
	}
}

func TestABufferThatSpansTheCutoverIsDroppedAndNotHandedBack(t *testing.T) {
	f := &fakeQueues{}
	c := newCutover(newBuffers(), f.open)
	defer c.close()
	old := settled(t, c, f)
	c.discard()
	if c.publish(old, block(marked, 4*FrameSamples)) {
		t.Error("a callback of the retired queue was told to hand its buffer back")
	}
	settled(t, c, f)
	if c.publish(old, block(marked, 4*FrameSamples)) {
		t.Error("a callback of the retired queue was told to hand its buffer back after the new one opened")
	}
	if frames := drain(t, c.b); frames != 0 {
		t.Errorf("%d frames reached the engine from the retired queue", frames)
	}
}

func TestCallbacksThatArriveWhileTheOldQueueIsDisposedPublishNothing(t *testing.T) {
	f := &fakeQueues{}
	var c *cutover
	late := atomic.Int64{}
	f.onEnd = func(q *fakeQueue) {
		// The system delivers what the queue still held while it is being disposed of.
		for range 5 {
			if c.publish(q.instance, block(marked, FrameSamples)) {
				late.Add(1)
			}
		}
	}
	c = newCutover(newBuffers(), f.open)
	settled(t, c, f)
	c.discard()
	settled(t, c, f)
	c.close()
	<-c.done
	if late.Load() != 0 {
		t.Errorf("%d callbacks of a queue being disposed of were accepted", late.Load())
	}
	if frames := drain(t, c.b); frames != 0 {
		t.Errorf("%d frames reached the engine", frames)
	}
}

func TestAQueueThatCannotBeReopenedLosesCaptureAndKeepsNothingRunning(t *testing.T) {
	f := &fakeQueues{}
	c := newCutover(newBuffers(), f.open)
	defer c.close()
	old := settled(t, c, f)
	f.fail.Store(true)
	c.discard()
	for deadline := time.Now().Add(5 * time.Second); !c.lost.Load(); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("capture was not reported lost")
		}
	}
	if live := f.live(); len(live) != 0 {
		t.Errorf("%d queues were kept running after the reopening failed", len(live))
	}
	if c.publish(old, block(marked, 4*FrameSamples)) {
		t.Error("the old queue may still publish")
	}
	if frames := drain(t, c.b); frames != 0 {
		t.Errorf("%d frames reached the engine", frames)
	}
}

func TestRepeatedDiscardsLeaveOneQueue(t *testing.T) {
	f := &fakeQueues{}
	c := newCutover(newBuffers(), f.open)
	defer c.close()
	settled(t, c, f)
	for range 200 {
		c.discard()
	}
	current := settled(t, c, f)
	f.mu.Lock()
	opened := len(f.opened)
	f.mu.Unlock()
	if opened < 2 {
		t.Errorf("%d queues were opened in all: the discards opened none", opened)
	}
	if !c.publish(current, block(fresh, FrameSamples)) {
		t.Error("the queue left after the discards may not publish")
	}
}

func TestADiscardRacingCloseLeavesNoQueue(t *testing.T) {
	for range 300 {
		f := &fakeQueues{}
		c := newCutover(newBuffers(), f.open)
		var racing sync.WaitGroup
		racing.Add(2)
		go func() { defer racing.Done(); c.discard() }()
		go func() { defer racing.Done(); c.close() }()
		racing.Wait()
		<-c.done
		if live := f.live(); len(live) != 0 {
			t.Fatalf("%d queues outlived Close", len(live))
		}
		c.mu.Lock()
		current := c.current
		c.mu.Unlock()
		if current != 0 {
			t.Fatalf("instance %d may still publish after Close", current)
		}
	}
}

func TestFreshAudioArrivesAfterTheBarrierAndNothingMarkedDoes(t *testing.T) {
	f := &fakeQueues{}
	c := newCutover(newBuffers(), f.open)
	defer c.close()
	old := settled(t, c, f)
	c.publish(old, block(marked, 6*FrameSamples))
	c.discard()
	c.publish(old, block(marked, 6*FrameSamples)) // late, from the retired queue
	current := settled(t, c, f)
	// The first discardDepth samples of the new queue are dropped as well; what follows flows.
	frames := 0
	for range 20 {
		c.publish(current, block(fresh, FrameSamples))
		c.publish(old, block(marked, FrameSamples))
		frames += drain(t, c.b)
	}
	if frames == 0 {
		t.Error("no fresh audio reached the engine after the discard")
	}
}

func TestAnOpeningStillUnderWayCannotBringCaptureBackAfterClose(t *testing.T) {
	f := &fakeQueues{hold: make(chan struct{}), entered: make(chan struct{}, 1)}
	c := newCutover(newBuffers(), f.open)
	<-f.entered // the worker is inside the opening
	c.mu.Lock()
	opening := c.current
	c.mu.Unlock()
	closed := make(chan struct{})
	go func() { c.close(); close(closed) }()
	select {
	case <-closed: // Close does not wait for the opening
	case <-time.After(5 * time.Second):
		t.Fatal("Close waited for a queue that was still being opened")
	}
	if c.publish(opening, block(marked, 4*FrameSamples)) {
		t.Error("the instance being opened may publish after Close")
	}
	close(f.hold) // the opening returns after Close
	<-c.done
	if live := f.live(); len(live) != 0 {
		t.Errorf("%d queues outlived Close", len(live))
	}
	if frames := drain(t, c.b); frames != 0 {
		t.Errorf("%d frames reached the engine after Close", frames)
	}
}
