// Package engine is the telephony engine: one account and one call, driven by Commands and
// reporting Events. A single actor goroutine owns every state change; network operations run beside
// it and report back, so the actor never blocks on the network and a slow front end never blocks
// the actor.
package engine

import (
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/media"
	"github.com/jniltinho/go-microsip/internal/core/ports"
)

const (
	// commandQueue bounds the commands waiting for the actor; Send refuses beyond it.
	commandQueue = 64
	// noticeBacklog bounds the notices waiting for a slow front end; notices are advisory.
	noticeBacklog = 64
)

// Options is what the engine needs from its surroundings.
type Options struct {
	// Audio opens the sound devices of a call.
	Audio ports.AudioDevices
	// Input and Output are the device IDs to open; empty is the default device.
	Input, Output string
	// CA is the PEM bundle that replaces the system roots for a TLS account.
	CA []byte
	// Untrusted is set when a CA bundle is configured and could not be used. A TLS account then
	// fails to register as a certificate failure, without contacting the server: it never falls
	// back to the system roots.
	Untrusted error
	// UserAgent is the product name sent in the User-Agent header.
	UserAgent string
	// Codecs is the offer in order of preference; empty is the endpoint's default.
	Codecs []media.Codec
	// Log receives diagnostics; they never carry a password, a dialed number or a DTMF key.
	Log *slog.Logger
}

// Engine is a running engine.
type Engine struct {
	opts     Options
	commands chan domain.Command
	events   *outbox
	stopped  chan struct{}

	// The results of work done beside the actor. They are bounded by the operations in flight, at
	// most one per command taken, so the list needs no limit of its own.
	mu      sync.Mutex
	results []func()
	wake    chan struct{}
	// dead: the actor has ended. A result that arrives later is applied by the goroutine that
	// brings it, one at a time under late; it finds its account or its call gone and only releases
	// what it brought.
	dead bool
	late sync.Mutex
}

// Start runs an engine. It stops after a Shutdown command; Stopped is then its last event.
func Start(o Options) *Engine {
	if o.Log == nil {
		o.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	e := &Engine{
		opts:     o,
		commands: make(chan domain.Command, commandQueue),
		wake:     make(chan struct{}, 1),
		events:   newOutbox(),
		stopped:  make(chan struct{}),
	}
	go e.run()
	return e
}

// Send hands a command to the engine without waiting. It reports false when the queue is full or
// the engine has stopped: the command was not taken, and the caller tells the user (NoticeOverloaded).
func (e *Engine) Send(c domain.Command) bool {
	select {
	case <-e.stopped:
		return false
	default:
	}
	select {
	case e.commands <- c:
		return true
	default:
		return false
	}
}

// Events delivers the engine's events in order. The channel closes after Stopped. A front end that
// reads slowly misses nothing final: intermediate snapshots of a call and repeated registration
// states are merged into the newest, but a call's last snapshot and Stopped are always delivered.
func (e *Engine) Events() <-chan domain.Event { return e.events.out }

// Done is closed when the engine has stopped.
func (e *Engine) Done() <-chan struct{} { return e.stopped }

// post hands the actor the result of work done beside it. No result is ever dropped: it may own a
// socket, a dialog or a sound device that only its callback releases.
func (e *Engine) post(f func()) {
	e.mu.Lock()
	if !e.dead {
		e.results = append(e.results, f)
		e.mu.Unlock()
		select {
		case e.wake <- struct{}{}:
		default:
		}
		return
	}
	e.mu.Unlock()
	e.late.Lock()
	defer e.late.Unlock()
	e.apply(f)
}

// apply runs one result. A failure in it is contained, so the results after it still run: each may
// own something only its callback releases.
func (e *Engine) apply(f func()) {
	defer func() {
		if r := recover(); r != nil {
			e.opts.Log.Error("engine failure in a result", "panic", fmt.Sprint(r))
		}
	}()
	f()
}

// take returns the results posted so far.
func (e *Engine) take() []func() {
	e.mu.Lock()
	defer e.mu.Unlock()
	results := e.results
	e.results = nil
	return results
}

// outbox decouples the actor from the front end: emit never blocks, and the deliverer waits for the
// reader as long as it takes.
type outbox struct {
	mu      sync.Mutex
	pending []domain.Event
	closed  bool
	sealed  bool // Stopped was emitted: nothing comes after it
	wake    chan struct{}
	out     chan domain.Event
}

func newOutbox() *outbox {
	o := &outbox{wake: make(chan struct{}, 1), out: make(chan domain.Event)}
	go o.deliver()
	return o
}

// emit queues an event, merging it into a pending one of the same kind when nothing is lost by it.
func (o *outbox) emit(ev domain.Event) {
	o.mu.Lock()
	if o.sealed {
		o.mu.Unlock()
		return
	}
	merged := false
	switch v := ev.(type) {
	case domain.Stopped:
		o.sealed = true
	case domain.RegistrationChanged:
		for i := len(o.pending) - 1; i >= 0 && !merged; i-- {
			if _, ok := o.pending[i].(domain.RegistrationChanged); ok {
				o.pending[i], merged = ev, true
			}
		}
	case domain.CallChanged:
		// Only a snapshot that is not final may be replaced, and only by a newer one of its call.
		for i := len(o.pending) - 1; i >= 0 && !merged; i-- {
			if p, ok := o.pending[i].(domain.CallChanged); ok && p.Call.ID == v.Call.ID {
				if !p.Call.Phase.IsFinal() && !v.Call.Phase.IsFinal() {
					o.pending[i], merged = ev, true
				}
				break
			}
		}
	case domain.Noticed:
		notices := 0
		for _, p := range o.pending {
			if _, ok := p.(domain.Noticed); ok {
				notices++
			}
		}
		merged = notices >= noticeBacklog // dropped: the front end is far behind anyway
	}
	if !merged {
		o.pending = append(o.pending, ev)
	}
	o.mu.Unlock()
	select {
	case o.wake <- struct{}{}:
	default:
	}
}

// close ends the stream after everything pending was delivered.
func (o *outbox) close() {
	o.mu.Lock()
	o.closed = true
	o.mu.Unlock()
	select {
	case o.wake <- struct{}{}:
	default:
	}
}

func (o *outbox) deliver() {
	for {
		o.mu.Lock()
		if len(o.pending) == 0 {
			closed := o.closed
			o.mu.Unlock()
			if closed {
				close(o.out)
				return
			}
			<-o.wake
			continue
		}
		ev := o.pending[0]
		o.pending = o.pending[1:]
		o.mu.Unlock()
		o.out <- ev
	}
}

// Relay passes the events of in on to a reader of any speed, by the rule of Engine.Events: while
// the reader is behind, intermediate snapshots of a call and repeated registration states are
// merged into the newest and notices are capped, but a call's final snapshot and Stopped are
// always delivered, in order. The returned channel closes after in is closed and everything
// pending was read. Sending to in never waits for the reader.
func Relay(in <-chan domain.Event) <-chan domain.Event {
	o := newOutbox()
	go func() {
		for ev := range in {
			o.emit(ev)
		}
		o.close()
	}()
	return o.out
}
