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
// call, and does nothing when a test is already running.
func (p *Phone) StartAudioTest() error {
	if p.inCall.Load() {
		return Refused(domain.NoticeBusy)
	}
	p.mu.Lock()
	running := p.test != nil
	p.mu.Unlock()
	if running {
		return nil
	}
	sound, err := chosen{p.audio, p}.Open("", "")
	if err != nil {
		return err
	}
	t := &audioTest{stop: make(chan struct{}), done: make(chan struct{})}
	p.mu.Lock()
	// Under the lock a call that began meanwhile is seen here, or sees this test and stops it.
	if p.inCall.Load() || p.test != nil {
		busy := p.inCall.Load()
		p.mu.Unlock()
		sound.Close()
		if busy {
			return Refused(domain.NoticeBusy)
		}
		return nil
	}
	p.test = t
	p.mu.Unlock()
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
