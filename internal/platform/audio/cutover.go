package audio

import (
	"sync"
	"sync/atomic"
)

// The capture side of a backend whose device layer delivers through queues that hold what they
// recorded (macOS's AudioQueue; add-macos-universal, design D4). Clearing the ring does not make
// a discard there: a buffer recorded earlier may still be inside the queue. The cutover is the
// queue itself. Every queue is an instance with a number, its callbacks carry that number, and
// only the current instance may publish; a discard retires the instance and a new queue is
// opened in its place.
//
// The file has no build constraint so that its tests run wherever the project's tests run.

// instances numbers every queue of the process, capture or playback: a number is never reused.
var instances atomic.Uint64

// captureQueue is one started capture queue of the device layer.
type captureQueue interface {
	// end stops the queue at once and disposes of it. When it returns, no callback of the queue
	// runs or will run.
	end()
}

// cutover orders what capture callbacks publish against discards and the end of the stream.
type cutover struct {
	b *buffers
	// open opens and starts a queue whose callbacks carry instance. It may take long: macOS
	// holds the start of a capture queue while it asks the user for the microphone.
	open func(instance uint64) (captureQueue, error)

	mu      sync.Mutex // check and publication are one critical section under it
	current uint64     // the instance that may publish; 0 when none may
	closed  bool

	wake chan struct{} // tells the worker that current or closed changed
	done chan struct{} // closed when the worker has ended and disposed of every queue
	lost atomic.Bool   // a queue could not be opened: nothing more comes from this microphone

	hook func() // tests: runs inside publish, between the check and the publication
}

// newCutover starts capture: the first queue is opened by the worker, not by the caller, so a
// stream opens at once while macOS still asks for the microphone.
func newCutover(b *buffers, open func(instance uint64) (captureQueue, error)) *cutover {
	c := &cutover{b: b, open: open, wake: make(chan struct{}, 1), done: make(chan struct{})}
	go c.run()
	c.discard()
	return c
}

// publish is what a capture callback does with the samples its buffer brought. It reports
// whether the buffer goes back to its queue: a callback of a retired instance publishes nothing
// and hands nothing back.
func (c *cutover) publish(instance uint64, p []int16) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if instance != c.current {
		return false
	}
	if c.hook != nil {
		c.hook()
	}
	c.b.captured(p)
	return true
}

// discard retires the current instance and empties the ring, in one step that no publication
// can interleave with; the worker then disposes of the old queue and opens a new one. It does
// not wait for either: nothing recorded so far can be published once it returns.
func (c *cutover) discard() {
	c.mu.Lock()
	if !c.closed {
		c.current = instances.Add(1)
		c.b.discardBegin()
	}
	c.mu.Unlock()
	c.signal()
}

// close ends capture for good. It does not wait for a queue that is still being opened: the
// worker disposes of it when its opening returns, and it can publish nothing meanwhile.
func (c *cutover) close() {
	c.mu.Lock()
	c.closed, c.current = true, 0
	c.mu.Unlock()
	c.signal()
}

func (c *cutover) signal() {
	select {
	case c.wake <- struct{}{}:
	default: // the worker has not looked since the last change: it will see this one too
	}
}

// run is the worker: the only place that opens queues and disposes of them, one at a time.
func (c *cutover) run() {
	defer close(c.done)
	var (
		queue  captureQueue
		opened uint64
	)
	for range c.wake {
		c.mu.Lock()
		want, closed := c.current, c.closed
		c.mu.Unlock()
		if queue != nil && (closed || want != opened) {
			queue.end()
			queue = nil
		}
		if closed {
			return
		}
		if queue != nil || want == 0 {
			continue
		}
		q, err := c.open(want)
		if err != nil {
			// It fails closed: the old queue is gone and no other is tried.
			c.mu.Lock()
			if c.current == want {
				c.current = 0
				c.lost.Store(true)
			}
			c.mu.Unlock()
			continue
		}
		queue, opened = q, want
		c.mu.Lock()
		if c.current == want && !c.closed {
			c.b.discardEnd(0)
		}
		c.mu.Unlock()
		// Retired or closed while it was opening: the change left a signal, and the next turn
		// of the loop disposes of this queue.
	}
}
