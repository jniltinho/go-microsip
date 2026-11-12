package app

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/ports"
)

const (
	// AudioTestHz is the tone of the settings' sound test.
	AudioTestHz = 440
	// audioTestFor is how long a test that nobody stops lasts.
	audioTestFor  = 10 * time.Second
	audioTestRate = 8000
)

// audioTest is one run of the settings' sound test.
type audioTest struct {
	stop  chan struct{}
	done  chan struct{}
	level atomic.Uint32 // the microphone's peak, in thousandths of full scale; it falls back by itself
}

// StartAudioTest plays a tone on the speaker chosen in the settings and measures the chosen
// microphone, with no call, until StopAudioTest, a call, or ten seconds. It is refused during a
// call, and does nothing when a test is already running or starting. A test that is stopped
// while its devices are still opening never plays.
func (p *Phone) StartAudioTest() error {
	t := &audioTest{stop: make(chan struct{}), done: make(chan struct{})}
	p.mu.Lock()
	// Under the lock a call that begins meanwhile sees this test, starting or running, and stops
	// it; one that began before is seen here.
	switch {
	case p.inCall.Load():
		p.mu.Unlock()
		return Refused(domain.NoticeBusy)
	case p.test != nil || p.closed:
		p.mu.Unlock()
		return nil
	}
	p.test = t
	p.mu.Unlock()
	sound, err := chosen{p.audio, p}.Open("", "")
	if err != nil {
		p.mu.Lock()
		if p.test == t {
			p.test = nil
		}
		p.mu.Unlock()
		close(t.done)
		return err
	}
	select {
	case <-t.stop: // stopped while the devices were opening
		sound.Close()
		close(t.done)
		return nil
	default:
	}
	go p.runAudioTest(t, sound)
	return nil
}

func (p *Phone) runAudioTest(t *audioTest, sound ports.MediaIO) {
	defer close(t.done)
	defer sound.Close()
	defer func() {
		p.mu.Lock()
		if p.test == t {
			p.test = nil
		}
		p.mu.Unlock()
	}()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	end := time.After(audioTestFor)
	var heard, tone [ports.FrameSamples]int16
	for n := 0; ; {
		for i := range tone {
			tone[i] = int16(math.MaxInt16 / 4 * math.Sin(2*math.Pi*AudioTestHz*float64(n)/audioTestRate))
			n++
		}
		sound.Play(&tone)
		level := t.level.Load() * 4 / 5 // a peak stays in sight for a moment
		if sound.Capture(&heard) {
			for _, v := range heard {
				if peak := uint32(math.Abs(float64(v)) * 1000 / math.MaxInt16); peak > level {
					level = peak
				}
			}
		}
		t.level.Store(level)
		select {
		case <-tick.C:
		case <-t.stop:
			return
		case <-end:
			return
		}
	}
}

// StopAudioTest ends the sound test, when one is running, and returns once its devices are
// released.
func (p *Phone) StopAudioTest() {
	p.mu.Lock()
	t := p.test
	p.test = nil
	p.mu.Unlock()
	if t != nil {
		close(t.stop)
		<-t.done
	}
}

// AudioTest reports whether the sound test is running and the level of its microphone, from 0
// (silence) to 1 (full scale).
func (p *Phone) AudioTest() (running bool, level float64) {
	p.mu.Lock()
	t := p.test
	p.mu.Unlock()
	if t == nil {
		return false, 0
	}
	return true, float64(t.level.Load()) / 1000
}
