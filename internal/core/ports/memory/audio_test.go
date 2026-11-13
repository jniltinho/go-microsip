package memory

import (
	"errors"
	"testing"

	"github.com/jniltinho/go-microsip/internal/core/ports"
)

func TestAudio(t *testing.T) {
	a := &Audio{Tone: 700}
	io, err := a.Open("", "")
	if err != nil {
		t.Fatal(err)
	}
	s := a.Last()
	var frame [ports.FrameSamples]int16
	for range 50 {
		if !io.Capture(&frame) {
			t.Fatal("no frame")
		}
		io.Play(&frame) // loop the microphone to the speaker
	}
	frames, share := s.Heard(700, 1300)
	if frames != 50 || share[700] < 0.99 || share[1300] > 0 {
		t.Fatalf("heard %d frames, shares %v", frames, share)
	}
	s.Reset()
	if frames, _ := s.Heard(700); frames != 0 {
		t.Fatal("Reset kept frames")
	}
	if a.OpenStreams() != 1 {
		t.Fatal("OpenStreams")
	}
	io.Close()
	if a.OpenStreams() != 0 || !s.Closed() {
		t.Fatal("Close")
	}
	if _, err := (&Audio{Unavailable: true}).Open("", ""); !errors.Is(err, ports.ErrNoAudioService) {
		t.Fatalf("Open = %v, want ErrNoAudioService", err)
	}
}
